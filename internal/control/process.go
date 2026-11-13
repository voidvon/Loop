package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/proxy"
)

func UIExecutable(options app.Options) string {
	if filepath.Base(options.Executable) == "loop" {
		return options.Executable
	}
	for _, path := range []string{filepath.Join(filepath.Dir(options.Executable), "loop"), filepath.Join(filepath.Dir(options.Executable), "../../../../MacOS/loop"), filepath.Join("bin", "loop")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			absolute, _ := filepath.Abs(path)
			return absolute
		}
	}
	return options.Executable
}

func ProcessArgs(options app.Options) []string {
	args := []string{"--state-dir", options.StateDir}
	if options.ConfigPath != "" {
		args = append(args, "--config", options.ConfigPath)
	} else {
		args = append(args, "--profiles", options.Store.Dir)
	}
	if options.CorePath != "" {
		args = append(args, "--core", options.CorePath)
	}
	if options.HelperPath != "" {
		args = append(args, "--tun-helper", options.HelperPath)
	}
	return args
}

func Ensure(ctx context.Context, options app.Options, noTray bool) (*Client, error) {
	client, err := NewClient(options.StateDir)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	state, probeErr := client.Status(probeCtx)
	cancel()
	if probeErr == nil {
		if err := Compatible(state, options); err != nil {
			client.Close()
			return nil, err
		}
		return client, nil
	}
	// A responding but incompatible daemon must never be replaced.
	if state.Protocol != 0 {
		client.Close()
		return nil, probeErr
	}
	var executable string
	for _, path := range []string{filepath.Join(filepath.Dir(options.Executable), "../Helpers/loop-background.app/Contents/MacOS/loopd"), filepath.Join(filepath.Dir(options.Executable), "loopd"), filepath.Join("bin", "loopd")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			executable, _ = filepath.Abs(path)
			break
		}
	}
	if executable == "" {
		client.Close()
		return nil, errors.New("找不到 loopd；请使用完整应用包，开发环境先执行 make build")
	}
	if err := os.MkdirAll(options.StateDir, 0700); err != nil {
		client.Close()
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(options.StateDir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		client.Close()
		return nil, err
	}
	launchOptions := RuntimePaths(options)
	args := append(ProcessArgs(launchOptions), "--ui", UIExecutable(options))
	if noTray {
		args = append(args, "--no-tray")
	}
	command := exec.Command(executable, args...)
	command.Stdout, command.Stderr, command.SysProcAttr = log, log, detachProcess()
	if err := command.Start(); err != nil {
		log.Close()
		client.Close()
		return nil, err
	}
	log.Close()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, time.Second)
		state, err = client.Status(checkCtx)
		cancel()
		if err == nil {
			if err := Compatible(state, options); err != nil {
				client.Close()
				return nil, err
			}
			return client, nil
		}
		select {
		case <-ctx.Done():
			client.Close()
			return nil, ctx.Err()
		case <-timeout.C:
			client.Close()
			return nil, fmt.Errorf("后台启动超时，请检查 %s", filepath.Join(options.StateDir, "daemon.log"))
		case <-done:
			// Another launcher may have won the single-instance lock.
			checkCtx, cancel := context.WithTimeout(ctx, time.Second)
			state, err = client.Status(checkCtx)
			cancel()
			if err == nil {
				if err := Compatible(state, options); err != nil {
					client.Close()
					return nil, err
				}
				return client, nil
			}
			client.Close()
			return nil, fmt.Errorf("后台启动失败，请检查 %s", filepath.Join(options.StateDir, "daemon.log"))
		case <-ticker.C:
		}
	}
}

// A macOS background helper has its own bundle identity. Resolve bundled core
// and TUN helper against the UI/CLI bundle before crossing that boundary.
func RuntimePaths(options app.Options) app.Options {
	ui := UIExecutable(options)
	if options.CorePath == "" {
		options.CorePath, _ = proxy.FindCore(ui)
	}
	if options.HelperPath == "" {
		path := filepath.Join(filepath.Dir(ui), "loop-tun")
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			options.HelperPath = path
		}
	}
	return options
}

// UIEndpoint makes repeated launches focus the existing UI instead of keeping
// another Fyne process alive. It never owns the proxy or daemon lifetime.
func UIEndpoint(stateDir string, show, quit func()) (func(), error) {
	dir, err := SocketDirectory(stateDir)
	if err != nil {
		return nil, err
	}
	lock, err := lockFile(filepath.Join(dir, "ui.lock"))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "ui.sock")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			unlockFile(lock)
			return nil, errors.New("界面通信地址被普通文件占用")
		}
		_ = os.Remove(path)
	}
	listener, err := netListenUI(path)
	if err != nil {
		unlockFile(lock)
		return nil, err
	}
	server := newUIServer(show, quit)
	go func() { _ = server.Serve(listener) }()
	return func() { _ = server.Close(); _ = os.Remove(path); unlockFile(lock) }, nil
}

func ShowUI(ctx context.Context, options app.Options) error {
	dir, err := SocketDirectory(options.StateDir)
	if err != nil {
		return err
	}
	if requestShowUI(ctx, filepath.Join(dir, "ui.sock")) == nil {
		return nil
	}
	executable := UIExecutable(options)
	if executable == "" {
		return errors.New("找不到图形界面程序")
	}
	log, err := os.OpenFile(filepath.Join(options.StateDir, "ui.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.Command(executable, ProcessArgs(options)...)
	command.Stdout, command.Stderr, command.SysProcAttr = log, log, detachProcess()
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
