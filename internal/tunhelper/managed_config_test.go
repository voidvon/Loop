package tunhelper

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestManagedConfigBundlesUserReadableSnapshots(t *testing.T) {
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "snapshot.srs")
	if err := os.WriteFile(snapshot, []byte("test snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	config := managedTestConfig()
	config["log"] = map[string]any{"output": "/untrusted/root/log"}
	config["experimental"] = map[string]any{"cache_file": map[string]any{"path": "/untrusted/root/cache"}}
	config["route"].(map[string]any)["rule_set"] = []any{map[string]any{"type": "local", "tag": "test", "format": "binary", "path": snapshot}}
	data, _ := json.Marshal(config)
	req, err := BundleConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Resources) != 1 || strings.Contains(string(req.Config), snapshot) {
		t.Fatal("ordinary file path leaked into privileged request")
	}
	state := t.TempDir()
	prepared, err := prepareManagedConfig(req, state)
	if err != nil {
		t.Fatal(err)
	}
	set := prepared["route"].(map[string]any)["rule_set"].([]any)[0].(map[string]any)
	path := set["path"].(string)
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "test snapshot" || filepath.Dir(path) != state {
		t.Fatal("snapshot was not materialized privately", err)
	}
	encoded, _ := json.Marshal(prepared)
	if strings.Contains(string(encoded), "/untrusted/root") {
		t.Fatal("root log/cache path was accepted")
	}
}

func TestManagedConfigRejectsExecutableAndExternalFileFeatures(t *testing.T) {
	for _, name := range []string{"outbound", "plugin", "certificate", "rule path", "resource escape", "extra inbound", "listener", "endpoint"} {
		t.Run(name, func(t *testing.T) {
			config := managedTestConfig()
			out := config["outbounds"].([]any)[0].(map[string]any)
			switch name {
			case "outbound":
				out["type"] = "ssh"
			case "plugin":
				out["plugin"] = "/bin/sh"
			case "certificate":
				out["tls"] = map[string]any{"certificate_path": "/etc/master.passwd"}
			case "rule path":
				config["route"].(map[string]any)["rule_set"] = []any{map[string]any{"path": "/etc/master.passwd"}}
			case "resource escape":
				config["route"].(map[string]any)["rule_set"] = []any{map[string]any{"path": resourcePrefix + "../secret"}}
			case "extra inbound":
				config["inbounds"] = append(config["inbounds"].([]any), map[string]any{"type": "direct"})
			case "listener":
				config["inbounds"].([]any)[0].(map[string]any)["listen"] = "0.0.0.0"
			case "endpoint":
				config["endpoints"] = []any{}
			}
			data, _ := json.Marshal(config)
			if _, err := prepareManagedConfig(Request{Config: data}, t.TempDir()); err == nil {
				t.Fatal("unsafe privileged feature accepted")
			}
		})
	}
}

func TestManagedConfigChecksRealProfilesInAllModes(t *testing.T) {
	s, core := testutil.CoreStore(t)
	if err := s.SaveTUN(true); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []profile.Mode{profile.ModeRule, profile.ModeDirect, profile.ModeGlobal} {
		config, err := s.CompileMode(mode)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(config)
		req, err := BundleConfig(data)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		prepared, err := prepareManagedConfig(req, dir)
		if err != nil {
			t.Fatal(mode, err)
		}
		path := filepath.Join(dir, "config.json")
		if err := profile.WriteJSON(path, prepared); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(core, "check", "-c", path).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", mode, err, output)
		}
	}
}
