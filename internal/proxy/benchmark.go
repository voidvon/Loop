package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BenchmarkOptions follows hey's count, worker, timeout, duration and per-worker
// rate semantics. This tool measures HTTP requests, not ICMP or link capacity.
type BenchmarkOptions struct {
	URL                string        `json:"url"`
	Requests           int           `json:"requests"`
	Concurrency        int           `json:"concurrency"`
	Timeout            time.Duration `json:"timeout"`
	Duration           time.Duration `json:"duration,omitempty"`
	Rate               float64       `json:"rate,omitempty"`
	Method             string        `json:"method"`
	DisableKeepAlive   bool          `json:"disable_keep_alive"`
	FollowRedirects    bool          `json:"follow_redirects"`
	DisableCompression bool          `json:"disable_compression"`
	HTTP2              bool          `json:"http2"`
}

func (o BenchmarkOptions) Validated() (BenchmarkOptions, error) {
	var err error
	o.URL, err = NormalizeRoutingURL(o.URL)
	if err != nil {
		return o, err
	}
	if o.Duration < 0 || o.Duration > time.Hour {
		return o, errors.New("持续时间须在 0 至 1 小时之间")
	}
	if o.Concurrency < 1 || o.Concurrency > 256 {
		return o, errors.New("并发数须在 1 至 256 之间")
	}
	if o.Duration == 0 && (o.Requests < o.Concurrency || o.Requests > 100000) {
		return o, errors.New("请求数须不少于并发数，且不超过 100000")
	}
	if o.Timeout <= 0 || o.Timeout > 120*time.Second {
		return o, errors.New("单次超时须大于 0 且不超过 120 秒")
	}
	if math.IsNaN(o.Rate) || math.IsInf(o.Rate, 0) || o.Rate < 0 || o.Rate > 10000 {
		return o, errors.New("每任务限速须在 0 至 10000 请求/秒之间")
	}
	if o.Method == "" {
		o.Method = http.MethodGet
	}
	if o.Method != http.MethodGet && o.Method != http.MethodHead {
		return o, errors.New("测速目前支持 GET 和 HEAD")
	}
	return o, nil
}

type BenchmarkTarget struct {
	Key, Name, Source string
	Outbound          map[string]any
}

type BenchmarkResult struct {
	Key               string         `json:"key"`
	Name              string         `json:"name"`
	Source            string         `json:"source,omitempty"`
	Planned           int            `json:"planned"`
	Completed         int            `json:"completed"`
	Success           int            `json:"success"`
	HTTPFailures      int            `json:"http_failures"`
	NetworkFailures   int            `json:"network_failures"`
	CancelledRequests int            `json:"cancelled_requests"`
	StatusCodes       map[int]int    `json:"status_codes"`
	Errors            map[string]int `json:"errors"`
	Bytes             int64          `json:"bytes"`
	Elapsed           time.Duration  `json:"elapsed"`
	Average           time.Duration  `json:"average"`
	Fastest           time.Duration  `json:"fastest"`
	Slowest           time.Duration  `json:"slowest"`
	P50               time.Duration  `json:"p50"`
	P90               time.Duration  `json:"p90"`
	P95               time.Duration  `json:"p95"`
	P99               time.Duration  `json:"p99"`
	RequestsPerSecond float64        `json:"requests_per_second"`
	BytesPerSecond    float64        `json:"bytes_per_second"`
	Finished          bool           `json:"finished"`
}

type BenchmarkReport struct {
	ID        string            `json:"id"`
	Options   BenchmarkOptions  `json:"options"`
	Results   []BenchmarkResult `json:"results"`
	Cancelled bool              `json:"cancelled"`
}

type BenchmarkProgress struct {
	ID      string            `json:"id"`
	Index   int               `json:"index"`
	Total   int               `json:"total"`
	Result  BenchmarkResult   `json:"result"`
	Results []BenchmarkResult `json:"results,omitempty"`
}

type BenchmarkProber struct{ CorePath string }

// Run loads all selected nodes in one private core, then benchmarks one node at
// a time. Concurrency is per node and never multiplied by the selection count.
// A non-nil config instead tests a snapshot of the current routing configuration.
func (p BenchmarkProber) Run(ctx context.Context, id string, options BenchmarkOptions, config map[string]any, targets []BenchmarkTarget, progress func(BenchmarkProgress)) (report BenchmarkReport, err error) {
	options, err = options.Validated()
	report.ID, report.Options = id, options
	if err != nil {
		return report, err
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if len(targets) == 0 || len(targets) > 128 {
		return report, errors.New("请选择 1 至 128 个测试目标")
	}
	build := func(port int, password string) (map[string]any, error) {
		if config != nil {
			value, err := routingConfig(config, port, password)
			if err == nil {
				value["log"] = map[string]any{"disabled": true}
			}
			return value, err
		}
		nodes := make([]ProbeTarget, len(targets))
		for i, target := range targets {
			nodes[i] = ProbeTarget{Key: target.Key, Outbound: target.Outbound}
		}
		return probeConfig(nodes, port, password)
	}
	address, password, closeSession, err := startBenchmarkCore(ctx, p.CorePath, build)
	if err != nil {
		return report, err
	}
	defer closeSession()
	for i, target := range targets {
		if ctx.Err() != nil {
			report.Cancelled = true
			return report, ctx.Err()
		}
		username := probeUsername(i)
		if config != nil {
			username = "routing-test"
		}
		proxyURL := &url.URL{Scheme: "socks5", Host: address, User: url.UserPassword(username, password)}
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: options.DisableKeepAlive,
			DisableCompression: options.DisableCompression, ForceAttemptHTTP2: options.HTTP2,
			MaxIdleConns: options.Concurrency, MaxIdleConnsPerHost: options.Concurrency, MaxConnsPerHost: options.Concurrency,
			TLSHandshakeTimeout: options.Timeout, ResponseHeaderTimeout: options.Timeout, IdleConnTimeout: 30 * time.Second}
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetHTTP1(true)
		transport.Protocols.SetHTTP2(options.HTTP2)
		client := &http.Client{Transport: transport}
		if !options.FollowRedirects {
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		}
		result := runHTTPBenchmark(ctx, client, options, target, func(value BenchmarkResult) {
			if progress != nil {
				progress(BenchmarkProgress{ID: id, Index: i, Total: len(targets), Result: value, Results: append([]BenchmarkResult(nil), report.Results...)})
			}
		})
		transport.CloseIdleConnections()
		report.Results = append(report.Results, result)
		if ctx.Err() != nil {
			report.Cancelled = true
			return report, ctx.Err()
		}
	}
	return report, nil
}

type benchmarkSample struct {
	duration  time.Duration
	bytes     int64
	status    int
	err       error
	cancelled bool
}

func runHTTPBenchmark(parent context.Context, client *http.Client, options BenchmarkOptions, target BenchmarkTarget, progress func(BenchmarkResult)) BenchmarkResult {
	ctx, cancel := context.WithCancel(parent)
	if options.Duration > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(parent, options.Duration)
	}
	defer cancel()
	started := time.Now()
	stats := newBenchmarkStats(target, options.Requests)
	if options.Duration > 0 {
		stats.result.Planned = 0
	}
	if progress != nil {
		progress(stats.snapshot(0, false))
	}
	samples := make(chan benchmarkSample, options.Concurrency)
	var counter atomic.Int64
	var workers sync.WaitGroup
	for range options.Concurrency {
		workers.Go(func() {
			var next time.Time
			for {
				if ctx.Err() != nil {
					return
				}
				if options.Duration == 0 && counter.Add(1) > int64(options.Requests) {
					return
				}
				if !next.IsZero() {
					if delay := time.Until(next); delay > 0 {
						timer := time.NewTimer(delay)
						select {
						case <-timer.C:
						case <-ctx.Done():
							timer.Stop()
							return
						}
					}
				}
				if ctx.Err() != nil {
					return
				}
				begin := time.Now()
				if options.Rate > 0 {
					next = begin.Add(time.Duration(float64(time.Second) / options.Rate))
				}
				sample := benchmarkRequest(ctx, client, options)
				samples <- sample
			}
		})
	}
	go func() { workers.Wait(); close(samples) }()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case sample, ok := <-samples:
			if !ok {
				result := stats.snapshot(time.Since(started), true)
				if progress != nil {
					progress(result)
				}
				return result
			}
			stats.add(sample)
		case <-ticker.C:
			if progress != nil {
				progress(stats.snapshot(time.Since(started), false))
			}
		}
	}
}

func benchmarkRequest(parent context.Context, client *http.Client, options BenchmarkOptions) benchmarkSample {
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, options.Method, options.URL, nil)
	if err != nil {
		return benchmarkSample{err: errors.New("请求地址无效")}
	}
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", "Loop-Benchmark")
	response, err := client.Do(req)
	sample := benchmarkSample{err: err}
	if response != nil {
		sample.status = response.StatusCode
		sample.bytes, sample.err = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if err != nil {
			sample.err = err
		}
	}
	sample.duration = time.Since(started)
	sample.cancelled = parent.Err() != nil && sample.err != nil
	if ctx.Err() != nil && !sample.cancelled && sample.err != nil {
		sample.err = context.DeadlineExceeded
	}
	return sample
}

func startBenchmarkCore(parent context.Context, core string, build func(int, string) (map[string]any, error)) (address, password string, cleanup func(), err error) {
	if core == "" {
		return "", "", nil, errors.New("找不到测速内核")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, err
	}
	defer listener.Close()
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		return "", "", nil, err
	}
	password = hex.EncodeToString(token[:])
	config, err := build(listener.Addr().(*net.TCPAddr).Port, password)
	if err != nil {
		return "", "", nil, err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return "", "", nil, err
	}
	temporary, err := os.MkdirTemp("", "loop-benchmark-*")
	if err != nil {
		return "", "", nil, err
	}
	remove := func() { os.RemoveAll(temporary) }
	path := filepath.Join(temporary, "config.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		remove()
		return "", "", nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, core, "run", "-c", path, "-D", temporary, "--disable-color")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	address = listener.Addr().String()
	listener.Close()
	if err = cmd.Start(); err != nil {
		cancel()
		remove()
		return "", "", nil, errors.New("无法启动批量测速内核")
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	cleanup = func() { cancel(); <-done; remove() }
	if err = waitReady(parent, address, done); err != nil {
		cleanup()
		if parent.Err() != nil {
			return "", "", nil, parent.Err()
		}
		return "", "", nil, errors.New("测速内核启动失败，请检查配置和所选节点")
	}
	return address, password, cleanup, nil
}

func benchmarkError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "请求超时"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "请求超时"
	}
	if strings.Contains(err.Error(), "x509:") || strings.Contains(err.Error(), "tls:") {
		return "TLS 验证或握手失败"
	}
	return "连接或读取失败"
}
