package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"local/loop/internal/tunhelper"
)

type tunSession struct {
	helper, core, token string
	conn                *net.UnixConn
	done                chan struct{}
	stopOnce            sync.Once
	mu                  sync.Mutex
	err                 error
	cleanupErr          error
	stopping            bool
}

func TUNAvailable() bool { return true }

func prepareTUN(helper, core string) (*tunSession, error) {
	if err := tunhelper.CheckRouteConflict(); err != nil {
		return nil, err
	}
	if helper == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		helper = filepath.Join(filepath.Dir(executable), "loop-tun")
	}
	helper, err := filepath.Abs(helper)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(helper); err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return nil, errors.New("找不到 TUN 辅助程序，请使用完整应用包，或先运行 go build -o loop-tun ./cmd/loop-tun")
	}
	core, err = filepath.Abs(core)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &tunSession{helper: helper, core: core, token: hex.EncodeToString(secret), done: make(chan struct{})}, nil
}

func (s *tunSession) connect(ctx context.Context, config []byte, log io.Writer) error {
	request, err := tunhelper.BundleConfig(config)
	if err != nil {
		close(s.done)
		return err
	}
	request.Core = s.core
	// The helper executable is needed only for installation/updates. Ordinary
	// connections are owned directly by loopd, without an intermediate process.
	if err := tunhelper.EnsureService(ctx, s.helper, s.core); err != nil {
		close(s.done)
		return err
	}
	conn, err := tunhelper.DialService(ctx, s.token)
	if err != nil {
		close(s.done)
		return err
	}
	return s.start(ctx, conn, request, log)
}

func (s *tunSession) start(ctx context.Context, conn *net.UnixConn, request tunhelper.Request, log io.Writer) error {
	s.conn = conn
	ready := make(chan error, 1)
	go s.run(request, log, ready)
	select {
	case err := <-ready:
		return err
	case <-ctx.Done():
		s.stop()
		return ctx.Err()
	}
}

func (s *tunSession) run(request tunhelper.Request, log io.Writer, ready chan<- error) {
	defer close(s.done)
	defer s.conn.Close()
	decoder := json.NewDecoder(s.conn)
	// A half-close tells the service to stop. Keep reading until its EOF, which
	// acknowledges that the core and temporary DNS have both been released.
	defer func() {
		s.stop()
		for {
			var msg tunhelper.Message
			if err := decoder.Decode(&msg); err != nil {
				if err != io.EOF {
					s.mu.Lock()
					s.cleanupErr = fmt.Errorf("等待 TUN 服务清理失败：%w", err)
					s.err = errors.Join(s.err, s.cleanupErr)
					s.mu.Unlock()
				}
				return
			}
			s.message(msg, log)
		}
	}()
	s.conn.SetDeadline(time.Now().Add(10 * time.Second))
	err := func() error {
		var hello tunhelper.Message
		if err := decoder.Decode(&hello); err != nil {
			return err
		}
		if hello.Token != s.token {
			return errors.New("TUN 辅助进程身份验证失败")
		}
		if hello.Error != "" {
			return errors.New(hello.Error)
		}
		if err := json.NewEncoder(s.conn).Encode(request); err != nil {
			return err
		}
		for {
			var msg tunhelper.Message
			if err := decoder.Decode(&msg); err != nil {
				return fmt.Errorf("TUN 内核启动失败: %w", err)
			}
			s.message(msg, log)
			if msg.Error != "" {
				return errors.New(msg.Error)
			}
			if msg.Started {
				return nil
			}
		}
	}()
	if err != nil {
		s.setError(err)
		ready <- err
		return
	}
	// Serialize clearing the startup deadline with stop(), so cancellation cannot
	// accidentally clear the cleanup timeout or permit writes after shutdown.
	s.mu.Lock()
	if !s.stopping {
		s.conn.SetDeadline(time.Time{})
	}
	s.mu.Unlock()
	ready <- nil
	for {
		var msg tunhelper.Message
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				s.setError(err)
			}
			return
		}
		s.message(msg, log)
	}
}

func (s *tunSession) message(msg tunhelper.Message, log io.Writer) {
	if msg.Log != "" {
		io.WriteString(log, msg.Log)
	}
	if msg.Error != "" {
		s.setError(errors.New(msg.Error))
	}
}

func (s *tunSession) setError(err error) { s.mu.Lock(); s.err = errors.Join(s.err, err); s.mu.Unlock() }

func (s *tunSession) stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.stopping = true
		if s.conn != nil {
			s.conn.CloseWrite()
			s.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		}
	})
}
func (s *tunSession) cleanup() error {
	s.stop()
	<-s.done
	return s.cleanupErr
}
func (s *tunSession) exitError() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
