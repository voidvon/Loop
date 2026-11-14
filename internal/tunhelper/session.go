// Package tunhelper implements a short-lived privileged core supervisor.
// The client owns the socket: EOF (including a GUI crash) stops the core.
package tunhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Request struct {
	Core      string          `json:"core"`
	Config    json.RawMessage `json:"config"`
	Resources [][]byte        `json:"resources,omitempty"`
}

type Message struct {
	Token   string `json:"token,omitempty"`
	Started bool   `json:"started,omitempty"`
	Log     string `json:"log,omitempty"`
	Error   string `json:"error,omitempty"`
}

type streamWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	tail    string
	started bool
	ready   chan struct{}
}

func (w *streamWriter) send(m Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.encoder.Encode(m)
}
func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.encoder.Encode(Message{Log: string(p)}); err != nil {
		return 0, err
	}
	if !w.started {
		w.tail += string(p)
		if strings.Contains(w.tail, "sing-box started") {
			w.started = true
			close(w.ready)
		}
		if len(w.tail) > 256 {
			w.tail = w.tail[len(w.tail)-256:]
		}
	}
	return len(p), nil
}

// Serve runs once; there is no installed daemon or persistent elevated process.
func Serve(ctx context.Context, conn net.Conn, token string) error {
	if err := CheckRouteConflict(); err != nil {
		conn.Close()
		return err
	}
	return serve(ctx, conn, token, startDNS)
}

func serve(ctx context.Context, conn net.Conn, token string, setupDNS func(map[string]any) (func(), <-chan struct{}, error)) error {
	return serveSession(ctx, conn, token, setupDNS, "")
}

func serveSession(ctx context.Context, conn net.Conn, token string, setupDNS func(map[string]any) (func(), <-chan struct{}, error), managedCore string) (resultErr error) {
	w := &streamWriter{encoder: json.NewEncoder(conn), ready: make(chan struct{})}
	defer func() {
		if resultErr != nil {
			conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_ = w.send(Message{Error: resultErr.Error()})
		}
		conn.Close()
	}()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := w.send(Message{Token: token}); err != nil {
		return err
	}
	var req Request
	limit := int64(16 << 20)
	if managedCore != "" {
		limit = maxRequestSize
	}
	if err := json.NewDecoder(io.LimitReader(conn, limit)).Decode(&req); err != nil {
		return err
	}
	conn.SetDeadline(time.Time{})
	core := req.Core
	if managedCore != "" {
		core = managedCore
	}
	if !filepath.IsAbs(core) {
		return errors.New("内核路径必须为绝对路径")
	}
	state, err := os.MkdirTemp("", "loop-tun-root-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(state)
	// Privileged files stay in a private temporary directory. Never let a user
	// log/cache path become an arbitrary root file write.
	var config map[string]any
	if managedCore != "" {
		config, err = prepareManagedConfig(req, state)
		if err != nil {
			return err
		}
	} else {
		if err := json.Unmarshal(req.Config, &config); err != nil {
			return err
		}
	}
	if config == nil {
		return errors.New("配置为空")
	}
	config["log"] = map[string]any{"level": "info", "timestamp": true}
	delete(config, "experimental")
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	configPath := filepath.Join(state, "config.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return err
	}
	cmd := exec.Command(core, "run", "-c", configPath, "-D", state)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + state, "TMPDIR=" + state}
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		w.send(Message{Error: err.Error()})
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() error {
		cmd.Process.Signal(os.Interrupt)
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			return <-done
		}
	}
	closed := make(chan struct{})
	go func() { io.Copy(io.Discard, conn); close(closed) }()
	var dnsDone <-chan struct{}
	select {
	case <-w.ready:
		release, leaseDone, err := setupDNS(config)
		if err != nil {
			w.send(Message{Error: err.Error()})
			stop()
			return err
		}
		defer release()
		dnsDone = leaseDone
		if err := w.send(Message{Started: true}); err != nil {
			stop()
			return err
		}
	case err := <-done:
		startErr := fmt.Errorf("TUN 内核启动失败: %v", err)
		w.send(Message{Error: startErr.Error()})
		return startErr
	case <-closed:
		stop()
		return nil
	case <-ctx.Done():
		stop()
		return ctx.Err()
	}
	select {
	case err := <-done:
		if err != nil {
			w.send(Message{Error: fmt.Sprintf("TUN 内核退出: %v", err)})
		}
		return err
	case <-closed:
		stop()
		return nil
	case <-ctx.Done():
		stop()
		return ctx.Err()
	case <-dnsDone:
		stop()
		err := errors.New("TUN 临时 DNS 会话意外退出，连接已停止")
		w.send(Message{Error: err.Error()})
		return err
	}
}
