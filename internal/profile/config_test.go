package profile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func legacyStore(t *testing.T) Store {
	t.Helper()
	s := testStore(t)
	config, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{}
	for _, key := range []string{"proxy_ports", "log", "experimental"} {
		if value, ok := config[key]; ok {
			base[key] = value
			delete(config, key)
		}
	}
	if err := WriteJSON(filepath.Join(s.Dir, "base.json"), base); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(filepath.Join(s.Dir, "routing.json"), config); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.Dir, MainConfigName)); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMainConfigMigrationPreservesSourcesAndCompiledBehavior(t *testing.T) {
	s := legacyStore(t)
	before, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{}
	for _, name := range []string{"base.json", "routing.json", "nodes.json", "subscriptions.json", "selection.json"} {
		originals[name] = snapshot(t, s, name)
	}
	if err := s.EnsureMainConfig(); err != nil {
		t.Fatal(err)
	}
	after, err := s.Compile()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration changed compiled settings", err)
	}
	info, err := os.Stat(filepath.Join(s.Dir, MainConfigName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("main config is not private", err)
	}
	for name, want := range originals {
		if !bytes.Equal(snapshot(t, s, name), want) {
			t.Fatal("migration altered original", name)
		}
	}
	// Once migrated, even broken legacy backups must have no effect.
	os.WriteFile(filepath.Join(s.Dir, "routing.json"), []byte(`broken legacy backup`), 0600)
	if err := s.EnsureMainConfig(); err != nil {
		t.Fatal(err)
	}
	again, err := s.Compile()
	if err != nil || !reflect.DeepEqual(after, again) {
		t.Fatal("legacy file overrode main source", err)
	}
}

func TestMigrationKeepsInvalidSourcesAndExistingMainFile(t *testing.T) {
	s := legacyStore(t)
	broken := []byte(`{"route":`)
	os.WriteFile(filepath.Join(s.Dir, "routing.json"), broken, 0600)
	if err := s.EnsureMainConfig(); err == nil {
		t.Fatal("migration accepted invalid legacy JSON")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, MainConfigName)); !os.IsNotExist(err) {
		t.Fatal("failed migration published a main config")
	}
	if !bytes.Equal(snapshot(t, s, "routing.json"), broken) {
		t.Fatal("failed migration altered original")
	}
	// Invalid main files remain editable and are never silently replaced.
	os.WriteFile(filepath.Join(s.Dir, MainConfigName), broken, 0600)
	if err := s.EnsureMainConfig(); err != nil {
		t.Fatal("existing main file was replaced/rejected during migration", err)
	}
	if _, err := s.Config(); err == nil {
		t.Fatal("invalid main fell back to legacy sources")
	}
	if !bytes.Equal(snapshot(t, s, MainConfigName), broken) {
		t.Fatal("existing main file overwritten")
	}
}

func TestMainConfigRejectsEmbeddedNodesAndPreservesLargeNumbers(t *testing.T) {
	s := testStore(t)
	config, _ := s.Config()
	config["outbounds"] = []any{map[string]any{"type": "direct"}}
	data, _ := json.Marshal(config)
	if _, err := s.CompileDocument(MainConfigName, data); err == nil {
		t.Fatal("main source accepted managed outbounds")
	}
	delete(config, "outbounds")
	config["experimental"] = map[string]any{"cache_file": map[string]any{"cache_id": json.Number("18446744073709551615")}}
	if err := WriteJSON(filepath.Join(s.Dir, MainConfigName), config); err != nil {
		t.Fatal(err)
	}
	compiled, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(compiled)
	if !bytes.Contains(encoded, []byte("18446744073709551615")) {
		t.Fatal("compilation rounded a main configuration value")
	}
}
