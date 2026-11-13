package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/control"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestCLIControlsDaemonWithoutLaunchingUI(t *testing.T) {
	store := testutil.NewStore(t)
	if err := store.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	options := app.Options{Store: store, StateDir: t.TempDir(), CorePath: "/unused"}
	runtime := control.NewWithBackend(app.NewService(options), &cliBackend{})
	server, err := control.NewServer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("daemon still running")
		}
	}()
	launch := func(app.Options) error { t.Fatal("CLI launched Fyne"); return nil }
	for _, args := range [][]string{
		{"status", "--json"}, {"--json", "daemon", "status"}, {"mode", "direct", "--json"},
		{"policy", "list", "--json"}, {"policy", "select", "Available", profile.NodeKey("manual", "a"), "--json"},
		{"connect", "--json"}, {"disconnect", "--json"},
	} {
		flags := append([]string{"--profiles", store.Dir, "--state-dir", options.StateDir}, args...)
		var output bytes.Buffer
		if err := Run(flags, &output, launch); err != nil {
			t.Fatal(args, err)
		}
		var value control.Envelope
		if err := json.Unmarshal(output.Bytes(), &value); err != nil || !value.OK || value.Status.PID == 0 {
			t.Fatal(args, output.String(), err)
		}
	}
	var output bytes.Buffer
	if err := Run([]string{"--profiles", store.Dir, "--state-dir", options.StateDir, "--json", "policy", "select", "Available", "missing"}, &output, launch); err == nil {
		t.Fatal("invalid policy did not fail")
	}
	var value control.Envelope
	if err := json.Unmarshal(output.Bytes(), &value); err != nil || value.OK || value.Error == "" {
		t.Fatal("error not machine readable", output.String())
	}
	output.Reset()
	if err := Run([]string{"--profiles", store.Dir, "--state-dir", options.StateDir, "-select-source", "manual", "-select-node", "a"}, &output, launch); err == nil || !strings.Contains(err.Error(), "后台") {
		t.Fatal("legacy writer bypassed daemon", err)
	}
}

type cliBackend struct{}

func (*cliBackend) Start(context.Context) error { return nil }
func (*cliBackend) Stop() error                 { return nil }

func TestStatusWithoutDaemonDoesNotInitializeProfiles(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	if err := Run([]string{"status", "--state-dir", t.TempDir(), "--json"}, &output, nil); err == nil {
		t.Fatal("missing daemon succeeded")
	}
	var response control.Envelope
	if err := json.Unmarshal(output.Bytes(), &response); err != nil || response.OK || response.Error == "" {
		t.Fatal("missing daemon is not machine readable", output.String())
	}
	if _, err := os.Stat("profiles"); !os.IsNotExist(err) {
		t.Fatal("status initialized profiles")
	}
}
