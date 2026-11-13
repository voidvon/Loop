package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A real local HTTP CONNECT node, so tests exercise sing-box's outbound rather
// than mistaking a directly reachable test server for a successful proxy test.
func latencyTestNode(t *testing.T, key string, hits *atomic.Int32) ProbeTarget {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", 405)
			return
		}
		upstream, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			http.Error(w, "unavailable", 502)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		rw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n")
		rw.Flush()
		go func() { defer conn.Close(); defer upstream.Close(); io.Copy(upstream, rw) }()
		go func() { defer conn.Close(); defer upstream.Close(); io.Copy(conn, upstream) }()
	}))
	t.Cleanup(server.Close)
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	return ProbeTarget{Key: key, Outbound: map[string]any{"type": "http", "server": host, "server_port": number}}
}
func latencyTestCore(t *testing.T) string {
	t.Helper()
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	return core
}
func TestLatencyProberRoutesEachNodeAndRejectsFailure(t *testing.T) {
	core := latencyTestCore(t)
	var requests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer origin.Close()
	var hitsA, hitsB atomic.Int32
	a, b := latencyTestNode(t, "a", &hitsA), latencyTestNode(t, "b", &hitsB)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	bad := ProbeTarget{Key: "bad", Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": closedPort}}
	config, err := probeConfig([]ProbeTarget{a, b, bad}, 2081, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckConfiguration(context.Background(), core, config); err != nil {
		t.Fatal(err)
	}
	results := map[string]LatencyResult{}
	var mu sync.Mutex
	prober := LatencyProber{CorePath: core, URL: origin.URL, Timeout: time.Second, Parallelism: 3}
	if err := prober.Run(context.Background(), []ProbeTarget{a, b, bad}, func(key string, result LatencyResult) { mu.Lock(); defer mu.Unlock(); results[key] = result }); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b"} {
		if result, ok := results[key]; !ok || result.Err != nil || result.Duration <= 0 {
			t.Fatalf("%s: %+v", key, result)
		}
	}
	if results["bad"].Err == nil {
		t.Fatal("failed node silently bypassed proxy")
	}
	if hitsA.Load() != 1 || hitsB.Load() != 1 || requests.Load() != 2 {
		t.Fatal("requests did not follow per-user node routes", hitsA.Load(), hitsB.Load(), requests.Load())
	}
}
func TestLatencyProberCancellationTimeoutAndHTTPStatus(t *testing.T) {
	core := latencyTestCore(t)
	for _, tc := range []struct {
		name   string
		status int
		wait   bool
		cancel bool
	}{
		{name: "unexpected response", status: 200},
		{name: "timeout", wait: true},
		{name: "cancel", wait: true, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				if tc.wait {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(tc.status)
			}))
			defer origin.Close()
			var hits atomic.Int32
			node := latencyTestNode(t, "a", &hits)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			result := make(chan LatencyResult, 1)
			started := time.Now()
			err := (LatencyProber{CorePath: core, URL: origin.URL, Timeout: 250 * time.Millisecond}).Run(ctx, []ProbeTarget{node}, func(_ string, r LatencyResult) { result <- r })
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation not propagated", err)
			}
			if !tc.cancel && err != nil {
				t.Fatal(err)
			}
			measured := <-result
			if measured.Err == nil {
				t.Fatal("failure was reported as latency")
			}
			if tc.wait && !tc.cancel && !errors.Is(measured.Err, context.DeadlineExceeded) {
				t.Fatal("timeout not identified", measured.Err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("cancellation/timeout did not promptly reap core")
			}
		})
	}
}
