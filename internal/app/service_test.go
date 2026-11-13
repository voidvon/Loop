package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestConfigSaveChecksActualCoreBeforeReplacingSource(t *testing.T) {
	s, core := testutil.CoreStore(t)
	service := NewService(Options{Store: s, CorePath: core})
	path := filepath.Join(s.Dir, profile.MainConfigName)
	original, _ := os.ReadFile(path)
	draft := bytes.Replace(original, []byte(`"strategy": "prefer_ipv4"`), []byte(`"strategy": "ipv4_only"`), 1)
	saved, err := service.SaveDocument(context.Background(), draft, original)
	if err != nil {
		t.Fatal(err)
	}
	invalid := bytes.Replace(saved, []byte(`"action": "resolve"`), []byte(`"action": "unknown-action"`), 1)
	if bytes.Equal(invalid, saved) {
		t.Fatal("fixture did not contain resolve action")
	}
	if _, err := service.SaveDocument(context.Background(), invalid, saved); err == nil {
		t.Fatal("core accepted invalid routing action")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, saved) {
		t.Fatal("core rejection overwrote valid configuration")
	}
}

func TestCancelledSaveDoesNotTouchSourceOrCreateBackup(t *testing.T) {
	s := testutil.NewStore(t)
	service := NewService(Options{Store: s, CorePath: "/unused/core"})
	path := filepath.Join(s.Dir, profile.MainConfigName)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.SaveDocument(ctx, original, original); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("cancelled save changed the source")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("cancelled save created a backup: %v", err)
	}
}

func TestValidateNodesChecksEveryCandidateBeforeCallingCore(t *testing.T) {
	s := testutil.NewStore(t)
	service := NewService(Options{Store: s, CorePath: "/unused/core"})
	nodes := []profile.Node{
		{ID: "a", Name: "A", Outbound: map[string]any{"type": "trojan", "server": "example.com", "server_port": 443, "password": "test"}},
		{ID: "b", Name: "B", Outbound: map[string]any{"type": "direct"}},
	}
	err := service.ValidateNodes(context.Background(), nodes)
	if err == nil {
		t.Fatal("accepted a direct outbound as a subscription node")
	}
	want := profile.ValidateNode(nodes[1])
	if want == nil || err.Error() != want.Error() {
		t.Fatalf("called the core before validating all nodes: %v", err)
	}
}
