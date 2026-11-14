package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

func TestBenchmarkRPCProgressCancellationAndConnectionPreservation(t *testing.T) {
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnected", true: "connected"}[running], func(t *testing.T) {
			backend := &testBackend{running: running}
			controller := newTestController(t, backend)
			controller.Service.CorePath = core
			if err := controller.Service.Store.SaveMode(profile.ModeDirect); err != nil {
				t.Fatal(err)
			}
			client, _ := startTestServer(t, controller)
			remote := app.NewService(app.Options{})
			remote.Remote = client
			entered := make(chan struct{}, 3)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }))
			defer origin.Close()
			request := app.BenchmarkRequest{ID: "benchmark-job", Options: proxy.BenchmarkOptions{URL: origin.URL, Requests: 11, Concurrency: 3, Timeout: time.Second}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := remote.RunBenchmark(ctx, request, nil); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request never entered origin")
			}
			state, err := client.Status(context.Background())
			if err != nil || !state.Busy || state.Benchmark == nil || state.Benchmark.ID != "benchmark-job" {
				t.Fatal(state, err)
			}
			if _, err := controller.Execute(context.Background(), app.Operation{Action: "set_mode", Mode: profile.ModeGlobal}); err != ErrBusy {
				t.Fatal("benchmark did not serialize mutations", err)
			}
			cancel()
			if err := <-done; err == nil {
				t.Fatal("cancelled client returned success")
			}
			waitFor(t, func() bool { return !controller.Snapshot().Busy })
			state = controller.Snapshot()
			if state.Running != running || state.Error != "" || state.Benchmark == nil || !state.Benchmark.Result.Finished || state.Benchmark.Result.CancelledRequests == 0 {
				t.Fatal("lost cancellation result or changed connection", state)
			}
			if starts, stops := backend.counts(); starts != 0 || stops != 0 {
				t.Fatal("restarted active proxy", starts, stops)
			}
		})
	}
}
