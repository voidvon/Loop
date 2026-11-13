// Package testutil provides private, synthetic fixtures shared by client tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"local/loop/internal/profile"
)

func NewStore(t *testing.T) profile.Store {
	t.Helper()
	s := profile.Store{Dir: t.TempDir()}
	values := map[string]any{
		profile.MainConfigName: map[string]any{
			"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": 2080}},
			"route":    map[string]any{"final": "proxy"},
		},
		"nodes.json":         profile.NodeList{Nodes: []profile.Node{{ID: "a", Name: "A", Outbound: map[string]any{"type": "trojan", "server": "example.com", "server_port": 443, "password": "test"}}}},
		"subscriptions.json": profile.Subscriptions{},
		"selection.json":     profile.Selection{Source: "manual", Node: "a"},
	}
	for name, value := range values {
		if err := profile.WriteJSON(filepath.Join(s.Dir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// CoreStore uses public default policy/rule sets and synthetic node credentials.
// Paths resolve from this source file so moving tests never reads user profiles.
func CoreStore(t *testing.T) (profile.Store, string) {
	t.Helper()
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	s := NewStore(t)
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test fixtures")
	}
	defaults := filepath.Join(filepath.Dir(source), "..", "..", "profiles")
	for _, name := range []string{profile.MainConfigName, "rule-sets/geosite-cn.srs", "rule-sets/geoip-cn.srs"} {
		data, err := os.ReadFile(filepath.Join(defaults, name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return s, core
}
