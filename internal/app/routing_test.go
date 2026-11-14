package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestRoutingUsesSavedModeAndNamesWithoutWritingProfiles(t *testing.T) {
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	s := testutil.NewStore(t)
	// An unreachable HTTP node still reveals the exact selected node on failure.
	nodes := profile.NodeList{Nodes: []profile.Node{{ID: "a", Name: "Selected Node", Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": 1}}}}
	if err := profile.WriteJSON(filepath.Join(s.Dir, "nodes.json"), nodes); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"inbounds": []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080}},
		"dns":      map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "local"}}, "final": "local"},
		"route":    map[string]any{"final": "Available", "default_domain_resolver": "local"},
	}
	if err := profile.WriteJSON(filepath.Join(s.Dir, profile.MainConfigName), config); err != nil {
		t.Fatal(err)
	}
	service := NewService(Options{Store: s, CorePath: core})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	for _, mode := range []profile.Mode{profile.ModeRule, profile.ModeDirect, profile.ModeGlobal} {
		if err := s.SaveMode(mode); err != nil {
			t.Fatal(err)
		}
		before := routingStoreFiles(t, s.Dir)
		result, err := service.TestRouting(context.Background(), origin.URL)
		if err != nil || result.Mode != mode.Label() {
			t.Fatal(result, err)
		}
		if mode == profile.ModeRule {
			if result.Node != "Selected Node" || result.Source != "手动节点" || result.Error == "" || !reflect.DeepEqual(result.Chain, []string{"Available", "Selected Node"}) {
				t.Fatal("lost node mapping", result)
			}
		} else if !result.Direct || result.StatusCode != 204 || result.Error != "" {
			t.Fatal("mode ignored", result)
		}
		if !reflect.DeepEqual(before, routingStoreFiles(t, s.Dir)) {
			t.Fatal("route test changed profile files")
		}
	}
	// Complete-config callers need neither a node store nor a selected policy.
	config["outbounds"] = []any{map[string]any{"type": "direct", "tag": "my-direct"}}
	config["route"].(map[string]any)["final"] = "my-direct"
	data, _ := json.Marshal(config)
	path := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	service = NewService(Options{CorePath: core, ConfigPath: path})
	result, err := service.TestRouting(context.Background(), origin.URL)
	if err != nil || !result.Direct || result.Mode != "完整配置" || result.StatusCode != 204 {
		t.Fatal(result, err)
	}
}

func routingStoreFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	values := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		values[entry.Name()] = string(data)
	}
	return values
}
