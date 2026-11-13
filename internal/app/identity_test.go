package app

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRenameImportsPersistentDataWithoutRuntimeState(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, legacyName)
	current := filepath.Join(root, Name)
	files := map[string]string{
		"profiles/config.json":            `{"route":{"final":"direct"}}`,
		"profiles/nodes.json":             `{"nodes":[]}`,
		"profiles/subscriptions.json":     `{"subscriptions":[]}`,
		"profiles/selection.json":         `{"source":"manual","node":"a"}`,
		"profiles/policy-selections.json": `{"YouTube":"Available"}`,
		"profiles/settings.json":          `{"mode":"direct"}`,
		"profiles/rule-sets/custom.srs":   "synthetic rules",
		"desktop.json":                    `{"tray":{"show_mode_letter":false}}`,
		"proxy-state.json":                "must not inherit",
		"proxy.lock":                      "old lock",
		"runtime/config.json":             "old runtime",
		"sing-box.log":                    "old log",
	}
	for name, data := range files {
		path := filepath.Join(old, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Go(func() { failures <- migrateLegacyState(root) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		target := filepath.Join(current, name)
		if name == "desktop.json" || len(name) > 9 && name[:9] == "profiles/" {
			got, err := os.ReadFile(target)
			if err != nil || string(got) != data {
				t.Fatalf("missing persisted %s: %v", name, err)
			}
			info, _ := os.Stat(target)
			if info.Mode().Perm() != 0600 {
				t.Fatal("private data permissions changed", name)
			}
		} else if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("inherited runtime state", name)
		}
		original, err := os.ReadFile(filepath.Join(old, name))
		if err != nil || string(original) != data {
			t.Fatal("legacy data changed", name)
		}
	}
	config := filepath.Join(current, "profiles/config.json")
	replacement := []byte(`{"route":{"final":"Available"}}`)
	if err := os.WriteFile(config, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyState(root); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(config)
	if !bytes.Equal(got, replacement) {
		t.Fatal("second launch overwrote loop configuration")
	}
}

func TestRenamePreservesExistingProfilesAndPreferences(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{legacyName, Name} {
		dir := filepath.Join(root, name, "profiles")
		os.MkdirAll(dir, 0700)
		os.WriteFile(filepath.Join(dir, "config.json"), []byte(name), 0600)
		os.WriteFile(filepath.Join(root, name, "desktop.json"), []byte(name), 0600)
	}
	if err := migrateLegacyState(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"profiles/config.json", "desktop.json"} {
		got, _ := os.ReadFile(filepath.Join(root, Name, name))
		if string(got) != Name {
			t.Fatal("overwrote existing loop data")
		}
	}
	fyneRoot := filepath.Join(root, "fyne")
	legacyPrefs := filepath.Join(fyneRoot, legacyID, "preferences.json")
	os.MkdirAll(filepath.Dir(legacyPrefs), 0700)
	os.WriteFile(legacyPrefs, []byte(`{"appearance":"dark","menu-bar.mode-letter":false}`), 0600)
	if err := migrateDesktopPreferences(fyneRoot); err != nil {
		t.Fatal(err)
	}
	imported := filepath.Join(fyneRoot, ID, "preferences.json")
	before, _ := os.ReadFile(legacyPrefs)
	after, _ := os.ReadFile(imported)
	if !bytes.Equal(before, after) {
		t.Fatal("theme or menu preferences lost")
	}
	os.WriteFile(imported, []byte(`{"appearance":"light"}`), 0600)
	if err := migrateDesktopPreferences(fyneRoot); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(imported)
	if string(after) != `{"appearance":"light"}` {
		t.Fatal("overwrote current preferences")
	}
}

func TestRenameWithoutLegacyDataAndCustomStateStayEmpty(t *testing.T) {
	root := t.TempDir()
	if err := migrateLegacyState(root); err != nil {
		t.Fatal(err)
	}
	if err := migrateDesktopPreferences(filepath.Join(root, "fyne")); err != nil {
		t.Fatal(err)
	}
	if err := migrateInstalledState(filepath.Join(root, "custom-state")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("initialized data without legacy files", err)
	}
}

func TestFailedRenameImportDoesNotPublishPartialProfiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, legacyName, "profiles")
	os.MkdirAll(source, 0700)
	os.WriteFile(filepath.Join(source, "config.json"), []byte("keep"), 0600)
	if err := os.Symlink("missing", filepath.Join(source, "unavailable")); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyState(root); err == nil {
		t.Fatal("partial import reported success")
	}
	if _, err := os.Stat(filepath.Join(root, Name, "profiles")); !os.IsNotExist(err) {
		t.Fatal("published partial profile copy")
	}
	if _, err := os.Stat(filepath.Join(root, Name, ".legacy-imported")); !os.IsNotExist(err) {
		t.Fatal("failed import marked complete")
	}
}
