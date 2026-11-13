package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/proxy"
	"local/miniproxy/internal/testutil"
)

func TestStrategySwitchReconnectAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name              string
		failures          []error
		want              string
		starts            int
		running, hasError bool
	}{
		{"success", nil, "b", 1, true, false},
		{"rollback", []error{errors.New("failed"), nil}, "a", 2, true, true},
		{"rollback fails", []error{errors.New("new failed"), errors.New("old failed")}, "a", 2, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testutil.NewStore(t)
			node := profile.Node{ID: "b", Name: "B", Outbound: map[string]any{"type": "trojan", "server": "example.org", "server_port": 443, "password": "test"}}
			if err := s.SaveManual(node); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
			backend := &modeTestBackend{store: s, startErrors: tc.failures}
			running, err := SwitchStrategy(context.Background(), backend, s, profile.DefaultPolicy, profile.NodeKey("manual", "b"), true)
			selected, _ := s.Selection()
			if running != tc.running || (err != nil) != tc.hasError || selected.Node != tc.want || len(backend.starts) != tc.starts {
				t.Fatal(running, err, selected, backend.starts)
			}
			after, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
			if !bytes.Equal(before, after) {
				t.Fatal("strategy switch changed rules")
			}
		})
	}
}
func TestStrategySwitchOfflineAndFailedPreflight(t *testing.T) {
	s := testutil.NewStore(t)
	backend := &modeTestBackend{store: s}
	if running, err := SwitchStrategy(context.Background(), backend, s, "Telegram", profile.NodeKey("manual", "a"), false); running || err != nil {
		t.Fatal(running, err)
	}
	if backend.stops != 0 || len(backend.starts) != 0 {
		t.Fatal("offline selection touched backend")
	}
	before, _ := s.StrategyChoice("Telegram")
	if running, err := SwitchStrategy(context.Background(), backend, s, "Telegram", "JP", true); !running || err == nil {
		t.Fatal(running, err)
	}
	after, _ := s.StrategyChoice("Telegram")
	if before != after || backend.stops != 0 {
		t.Fatal("failed preflight modified live state")
	}
}
func TestExplicitFirstStrategyChoiceIsPinnedWithoutRestart(t *testing.T) {
	s := testutil.NewStore(t)
	n := profile.Node{ID: "jp", Name: "日本 JP", Outbound: map[string]any{"type": "trojan", "server": "example.org", "server_port": 443, "password": "test"}}
	if err := s.SaveManual(n); err != nil {
		t.Fatal(err)
	}
	backend := &modeTestBackend{store: s}
	if running, err := SwitchStrategy(context.Background(), backend, s, "JP", profile.NodeKey("manual", "jp"), true); !running || err != nil {
		t.Fatal(running, err)
	}
	if backend.stops != 0 || len(backend.starts) != 0 {
		t.Fatal("same selection restarted backend")
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "policy-selections.json"))
	if err != nil || !bytes.Contains(data, []byte(profile.NodeKey("manual", "jp"))) {
		t.Fatal("explicit first member was not persisted", err)
	}
}

func TestStrategyRuntimeCoreChecks(t *testing.T) {
	s, core := testutil.CoreStore(t)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	extra := profile.Node{ID: "jp", Name: "日本 JP", Outbound: map[string]any{"type": "trojan", "server": "example.org", "server_port": 443, "password": "test"}}
	if err := s.SaveManual(extra); err != nil {
		t.Fatal(err)
	}
	if err := s.SelectStrategy("YouTube", "JP"); err != nil {
		t.Fatal(err)
	}
	for _, tun := range []bool{false, true} {
		if err := s.SaveTUN(tun); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []profile.Mode{profile.ModeDirect, profile.ModeRule, profile.ModeGlobal} {
			config, err := s.CompileMode(mode)
			if err != nil {
				t.Fatal(mode, tun, err)
			}
			if err := proxy.CheckConfiguration(context.Background(), core, config); err != nil {
				t.Fatal(mode, tun, err)
			}
		}
	}
	// All imported candidate nodes are checked even with group-aware routing.
	service := NewService(Options{Store: s, CorePath: core})
	bad := profile.Node{ID: "bad", Name: "Bad", Outbound: map[string]any{"type": "trojan", "server": "example.org", "server_port": "invalid", "password": "test"}}
	if err := service.ValidateNodes(context.Background(), []profile.Node{extra, bad}); err == nil {
		t.Fatal("candidate validation skipped later nodes")
	}
}
