package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/testutil"
)

func TestStrategyProbeTargetsResolveDeduplicateWithoutWrites(t *testing.T) {
	s := testutil.NewStore(t)
	if err := s.SaveManual(profile.Node{ID: "hk", Name: "HK test", Outbound: map[string]any{"type": "trojan", "server": "example.org", "server_port": 443, "password": "test"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SelectStrategy("YouTube", "HK"); err != nil {
		t.Fatal(err)
	}
	// Include every stored file, including selection files, in the write check.
	before := map[string][]byte{}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		before[entry.Name()], err = os.ReadFile(filepath.Join(s.Dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(Options{Store: s})
	targets, err := service.StrategyProbeTargets("YouTube")
	if err != nil {
		t.Fatal(err)
	}
	// Available -> a, HK -> hk, then the same two literal nodes; other countries empty.
	if len(targets) != 2 || targets[0].Key != profile.NodeKey("manual", "a") || targets[1].Key != profile.NodeKey("manual", "hk") || targets[1].Outbound["server"] != "example.org" {
		t.Fatal("group references weren't resolved and deduplicated", targets)
	}
	for _, group := range []string{"JP", "missing"} {
		if _, err := service.StrategyProbeTargets(group); err == nil {
			t.Fatalf("%s must not fall back to a different node", group)
		}
	}
	entries, err = os.ReadDir(s.Dir)
	if err != nil || len(entries) != len(before) {
		t.Fatal("probing created files", err)
	}
	for name, data := range before {
		after, err := os.ReadFile(filepath.Join(s.Dir, name))
		if err != nil || !bytes.Equal(data, after) {
			t.Fatalf("probing changed %s: %v", name, err)
		}
	}
}
