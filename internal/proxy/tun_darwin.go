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
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"local/loop/internal/tunhelper"
)

type tunSession struct {
	listener net.Listener
	conn     net.Conn
	dir      string
	token    string
	readDone chan struct{}
	mu       sync.Mutex
	err      error
}

func TUNAvailable() bool { return true }

func prepareTUN(helper, core string) (*tunSession, *exec.Cmd, error) {
	if err := tunhelper.CheckRouteConflict(); err != nil {
		return nil, nil, err
	}
	if helper == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		helper = filepath.Join(filepath.Dir(executable), "loop-tun")
	}
	helper, err := filepath.Abs(helper)
	if err != nil {
		return nil, nil, err
	}
	if info, err := os.Stat(helper); err != nil || info.IsDir() || info.Mode()&0111 == 0 {
		return nil, nil, errors.New("找不到 TUN 辅助程序，请使用完整应用包，或先运行 go build -o loop-tun ./cmd/loop-tun")
	}
	// Keep the Unix socket below macOS's 104-byte path limit.
	dir, err := os.MkdirTemp("/tmp", "loop-tun-")
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "control.sock"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		listener.Close()
		os.RemoveAll(dir)
		return nil, nil, err
	}
	s := &tunSession{listener: listener, dir: dir, token: hex.EncodeToString(secret)}
	core, err = filepath.Abs(core)
	if err != nil {
		s.cleanup()
		return nil, nil, err
	}
	return s, exec.Command(helper, "-client", "-core", core, "-socket", filepath.Join(dir, "control.sock"), "-token", s.token), nil
}

func (s *tunSession) connect(ctx context.Context, done <-chan struct{}, core string, config []byte, log io.Writer) error {
	type accepted struct {
		conn net.Conn
		err  error
	}
	result := make(chan accepted, 1)
	go func() { conn, err := s.listener.Accept(); result <- accepted{conn, err} }()
	var conn net.Conn
	select {
	case r := <-result:
		if r.err != nil {
			return r.err
		}
		conn = r.conn
	case <-ctx.Done():
		s.listener.Close()
		r := <-result
		if r.conn != nil {
			r.conn.Close()
		}
		return ctx.Err()
	case <-done:
		s.listener.Close()
		r := <-result
		if r.conn != nil {
			r.conn.Close()
		}
		return errors.New("TUN 授权未完成或已取消")
	}
	s.conn = conn
	s.listener.Close()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	decoder := json.NewDecoder(conn)
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
	core, err := filepath.Abs(core)
	if err != nil {
		return err
	}
	request, err := tunhelper.BundleConfig(config)
	if err != nil {
		return err
	}
	request.Core = core
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	for {
		var msg tunhelper.Message
		if err := decoder.Decode(&msg); err != nil {
			return fmt.Errorf("TUN 内核启动失败: %w", err)
		}
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
		if msg.Log != "" {
			io.WriteString(log, msg.Log)
		}
		if msg.Started {
			break
		}
	}
	conn.SetDeadline(time.Time{})
	s.readDone = make(chan struct{})
	go func() {
		defer close(s.readDone)
		for {
			var msg tunhelper.Message
			if err := decoder.Decode(&msg); err != nil {
				return
			}
			if msg.Log != "" {
				io.WriteString(log, msg.Log)
			}
			if msg.Error != "" {
				s.mu.Lock()
				s.err = errors.New(msg.Error)
				s.mu.Unlock()
			}
		}
	}()
	return nil
}

func (s *tunSession) helperConnected() bool { return s.conn != nil }

func (s *tunSession) stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	if s.conn != nil {
		s.conn.Close()
	}
}
func (s *tunSession) cleanup() {
	s.stop()
	if s.readDone != nil {
		<-s.readDone
	}
	os.RemoveAll(s.dir)
}

func (s *tunSession) exitError() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
