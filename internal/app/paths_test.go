package app

import (
	"bytes"

	"local/miniproxy/internal/profile"
	"os"
	"path/filepath"
	"testing"
)

func TestAppUpdatesPreserveUserProfiles(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "MiniProxy.app", "Contents", "MacOS", "MiniProxy")
	bundled := filepath.Join(root, "MiniProxy.app", "Contents", "Resources", "profiles")
	state := filepath.Join(root, "state")
	os.MkdirAll(bundled, 0700)
	os.WriteFile(filepath.Join(bundled, "config.json"), []byte("bundled policy"), 0600)
	dir, err := ProfileDirectory(executable, state, "")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("my custom policy"), 0600)
	os.WriteFile(filepath.Join(bundled, "config.json"), []byte("new bundled policy"), 0600)
	if _, err := ProfileDirectory(executable, state, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if string(data) != "my custom policy" {
		t.Fatal("application update overwrote personal policy")
	}
}

func TestAppUpdateMigratesUserSourcesBeforeSeedingDefaultMain(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "MiniProxy.app", "Contents", "MacOS", "MiniProxy")
	bundled := filepath.Join(root, "MiniProxy.app", "Contents", "Resources", "profiles")
	state := filepath.Join(root, "state")
	target := filepath.Join(state, "profiles")
	os.MkdirAll(bundled, 0700)
	os.MkdirAll(target, 0700)
	os.WriteFile(filepath.Join(bundled, "config.json"), []byte(`{"route":{"final":"proxy"}}`), 0600)
	os.WriteFile(filepath.Join(target, "base.json"), []byte(`{"log":{"level":"debug"},"inbounds":[]}`), 0600)
	os.WriteFile(filepath.Join(target, "routing.json"), []byte(`{"route":{"final":"direct"}}`), 0600)
	dir, err := ProfileDirectory(executable, state, "")
	if err != nil {
		t.Fatal(err)
	}
	config, err := (profile.Store{Dir: dir}).Config()
	if err != nil {
		t.Fatal(err)
	}
	if config["route"].(map[string]any)["final"] != "direct" || config["log"].(map[string]any)["level"] != "debug" {
		t.Fatal("bundle default replaced user's legacy settings")
	}
	// Later upgrades preserve the migrated main file and any further edits.
	before, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	os.WriteFile(filepath.Join(target, "routing.json"), []byte(`broken old backup`), 0600)
	if _, err := ProfileDirectory(executable, state, ""); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("second upgrade overwrote migrated main file")
	}
}
