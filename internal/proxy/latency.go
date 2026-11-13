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
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const DefaultLatencyURL = "https://www.gstatic.com/generate_204"

type ProbeTarget struct {
	Key      string
	Outbound map[string]any
}
type LatencyResult struct {
	Duration time.Duration
	Err      error
}

// LatencyProber owns an isolated, unprivileged core with an authenticated local
// inbound. Requests route by authentication user, never by the active selector.
// No system proxy, TUN, saved configuration or application cache is involved.
type LatencyProber struct {
	CorePath    string
	URL         string
	Timeout     time.Duration
	Parallelism int
}

// Run reports each completed node through result, which may be called concurrently.
func (p LatencyProber) Run(ctx context.Context, targets []ProbeTarget, result func(string, LatencyResult)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("当前组没有可测速节点")
	}
	if p.CorePath == "" {
		return errors.New("找不到测速内核")
	}
	if p.URL == "" {
		p.URL = DefaultLatencyURL
	}
	if p.Timeout <= 0 {
		p.Timeout = 10 * time.Second
	}
	if p.Parallelism <= 0 {
		p.Parallelism = 4
	}
	if p.Parallelism > 8 {
		p.Parallelism = 8
	}
	token := make([]byte, 24)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	password := hex.EncodeToString(token)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	address := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	config, err := probeConfig(targets, port, password)
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "miniproxy-latency-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	path := filepath.Join(temporary, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	childCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(childCtx, p.CorePath, "run", "-c", path, "-D", temporary, "--disable-color")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	// Release immediately before launch. Authentication prevents another listener
	// from accidentally being treated as this test session.
	listener.Close()
	if err := cmd.Start(); err != nil {
		return errors.New("无法启动测速内核")
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() { stop(); <-done }()
	if err := waitReady(ctx, address, done); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("测速内核启动失败，请检查所测节点的配置")
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(p.Parallelism, len(targets)) {
		workers.Go(func() {
			for i := range jobs {
				measurement := measureLatency(ctx, address, probeUsername(i), password, p.URL, p.Timeout)
				if result != nil {
					result(targets[i].Key, measurement)
				}
			}
		})
	}
dispatch:
	for i := range targets {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		case <-done:
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-done:
		return errors.New("测速内核意外退出")
	default:
		return nil
	}
}
func probeUsername(i int) string { return "probe-" + strconv.Itoa(i) }
func probeConfig(targets []ProbeTarget, port int, password string) (map[string]any, error) {
	outbounds := []any{}
	users, rules := []any{}, []any{}
	seen := map[string]bool{}
	for i, target := range targets {
		if target.Key == "" || seen[target.Key] {
			return nil, errors.New("测速节点标识为空或重复")
		}
		seen[target.Key] = true
		data, err := json.Marshal(target.Outbound)
		if err != nil {
			return nil, err
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		if out == nil {
			return nil, errors.New("测速节点配置为空")
		}
		tag := probeUsername(i)
		out["tag"] = tag
		delete(out, "domain_resolver")
		outbounds = append(outbounds, out)
		users = append(users, map[string]any{"username": tag, "password": password})
		rules = append(rules, map[string]any{"auth_user": []string{tag}, "action": "route", "outbound": tag})
	}
	// An unmatched test request is rejected, never silently routed directly.
	rules = append(rules, map[string]any{"action": "reject"})
	return map[string]any{
		"log":       map[string]any{"disabled": true},
		"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": port, "users": users}},
		"outbounds": outbounds,
		"dns":       map[string]any{"servers": []any{map[string]any{"type": "udp", "tag": "probe-dns", "server": "223.5.5.5"}}, "final": "probe-dns", "strategy": "prefer_ipv4"},
		"route":     map[string]any{"auto_detect_interface": true, "default_domain_resolver": "probe-dns", "rules": rules, "final": probeUsername(0)},
	}, nil
}
func measureLatency(parent context.Context, address, username, password, targetURL string, timeout time.Duration) LatencyResult {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	proxyURL := &url.URL{Scheme: "http", Host: address, User: url.UserPassword(username, password)}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true, DisableCompression: true, ResponseHeaderTimeout: timeout}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return LatencyResult{Err: errors.New("测速地址无效")}
	}
	req.Header.Set("Cache-Control", "no-cache")
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return LatencyResult{Err: ctx.Err()}
		}
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return LatencyResult{Err: context.DeadlineExceeded}
		}
		return LatencyResult{Err: errors.New("连接失败")}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return LatencyResult{Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
	// Includes node connection, TLS and the test server's response headers.
	return LatencyResult{Duration: time.Since(started)}
}
