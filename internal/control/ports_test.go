package control

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/inbound"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestLivePortSaveAcrossRPCSurvivesFrontendCancellation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "recover"}[fail], func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			backend := &testBackend{running: true, failNext: fail, entered: make(chan struct{}, 1), release: make(chan struct{})}
			c := NewWithBackend(app.NewService(app.Options{Store: s, CorePath: core, StateDir: t.TempDir()}), backend)
			client, _ := startTestServer(t, c)
			path := filepath.Join(s.Dir, profile.MainConfigName)
			before, _ := os.ReadFile(path)
			ports := inbound.DefaultPorts()
			ports.GlobalEnabled = false
			draft, err := profile.ChangeProxyPorts(before, ports)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := client.Execute(ctx, app.Operation{Action: "save_ports", Data: draft, Expected: before})
				finished <- err
			}()
			select {
			case <-backend.entered:
			case <-time.After(25 * time.Second):
				t.Fatal("online save never reached restart")
			}
			cancel()
			if err := <-finished; err == nil {
				t.Fatal("client cancellation didn't return")
			}
			close(backend.release)
			waitFor(t, func() bool { return !c.Snapshot().Busy })
			after, _ := os.ReadFile(path)
			if !c.Snapshot().Running || bytes.Equal(before, after) != fail {
				t.Fatal("lost transaction after frontend exited")
			}
			// A fresh frontend can edit the running daemon and receive full results.
			ports.GlobalEnabled = false
			ports.Global = 2099
			draft, _ = profile.ChangeProxyPorts(after, ports)
			result, err := client.Execute(context.Background(), app.Operation{Action: "save_ports", Data: draft, Expected: after})
			if err != nil || !result.Ports.Running || len(result.Ports.Document) == 0 {
				t.Fatal("RPC lost port save outcome", err)
			}
		})
	}
}
