package profile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTUNIndependentFromModesAndProfiles(t *testing.T) {
	s := testStore(t)
	baseline := map[string][]byte{}
	for _, name := range []string{"config.json", "nodes.json", "subscriptions.json", "selection.json"} {
		baseline[name], _ = os.ReadFile(filepath.Join(s.Dir, name))
	}
	if enabled, err := s.TUN(); enabled || err != nil {
		t.Fatal(enabled, err)
	}
	if err := s.SaveTUN(true); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{ModeRule, ModeDirect, ModeGlobal} {
		if err := s.SaveMode(mode); err != nil {
			t.Fatal(err)
		}
		if enabled, err := s.TUN(); !enabled || err != nil {
			t.Fatal("mode reset TUN", enabled, err)
		}
		config, err := s.Compile()
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(config)
		var value struct {
			Inbounds []struct {
				Type      string
				AutoRoute bool `json:"auto_route"`
			}
			Route struct {
				AutoDetect bool `json:"auto_detect_interface"`
				Rules      []map[string]any
			}
			DNS struct{ Servers []struct{ Type string } }
		}
		if err := json.Unmarshal(encoded, &value); err != nil {
			t.Fatal(err)
		}
		last := value.Inbounds[len(value.Inbounds)-1]
		if last.Type != "tun" || !last.AutoRoute || !value.Route.AutoDetect {
			t.Fatal("missing TUN or loop protection")
		}
		if len(value.Route.Rules) < 2 || value.Route.Rules[1]["action"] != "hijack-dns" {
			t.Fatal("DNS bypasses policy")
		}
		for _, server := range value.DNS.Servers {
			if server.Type == "local" {
				t.Fatal("recursive system DNS")
			}
		}
		for name, want := range baseline {
			got, _ := os.ReadFile(filepath.Join(s.Dir, name))
			if !bytes.Equal(got, want) {
				t.Fatal("TUN changed profile", name)
			}
		}
	}
	if err := s.SaveTUN(false); err != nil {
		t.Fatal(err)
	}
	if mode, _ := s.Mode(); mode != ModeGlobal {
		t.Fatal("TUN reset routing mode")
	}
	config, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(config)
	if bytes.Contains(data, []byte(`"type":"tun"`)) {
		t.Fatal("TUN still present")
	}
}
