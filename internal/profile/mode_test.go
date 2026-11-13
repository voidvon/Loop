package profile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModesPreserveSavedPolicyAndSelection(t *testing.T) {
	s := testStore(t)
	paths := []string{"config.json", "nodes.json", "subscriptions.json", "selection.json"}
	before := map[string][]byte{}
	for _, path := range paths {
		before[path] = snapshot(t, s, path)
	}
	baseline, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := s.Mode(); err != nil || mode != ModeRule {
		t.Fatal("missing settings must use automatic routing", mode, err)
	}
	for _, mode := range []Mode{ModeDirect, ModeGlobal, ModeRule} {
		if err := s.SaveMode(mode); err != nil {
			t.Fatal(err)
		}
		config, err := s.Compile()
		if err != nil {
			t.Fatal(err)
		}
		route := config["route"].(map[string]any)
		dns := config["dns"].(map[string]any)
		switch mode {
		case ModeDirect:
			if route["final"] != "direct" || len(route["rules"].([]any)) != 0 || dns["final"] != "dns-direct" {
				t.Fatal("direct mode still has proxy routing")
			}
			data, _ := json.Marshal(config)
			if strings.Contains(string(data), `"proxy"`) || len(config["outbounds"].([]any)) != 1 {
				t.Fatal("direct mode depends on a proxy")
			}
		case ModeGlobal:
			if route["final"] != DefaultPolicy || route["rule_set"] != nil || config["http_clients"] != nil || dns["final"] != "dns-proxy" {
				t.Fatal("global override incomplete")
			}
			data, _ := json.Marshal(route)
			if strings.Contains(string(data), `"cn"`) {
				t.Fatal("global mode retained domestic bypass")
			}
			rules := route["rules"].([]any)
			if rules[len(rules)-1].(map[string]any)["ip_is_private"] != true {
				t.Fatal("global mode must keep LAN direct")
			}
		case ModeRule:
			if !reflect.DeepEqual(config, baseline) {
				t.Fatal("switching back failed to restore the original policy")
			}
		}
		for _, path := range paths {
			if !bytes.Equal(before[path], snapshot(t, s, path)) {
				t.Fatalf("mode changed %s", path)
			}
		}
		if got, err := (Store{Dir: s.Dir}).Mode(); err != nil || got != mode {
			t.Fatal("mode was not persisted", got, err)
		}
	}
}

func TestDirectModeWithoutNodes(t *testing.T) {
	s := testStore(t)
	for _, path := range []string{"nodes.json", "subscriptions.json", "selection.json"} {
		if err := os.Remove(filepath.Join(s.Dir, path)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveMode(ModeDirect); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Compile(); err != nil {
		t.Fatal("direct mode must not depend on node files", err)
	}
	if _, err := s.CompileMode(ModeRule); err == nil {
		t.Fatal("automatic mode silently fell back")
	}
	if _, err := s.CompileMode(ModeGlobal); err == nil {
		t.Fatal("global mode silently fell back")
	}
	if err := s.SaveMode(Mode("typo")); err == nil {
		t.Fatal("accepted invalid mode")
	}
	if got, _ := s.Mode(); got != ModeDirect {
		t.Fatal("invalid save changed mode")
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "settings.json"), []byte(`{"mode":"typo"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Compile(); err == nil {
		t.Fatal("corrupt mode silently used another mode")
	}
}

func TestModeSurvivesNodeAndSubscriptionChanges(t *testing.T) {
	s := testStore(t)
	if err := s.SaveMode(ModeGlobal); err != nil {
		t.Fatal(err)
	}
	if err := s.Select(Selection{"manual", "manual-b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSubscription(Subscription{ID: "a", Name: "A", Nodes: []Node{testNode("hk", "192.0.2.9")}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Select(Selection{"a", "hk"}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Mode(); err != nil || got != ModeGlobal {
		t.Fatal("node change reset mode", got, err)
	}
	node := testNode("candidate", "192.0.2.10")
	if err := s.SaveMode(ModeDirect); err != nil {
		t.Fatal(err)
	}
	config, err := s.CompileNode(node)
	if err != nil {
		t.Fatal(err)
	}
	if config["outbounds"].([]any)[0].(map[string]any)["type"] != "vless" {
		t.Fatal("direct mode skipped candidate validation")
	}
}
