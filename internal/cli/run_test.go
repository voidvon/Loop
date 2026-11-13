package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestArgumentsDoNotLaunchOrInitializeProfiles(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		wantError bool
	}{
		{"help", []string{"-help"}, false},
		{"unknown", []string{"-unknown"}, true},
		{"positional", []string{"unexpected"}, true},
		{"incomplete selection", []string{"-select-source", "manual"}, true},
		{"conflicting commands", []string{"-list-nodes", "-render-config", "unused.json"}, true},
		{"raw with profiles", []string{"-config", "raw.json"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "uninitialized")
			args := append([]string{"-profiles", dir}, tc.args...)
			var output bytes.Buffer
			err := Run(args, &output, func(app.Options) error {
				t.Fatal("argument handling launched the GUI")
				return nil
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError = %v", err, tc.wantError)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("argument handling initialized profiles: %v", err)
			}
			if !tc.wantError && !strings.Contains(output.String(), "-list-nodes") {
				t.Fatal("help did not include management commands")
			}
		})
	}
}

func TestManagementCommandsStayHeadless(t *testing.T) {
	s := testutil.NewStore(t)
	stateDir := t.TempDir()
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	launch := func(app.Options) error {
		t.Fatal("management command launched the GUI")
		return nil
	}
	var output bytes.Buffer
	if err := Run([]string{"-state-dir", stateDir, "-profiles", s.Dir, "-list-nodes"}, &output, launch); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "* manual\ta\t") {
		t.Fatalf("missing current node: %q", output.String())
	}
	if err := s.SaveManual(profile.Node{ID: "b", Name: "B", Outbound: map[string]any{"type": "trojan", "server": "example.org", "server_port": 443, "password": "test"}}); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(s.Dir, profile.MainConfigName)
	original, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"-state-dir", stateDir, "-profiles", s.Dir, "-select-source", "manual", "-select-node", "b"}, &output, launch); err != nil {
		t.Fatal(err)
	}
	selection, err := s.Selection()
	if err != nil || selection.Node != "b" {
		t.Fatalf("selection = %+v, error = %v", selection, err)
	}
	rendered := filepath.Join(t.TempDir(), "runtime.json")
	if err := Run([]string{"-state-dir", stateDir, "-profiles", s.Dir, "-render-config", rendered}, &output, launch); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(rendered)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Outbounds []struct{ Tag, Server, Default string }
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	selected := profile.NodeKey("manual", "b")
	nodeOK, groupOK := false, false
	for _, out := range config.Outbounds {
		nodeOK = nodeOK || (out.Tag == selected && out.Server == "example.org")
		groupOK = groupOK || (out.Tag == profile.DefaultPolicy && out.Default == selected)
	}
	if !nodeOK || !groupOK {
		t.Fatal("Available did not resolve to the CLI selected node")
	}

	after, err := os.ReadFile(policyPath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("node management changed the main policy")
	}
}

func TestLaunchReceivesOptionsAndReturnsError(t *testing.T) {
	s := testutil.NewStore(t)
	sentinel := errors.New("launcher failed")
	called := false
	err := Run([]string{"-profiles", s.Dir, "-core", "/test/core", "-tun-helper", "/test/helper"}, &bytes.Buffer{}, func(options app.Options) error {
		called = true
		if options.Store.Dir != s.Dir || options.CorePath != "/test/core" || options.HelperPath != "/test/helper" || options.ConfigPath != "" || options.StateDir == "" || options.Executable == "" {
			t.Fatalf("unexpected launch options: %+v", options)
		}
		return sentinel
	})
	if !called || !errors.Is(err, sentinel) {
		t.Fatalf("called = %v, error = %v", called, err)
	}
}

func TestRawConfigurationDoesNotRequireProfiles(t *testing.T) {
	t.Chdir(t.TempDir())
	called := false
	err := Run([]string{"-config", "raw.json"}, &bytes.Buffer{}, func(options app.Options) error {
		called = true
		if !filepath.IsAbs(options.ConfigPath) || filepath.Base(options.ConfigPath) != "raw.json" || options.Store.Dir != "" {
			t.Fatalf("raw config initialized a profile store: %+v", options)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("called = %v, error = %v", called, err)
	}
	if _, err := os.Stat("profiles"); !os.IsNotExist(err) {
		t.Fatalf("raw mode created profiles: %v", err)
	}
}
