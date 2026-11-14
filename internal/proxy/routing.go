package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RoutingResult is a single request, without following redirects. Duration is
// measured from request start to response headers, excluding core startup.
type RoutingResult struct {
	URL        string        `json:"url"`
	Mode       string        `json:"mode"`
	Rule       string        `json:"rule"`
	Outbound   string        `json:"outbound"`
	Chain      []string      `json:"chain,omitempty"`
	Node       string        `json:"node"`
	Source     string        `json:"source,omitempty"`
	Direct     bool          `json:"direct"`
	Rejected   bool          `json:"rejected"`
	Duration   time.Duration `json:"duration"`
	StatusCode int           `json:"status_code,omitempty"`
	Error      string        `json:"error,omitempty"`
}

func NormalizeRoutingURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(u.Host, " \t\r\n") {
		return "", errors.New("请输入有效的 HTTP 或 HTTPS 网址")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errors.New("网址端口无效")
		}
	}
	u.Fragment = ""
	return u.String(), nil
}

type RoutingProber struct {
	CorePath string
	Timeout  time.Duration
}

// Run uses a private core, retaining DNS, rule sets and selector defaults from
// the snapshot. It never starts a TUN, changes system proxy or opens a controller.
// Core diagnostics are consumed locally to observe the request's route, including
// rejected/failed requests, without racing a live-connections polling endpoint.
func (p RoutingProber) Run(parent context.Context, config map[string]any, value string) (result RoutingResult, err error) {
	result.URL, err = NormalizeRoutingURL(value)
	if err != nil {
		return result, err
	}
	if p.Timeout <= 0 {
		p.Timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, p.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if p.CorePath == "" {
		return result, errors.New("找不到分流测试内核")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return result, err
	}
	defer listener.Close()
	address := listener.Addr().String()
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return result, err
	}
	password := hex.EncodeToString(token[:])
	config, err = routingConfig(config, listener.Addr().(*net.TCPAddr).Port, password)
	if err != nil {
		return result, err
	}
	temporary, err := os.MkdirTemp("", "loop-routing-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temporary)
	data, err := json.Marshal(config)
	if err != nil {
		return result, err
	}
	path := filepath.Join(temporary, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return result, err
	}
	observer := &routingTrace{config: config}
	childCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(childCtx, p.CorePath, "run", "-c", path, "-D", temporary, "--disable-color")
	cmd.Stdout, cmd.Stderr = observer, observer
	listener.Close()
	if err := cmd.Start(); err != nil {
		return result, errors.New("无法启动分流测试内核")
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	defer func() { stop(); <-done }()
	if err := waitReady(ctx, address, done); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("分流测试内核启动失败，请检查配置、DNS 和规则集")
	}
	proxyURL := &url.URL{Scheme: "socks5", Host: address, User: url.UserPassword("routing-test", password)}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true, DisableCompression: true, ResponseHeaderTimeout: p.Timeout}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, result.URL, nil)
	if err != nil {
		return result, errors.New("网址无效")
	}
	req.Header.Set("Cache-Control", "no-cache")
	started := time.Now()
	response, requestErr := client.Do(req)
	if response != nil {
		result.Duration = time.Since(started)
		result.StatusCode = response.StatusCode
		response.Body.Close()
	}
	// Wait also drains the child's diagnostic pipes before reading the trace.
	stop()
	<-done
	observer.apply(&result)
	if result.Rejected {
		result.StatusCode, result.Duration = 0, 0
		result.Error = "已被分流规则拒绝"
	}
	if parent.Err() != nil {
		return result, parent.Err()
	}
	if requestErr != nil {
		result.Error = "连接失败，请检查节点、DNS 或目标网站"
		if ctx.Err() != nil {
			result.Error = "测试超时"
		}
		if result.Rejected {
			result.Error = "已被分流规则拒绝"
		}
	}
	if result.Outbound == "" && !result.Rejected {
		if result.Error != "" {
			result.Error += "；尚未取得出站结果"
		} else {
			result.Error = "请求已响应，但当前内核未提供可识别的出站结果"
		}
	}
	return result, nil
}

func routingConfig(original map[string]any, port int, password string) (map[string]any, error) {
	data, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, errors.New("测试配置为空")
	}
	// Prefer the owned default inlet regardless of order. Full-config debugging
	// keeps its custom inlet identity for existing inbound-specific rules.
	tag := "mixed-in"
	for _, value := range anySlice(config["inbounds"]) {
		in, _ := value.(map[string]any)
		if in["type"] == "mixed" && in["tag"] != "global-in" {
			if name, ok := in["tag"].(string); ok {
				tag = name
			}
			if tag == "mixed-in" {
				break
			}
		}
	}
	config["inbounds"] = []any{map[string]any{"type": "mixed", "tag": tag, "listen": "127.0.0.1", "listen_port": port,
		"users": []any{map[string]any{"username": "routing-test", "password": password}}}}
	config["log"] = map[string]any{"level": "debug", "timestamp": false}
	// No shared cache, listeners, pprof, dashboards or metrics from the user config.
	experimental := map[string]any{}
	if originalExp, ok := config["experimental"].(map[string]any); ok {
		if clash, ok := originalExp["clash_api"].(map[string]any); ok {
			if mode, ok := clash["default_mode"].(string); ok {
				experimental["clash_api"] = map[string]any{"default_mode": mode}
			}
		}
	}
	config["experimental"] = experimental
	route, ok := config["route"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("测试配置缺少 route")
	}
	route["auto_detect_interface"] = true
	return config, nil
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}
