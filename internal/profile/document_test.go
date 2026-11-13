package profile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONEditorRejectsAmbiguousDocumentsAndLocatesErrors(t *testing.T) {
	for _, data := range []string{`[]`, `null`, `{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{"a":1} {}`, `{"a":1,}`, "{\n  \"中文\": 1,\n  \"b\":\n}"} {
		_, err := ParseDocument([]byte(data))
		if err == nil {
			t.Fatal("accepted invalid document", data)
		}
		var position *DocumentError
		if !errors.As(err, &position) || position.Line < 1 || position.Column < 1 {
			t.Fatal("missing location", err)
		}
	}
	_, err := ParseDocument([]byte("{\n  \"a\": true,\n  \"b\": }"))
	var position *DocumentError
	if !errors.As(err, &position) || position.Line != 3 {
		t.Fatal("wrong syntax error location", err)
	}
	formatted, err := FormatDocument([]byte(`{"id":18446744073709551615,"rules":[{"domain":"例子.cn"}]}`))
	if err != nil || !bytes.Contains(formatted, []byte("18446744073709551615")) || !bytes.Contains(formatted, []byte("例子.cn")) {
		t.Fatal("formatting changed values", err)
	}
}

func TestDocumentSaveValidatesAndBacksUpWithoutTouchingProfiles(t *testing.T) {
	s := testStore(t)
	path := filepath.Join(s.Dir, "config.json")
	original := snapshot(t, s, "config.json")
	other := map[string][]byte{}
	for _, name := range []string{"nodes.json", "subscriptions.json", "selection.json"} {
		other[name] = snapshot(t, s, name)
	}
	draft := []byte(`{"inbounds":[{"type":"mixed","listen":"127.0.0.1","listen_port":2080}],"dns":{"servers":[{"type":"local","tag":"dns-direct"}]},"route":{"final":"direct","rules":[{"domain_suffix":"example.com","outbound":"proxy"}]}}`)
	check := func(data []byte) error { _, err := s.CompileDocument("config.json", data); return err }
	saved, err := SaveDocument(path, draft, original, check)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, snapshot(t, s, "config.json")) {
		t.Fatal("saved different content")
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup lost original", err)
	}
	info, _ := os.Stat(path)
	backupInfo, _ := os.Stat(path + ".bak")
	if info.Mode().Perm() != 0600 || backupInfo.Mode().Perm() != 0600 {
		t.Fatal("configuration permissions are not private")
	}
	for name, want := range other {
		if !bytes.Equal(snapshot(t, s, name), want) {
			t.Fatal("changed unrelated profile", name)
		}
	}
	for _, data := range [][]byte{[]byte(`{"route":`), []byte(`{"route":{"final":"provider-node"}}`)} {
		if _, err := SaveDocument(path, data, saved, check); err == nil {
			t.Fatal("saved invalid configuration")
		}
		if !bytes.Equal(snapshot(t, s, "config.json"), saved) {
			t.Fatal("invalid draft replaced working config")
		}
	}
	if _, err := SaveDocument(path, draft, saved, func([]byte) error { return errors.New("core rejected option") }); err == nil {
		t.Fatal("ignored core rejection")
	}
	backup, _ = os.ReadFile(path + ".bak")
	if !bytes.Equal(backup, original) {
		t.Fatal("failed validation changed backup")
	}
}

func TestDocumentSaveDetectsExternalChangesAfterValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"level":"info"}`)
	external := []byte(`{"level":"debug"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := SaveDocument(path, []byte(`{"level":"warn"}`), original, func([]byte) error { return os.WriteFile(path, external, 0600) })
	if err == nil || !strings.Contains(err.Error(), "其他操作") {
		t.Fatal("failed to detect external change", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, external) {
		t.Fatal("overwrote external edit")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatal("created backup for rejected save")
	}
}

func TestDocumentValidationUsesAutomaticPolicyAndOriginalRulePaths(t *testing.T) {
	s := testStore(t)
	if err := s.SaveMode(ModeDirect); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.Dir, "nodes.json")); err != nil {
		t.Fatal(err)
	}
	config, err := s.CompileDocument("config.json", snapshot(t, s, "config.json"))
	if err != nil {
		t.Fatal("validation incorrectly requires a selected node", err)
	}
	route := config["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatal("direct mode hid saved policy errors")
	}
	set := route["rule_set"].([]any)[0].(map[string]any)
	if set["path"] != filepath.Join(s.Dir, "rule-sets", "cn.srs") {
		t.Fatal("rule path resolved relative to temporary directory")
	}
	if _, err := s.CompileDocument("config.json", []byte(`{"inbounds":[{"type":"mixed"}],"route":{"final":"invalid"}}`)); err == nil {
		t.Fatal("direct mode masked invalid policy")
	}
	if _, err := s.CompileDocument("nodes.json", []byte(`{}`)); err == nil {
		t.Fatal("editor overwrote node storage")
	}
}
