package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testBenchmarkOptions(value string) BenchmarkOptions {
	return BenchmarkOptions{URL: value, Requests: 11, Concurrency: 3, Timeout: time.Second, Method: "GET", DisableCompression: true}
}

func TestBenchmarkRoutesEachNodeCountsEveryRequestAndReadsBody(t *testing.T) {
	core := latencyTestCore(t)
	var hitsA, hitsB, active, peak, requests atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous && !peak.CompareAndSwap(previous, current); previous = peak.Load() {
		}
		requests.Add(1)
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(201)
		io.WriteString(w, "hello")
	}))
	defer origin.Close()
	a, b := latencyTestNode(t, "a", &hitsA), latencyTestNode(t, "b", &hitsB)
	targets := []BenchmarkTarget{{Key: a.Key, Name: "Node A", Outbound: a.Outbound}, {Key: b.Key, Name: "Node B", Outbound: b.Outbound}}
	options := testBenchmarkOptions(origin.URL)
	var progress []BenchmarkProgress
	report, err := (BenchmarkProber{CorePath: core}).Run(context.Background(), "test", options, nil, targets, func(value BenchmarkProgress) { progress = append(progress, value) })
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 2 || requests.Load() != 22 || peak.Load() > 3 || peak.Load() < 2 || hitsA.Load() == 0 || hitsB.Load() == 0 {
		t.Fatal("incorrect targets, count or concurrency", report, requests.Load(), peak.Load(), hitsA.Load(), hitsB.Load())
	}
	for _, result := range report.Results {
		if result.Completed != 11 || result.Success != 11 || result.Bytes != 55 || result.NetworkFailures != 0 || result.HTTPFailures != 0 || result.Average < 10*time.Millisecond || result.RequestsPerSecond <= 0 || result.P95 == 0 || !result.Finished || result.StatusCodes[201] != 11 {
			t.Fatal(result)
		}
	}
	last := progress[len(progress)-1]
	if last.ID != "test" || last.Index != 1 || len(last.Results) != 1 || last.Results[0].Key != "a" || !last.Result.Finished {
		t.Fatal("missing final/previous-node progress", last)
	}
	// Progress maps are immutable snapshots, not live aliases of the accumulator.
	if progress[0].Result.Completed != 0 || len(progress[0].Result.StatusCodes) != 0 {
		t.Fatal("past progress mutated")
	}
}

func TestBenchmarkCurrentRoutingRedirectAndStatusSemantics(t *testing.T) {
	core := latencyTestCore(t)
	var hits, next atomic.Int32
	node := latencyTestNode(t, "node", &hits)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			next.Add(1)
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/next", 302)
			return
		}
		w.WriteHeader(403)
		io.WriteString(w, "denied")
	}))
	defer origin.Close()
	config := routingTestConfig(node, []any{map[string]any{"ip_cidr": []string{"127.0.0.0/8"}, "action": "route", "outbound": "direct"}})
	before := config["experimental"]
	options := testBenchmarkOptions(origin.URL)
	options.Requests = 5
	for _, tc := range []struct {
		path          string
		follow        bool
		code, success int
	}{{"/", false, 403, 0}, {"/redirect", false, 302, 0}, {"/redirect", true, 204, 5}} {
		options.URL, options.FollowRedirects = origin.URL+tc.path, tc.follow
		report, err := (BenchmarkProber{CorePath: core}).Run(context.Background(), "routing", options, config, []BenchmarkTarget{{Key: "routing", Name: "Current"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result := report.Results[0]
		if result.StatusCodes[tc.code] != 5 || result.Success != tc.success || result.HTTPFailures != 5-tc.success || result.NetworkFailures != 0 || result.Completed != 5 {
			t.Fatal(result)
		}
	}
	if hits.Load() != 0 || next.Load() != 5 || !reflect.DeepEqual(before, config["experimental"]) {
		t.Fatal("ignored direct rule, followed redirects unexpectedly or changed config", hits.Load(), next.Load())
	}
}

func TestBenchmarkRateDurationTimeoutCancellationAndCleanup(t *testing.T) {
	core := latencyTestCore(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	var hits atomic.Int32
	node := latencyTestNode(t, "node", &hits)
	targets := []BenchmarkTarget{{Key: node.Key, Name: "Node", Outbound: node.Outbound}}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	options := testBenchmarkOptions(origin.URL)
	options.Requests, options.Concurrency, options.Rate = 6, 2, 10
	report, err := (BenchmarkProber{CorePath: core}).Run(context.Background(), "rate", options, nil, targets, nil)
	if err != nil || report.Results[0].Completed != 6 || report.Results[0].Elapsed < 180*time.Millisecond {
		t.Fatal(report, err)
	}
	options.Duration, options.Requests, options.Rate = 220*time.Millisecond, 1, 0
	report, err = (BenchmarkProber{CorePath: core}).Run(context.Background(), "duration", options, nil, targets, nil)
	if err != nil || report.Results[0].Completed <= 1 || report.Results[0].Planned != 0 || report.Cancelled || report.Results[0].Elapsed < 200*time.Millisecond {
		t.Fatal(report, err)
	}
	for _, cancelTest := range []bool{false, true} {
		entered := make(chan struct{}, 1)
		blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-r.Context().Done()
		}))
		options = testBenchmarkOptions(blocked.URL)
		options.Requests, options.Concurrency, options.Timeout = 1, 1, 80*time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		if cancelTest {
			go func() { <-entered; cancel() }()
		}
		report, err = (BenchmarkProber{CorePath: core}).Run(ctx, "blocked", options, nil, targets, nil)
		cancel()
		blocked.Close()
		if cancelTest {
			if !errors.Is(err, context.Canceled) || !report.Cancelled || len(report.Results) != 1 || report.Results[0].CancelledRequests != 1 {
				t.Fatal(report, err)
			}
		} else if err != nil || report.Results[0].NetworkFailures != 1 || report.Results[0].Errors["请求超时"] != 1 {
			t.Fatal(report, err)
		}
	}
	paths, _ := filepath.Glob(filepath.Join(temporary, "loop-benchmark-*"))
	if len(paths) != 0 {
		t.Fatal("core files leaked", paths)
	}
}

func TestBenchmarkDoesNotFallbackOnFailedNodeAndHonorsKeepAlive(t *testing.T) {
	core := latencyTestCore(t)
	var connections atomic.Int32
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	origin.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	origin.Start()
	defer origin.Close()
	var hits atomic.Int32
	node := latencyTestNode(t, "node", &hits)
	options := testBenchmarkOptions(origin.URL)
	options.Requests, options.Concurrency = 5, 1
	for _, disable := range []bool{false, true} {
		before := connections.Load()
		options.DisableKeepAlive = disable
		report, err := (BenchmarkProber{CorePath: core}).Run(context.Background(), "reuse", options, nil, []BenchmarkTarget{{Key: "node", Outbound: node.Outbound}}, nil)
		if err != nil || report.Results[0].Success != 5 {
			t.Fatal(report, err)
		}
		want := int32(1)
		if disable {
			want = 5
		}
		if connections.Load()-before != want {
			t.Fatal("keepalive ignored", disable, connections.Load()-before)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	before := connections.Load()
	report, err := (BenchmarkProber{CorePath: core}).Run(context.Background(), "failed", options, nil, []BenchmarkTarget{{Key: "bad", Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": port}}}, nil)
	if err != nil || report.Results[0].NetworkFailures != 5 || report.Results[0].Success != 0 || connections.Load() != before {
		t.Fatal("failed node bypassed proxy", report, err)
	}
}

func TestBenchmarkStatsBoundedPercentilesAndValidation(t *testing.T) {
	s := newBenchmarkStats(BenchmarkTarget{Key: "test"}, 100)
	for i := 1; i <= 100; i++ {
		s.add(benchmarkSample{duration: time.Duration(i) * time.Millisecond, status: 200, bytes: 10})
	}
	r := s.snapshot(time.Second, true)
	if r.Average != 50500*time.Microsecond || r.Fastest != time.Millisecond || r.Slowest != 100*time.Millisecond || r.P95 < 95*time.Millisecond || r.P95 > 96*time.Millisecond || r.BytesPerSecond != 1000 || r.RequestsPerSecond != 100 {
		t.Fatal(r)
	}
	valid := testBenchmarkOptions("example.com")
	for _, mutate := range []func(*BenchmarkOptions){func(o *BenchmarkOptions) { o.Requests = 1 }, func(o *BenchmarkOptions) { o.Concurrency = 0 }, func(o *BenchmarkOptions) { o.Timeout = 0 }, func(o *BenchmarkOptions) { o.Duration = -1 }, func(o *BenchmarkOptions) { o.Method = "POST" }, func(o *BenchmarkOptions) { o.Rate = -1 }, func(o *BenchmarkOptions) { o.URL = "file:///test" }} {
		options := valid
		mutate(&options)
		if _, err := options.Validated(); err == nil {
			t.Fatal("invalid parameters accepted", options)
		}
	}
	if value, err := valid.Validated(); err != nil || !strings.HasPrefix(value.URL, "https://") {
		t.Fatal(value, err)
	}
}
