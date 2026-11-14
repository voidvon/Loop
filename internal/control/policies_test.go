package control

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestLivePolicySaveAcrossRPCSurvivesFrontendCancellation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "recover"}[fail], func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			backend := &testBackend{running: true, failNext: fail, entered: make(chan struct{}, 1), release: make(chan struct{})}
			c := NewWithBackend(app.NewService(app.Options{Store: s, CorePath: core, StateDir: t.TempDir()}), backend)
			client, _ := startTestServer(t, c)
			path := filepath.Join(s.Dir, profile.MainConfigName)
			before, _ := os.ReadFile(path)
			config, _ := profile.ParseDocument(before)
			groups, _ := profile.PolicyGroups(config)
			groups = append(groups, profile.PolicyGroup{Name: "Custom", Builtins: []string{"direct", "reject"}})
			draft, err := profile.ChangePolicyGroups(before, groups)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := client.Execute(ctx, app.Operation{Action: "save_policies", Data: draft, Expected: before})
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
			config, _ = profile.ParseDocument(after)
			groups, _ = profile.PolicyGroups(config)
			groups = append(groups, profile.PolicyGroup{Name: "Another", Builtins: []string{"direct"}})
			draft, _ = profile.ChangePolicyGroups(after, groups)
			result, err := client.Execute(context.Background(), app.Operation{Action: "save_policies", Data: draft, Expected: after})
			if err != nil || !result.Policies.Running || len(result.Policies.Document) == 0 {
				t.Fatal("RPC lost policy save outcome", err)
			}
		})
	}
}
