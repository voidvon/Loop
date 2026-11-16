package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"local/loop/internal/inbound"
)

type Desktop struct {
	ConfigPath string
	CorePath   string
	StateDir   string
	HelperPath string
	// PrepareConfig compiles independent policy + selected node on every start.
	// A nil callback retains explicit full-config mode for debugging.
	PrepareConfig func() (string, error)
	// OnExit runs on a background goroutine after an unexpected core exit.
	OnExit func(error)

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	logFile *os.File
	proxy   *systemProxy
	lock    *os.File
	tun     *tunSession
}

func FindCore(executable string) (string, error) {
	// The bundled core always takes priority over development installations.
	for _, p := range []string{
		filepath.Join(filepath.Dir(executable), "sing-box"),
		"/opt/homebrew/bin/sing-box", "/usr/local/bin/sing-box",
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return p, nil
		}
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p, nil
	}
	return "", errors.New("找不到 sing-box 内核，请使用包含内核的应用包，或在开发环境安装 sing-box")
}

// LocalAddress returns the default inlet used for system proxy integration.
func LocalAddress(config []byte) (string, error) {
	endpoints, err := inbound.Parse(config)
	return endpoints.Default, err
}

func HasTUN(config []byte) bool {
	var c struct {
		Inbounds []struct {
			Type string `json:"type"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(config, &c) != nil {
		return false
	}
	for _, inbound := range c.Inbounds {
		if inbound.Type == "tun" {
			return true
		}
	}
	return false
}

func (d *Desktop) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done != nil {
		return errors.New("代理已启动")
	}
	if err := os.MkdirAll(d.StateDir, 0700); err != nil {
		return err
	}
	lock, err := acquireLock(filepath.Join(d.StateDir, "client.lock"))
	if err != nil {
		return err
	}
	d.lock = lock
	defer func() {
		if d.done == nil {
			d.releaseLock()
		}
	}()
	d.proxy = &systemProxy{journal: filepath.Join(d.StateDir, "proxy-state.json")}
	if err := d.proxy.restore(); err != nil {
		return fmt.Errorf("恢复上次的系统代理: %w", err)
	}
	configPath := d.ConfigPath
	if d.PrepareConfig != nil {
		configPath, err = d.PrepareConfig()
		if err != nil {
			return err
		}
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取节点配置: %w", err)
	}
	endpoints, err := inbound.Parse(config)
	if err != nil {
		return err
	}
	address := endpoints.Default
	if err := inbound.CheckAvailable(endpoints.Addresses()); err != nil {
		return err
	}
	check := exec.CommandContext(ctx, d.CorePath, "check", "-c", configPath)
	if output, err := check.CombinedOutput(); err != nil {
		return fmt.Errorf("配置检查失败: %w\n%s", err, output)
	}
	d.logFile, err = os.OpenFile(filepath.Join(d.StateDir, "sing-box.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	var wait func() error
	var startErr error
	if HasTUN(config) {
		d.tun, err = prepareTUN(d.HelperPath, d.CorePath)
		if err != nil {
			d.logFile.Close()
			d.logFile = nil
			return err
		}
		startErr = d.tun.connect(ctx, config, d.logFile)
		d.done = d.tun.done
		session := d.tun
		wait = func() error { <-session.done; return session.exitError() }
	} else {
		cmd := exec.Command(d.CorePath, "run", "-c", configPath, "-D", d.StateDir)
		cmd.Stdout, cmd.Stderr = d.logFile, d.logFile
		if err := cmd.Start(); err != nil {
			d.logFile.Close()
			d.logFile = nil
			return err
		}
		d.cmd, d.done = cmd, make(chan struct{})
		done := d.done
		wait = func() error { err := cmd.Wait(); close(done); return err }
	}
	done := d.done
	go func() {
		err := wait()
		d.mu.Lock()
		unexpected := d.done == done
		var cleanup error
		if unexpected {
			cleanup = d.stopLocked()
		}
		d.mu.Unlock()
		if unexpected && d.OnExit != nil {
			d.OnExit(errors.Join(fmt.Errorf("sing-box 意外退出: %v", err), cleanup))
		}
	}()
	if startErr != nil {
		return errors.Join(startErr, d.stopLocked())
	}
	for _, endpoint := range endpoints.Addresses() {
		if err := waitReady(ctx, endpoint, done); err != nil {
			return errors.Join(err, d.stopLocked())
		}
	}
	// Verify a real request before switching the user's system proxy.
	if err := CheckConfiguredConnection(ctx, address, config); err != nil {
		return errors.Join(fmt.Errorf("节点连接测试失败: %w", err), d.stopLocked())
	}
	select {
	case <-done:
		return errors.Join(errors.New("内核在连接期间退出"), d.stopLocked())
	case <-ctx.Done():
		return errors.Join(ctx.Err(), d.stopLocked())
	default:
	}
	if d.tun == nil {
		if err := d.proxy.enable(address); err != nil {
			return errors.Join(fmt.Errorf("设置系统代理失败: %w", err), d.stopLocked())
		}
	}
	return nil
}

func waitReady(ctx context.Context, address string, done <-chan struct{}) error {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errors.New("内核启动失败，请查看 sing-box.log")
		case <-timer.C:
			return errors.New("等待内核监听超时")
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
		}
	}
}

// A direct-only configuration needs no working proxy node or foreign test URL.
// waitReady has already verified the local core listener before this check.
func CheckConfiguredConnection(ctx context.Context, address string, config []byte) error {
	var value struct {
		Outbounds []struct {
			Type string `json:"type"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(config, &value); err != nil {
		return err
	}
	if len(value.Outbounds) == 1 && value.Outbounds[0].Type == "direct" {
		dialer := net.Dialer{Timeout: time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return err
		}
		conn.Close()
		return ctx.Err()
	}
	// Test the fixed proxy inlet when present, including in homepage direct mode.
	if endpoints, err := inbound.Parse(config); err == nil && endpoints.Global != "" {
		address = endpoints.Global
	}
	return CheckConnection(ctx, address)
}

func CheckConnection(ctx context.Context, address string) error {
	proxyURL := &url.URL{Scheme: "http", Host: address}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.gstatic.com/generate_204", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("连通性检查返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

func (d *Desktop) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopLocked()
}

// Running reports whether the owned core is still alive after an operation.
func (d *Desktop) Running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done == nil {
		return false
	}
	select {
	case <-d.done:
		return false
	default:
		return true
	}
}

func (d *Desktop) stopLocked() error {
	var restoreErr error
	if d.proxy != nil {
		restoreErr = d.proxy.restore()
	}
	if d.done != nil {
		if d.tun != nil {
			d.tun.stop()
		} else {
			d.cmd.Process.Signal(os.Interrupt)
		}
		select {
		case <-d.done:
		case <-time.After(8 * time.Second):
			if d.tun != nil {
				return errors.New("等待 TUN 内核清理超时，请重试停止")
			}
			d.cmd.Process.Kill()
			<-d.done
		}
		d.cmd = nil
		d.done = nil
	}
	if d.tun != nil {
		restoreErr = errors.Join(restoreErr, d.tun.cleanup())
		d.tun = nil
	}
	if d.logFile != nil {
		d.logFile.Close()
		d.logFile = nil
	}
	d.releaseLock()
	return restoreErr
}

func (d *Desktop) releaseLock() {
	if d.lock != nil {
		d.lock.Close()
		d.lock = nil
	}
}
