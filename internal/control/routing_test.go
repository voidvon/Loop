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
)

func TestRoutingRPCPreservesConnectionAndCancelsOnClientDisconnect(t *testing.T) {
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
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			defer origin.Close()
			remote := app.NewService(app.Options{})
			remote.Remote = client
			result, err := remote.TestRouting(context.Background(), origin.URL)
			if err != nil || !result.Direct || result.StatusCode != 204 {
				t.Fatal(result, err)
			}
			entered := make(chan struct{})
			blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
			defer blocked.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := remote.TestRouting(ctx, blocked.URL); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("test request never started")
			}
			if !controller.Snapshot().Busy {
				t.Fatal("probe is not serialized")
			}
			cancel()
			if err := <-done; err == nil {
				t.Fatal("cancelled client returned success")
			}
			waitFor(t, func() bool { return !controller.Snapshot().Busy })
			state := controller.Snapshot()
			if state.Running != running || state.Error != "" {
				t.Fatal("probe changed connection state", state)
			}
			if starts, stops := backend.counts(); starts != 0 || stops != 0 {
				t.Fatal("probe restarted active backend", starts, stops)
			}
		})
	}
}
