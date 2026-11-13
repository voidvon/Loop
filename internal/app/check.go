package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/proxy"
)

func CheckProfileConnection(core, config string, store profile.Store) error {
	if config != "" {
		return checkConnection(core, config)
	}
	temporary, err := os.MkdirTemp("", "miniproxy-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	path, err := PrepareProfile(store, filepath.Join(temporary, "config.json"))
	if err != nil {
		return err
	}
	return checkConnection(core, path)
}

// A developer smoke check that leaves the machine's proxy settings untouched.
func checkConnection(core, config string) error {
	data, err := os.ReadFile(config)
	if err != nil {
		return err
	}
	if proxy.HasTUN(data) {
		return errors.New("-check-connection 不启动 TUN，请在首页启动以完成系统授权")
	}
	address, err := proxy.LocalAddress(data)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("检查端口 %s: %w", address, err)
	}
	listener.Close()
	state, err := os.MkdirTemp("", "miniproxy-core-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(state)
	config, err = filepath.Abs(config)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, core, "check", "-c", config).CombinedOutput(); err != nil {
		return fmt.Errorf("配置检查: %w %s", err, output)
	}
	cmd := exec.CommandContext(ctx, core, "run", "-c", config, "-D", state)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			// Leave a completion for the deferred cleanup to consume.
			done <- nil
			return errors.New("内核提前退出")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := proxy.CheckConfiguredConnection(ctx, address, data); err == nil {
				return nil
			} else if ctx.Err() != nil {
				return err
			}
		}
	}
}
