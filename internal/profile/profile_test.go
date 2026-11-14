package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testStore(t *testing.T) Store {
	t.Helper()
	s := Store{Dir: t.TempDir()}
	write := func(name string, value any) {
		t.Helper()
		if err := WriteJSON(filepath.Join(s.Dir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json", map[string]any{
		"proxy_ports": map[string]any{"default": 2080, "global": 2081, "global_enabled": false},
		"dns":         map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "dns-direct"}}},
		"route": map[string]any{
			"final":    "proxy",
			"rules":    []any{map[string]any{"domain_suffix": []string{"cn"}, "outbound": "direct"}},
			"rule_set": []any{map[string]any{"type": "local", "tag": "cn", "path": "rule-sets/cn.srs"}},
		},
	})
	write("nodes.json", NodeList{Nodes: []Node{testNode("manual-a", "192.0.2.1"), testNode("manual-b", "192.0.2.2")}})
	write("subscriptions.json", Subscriptions{Subscriptions: []Subscription{}})
	write("selection.json", Selection{"manual", "manual-a"})
	return s
}

func testNode(id, server string) Node {
	return Node{ID: id, Name: id, Outbound: map[string]any{
		"type": "vless", "tag": "foreign-tag", "server": server, "server_port": 443,
		"uuid": "00000000-0000-0000-0000-000000000001", "domain_resolver": "foreign-dns",
	}}
}

func snapshot(t *testing.T, s Store, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestNodeAndSubscriptionSwitchLeavePolicyIdentical(t *testing.T) {
	s := testStore(t)
	before := snapshot(t, s, "config.json")
	first, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Select(Selection{"manual", "manual-b"}); err != nil {
		t.Fatal(err)
	}
	second, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	// A provider's full config deliberately tries to inject different policy.
	export := map[string]any{
		"outbounds": []any{testNode("sub-node", "192.0.2.3").Outbound, map[string]any{"type": "direct", "tag": "provider-direct"}},
		"route":     map[string]any{"final": "provider-direct"},
		"dns":       map[string]any{"final": "foreign-dns"},
		"inbounds":  []any{map[string]any{"type": "tun"}},
	}
	path := filepath.Join(t.TempDir(), "provider.json")
	if err := WriteJSON(path, export); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ImportSubscription("airport", "机场", path); err != nil || n != 1 {
		t.Fatalf("import got %d, %v", n, err)
	}
	if selection, _ := s.Selection(); selection != (Selection{"manual", "manual-b"}) {
		t.Fatal("import changed selection")
	}
	if err := s.Select(Selection{"airport", "foreign-tag"}); err != nil {
		t.Fatal(err)
	}
	third, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"route", "dns", "inbounds"} {
		if !reflect.DeepEqual(first[key], second[key]) || !reflect.DeepEqual(first[key], third[key]) {
			t.Fatalf("%s changed after selecting a different node/source", key)
		}
	}
	for index, config := range []map[string]any{first, second, third} {
		outbound := resolvedPolicyNode(t, config, DefaultPolicy)
		if !strings.HasPrefix(outbound["tag"].(string), "node-") || outbound["domain_resolver"] != nil {
			t.Fatal("node identity or foreign DNS escaped into policy")
		}
		want := []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}[index]
		if outbound["server"] != want {
			t.Fatalf("expected %s, got %v", want, outbound["server"])
		}
	}
	if string(before) != string(snapshot(t, s, "config.json")) {
		t.Fatal("source policy file changed")
	}
	ruleSet := third["route"].(map[string]any)["rule_set"].([]any)[0].(map[string]any)
	if ruleSet["path"] != filepath.Join(s.Dir, "rule-sets/cn.srs") {
		t.Fatal("rule-set path must resolve relative to the policy")
	}
}

func TestInvalidSelectionAndPolicyFailWithoutOverwriting(t *testing.T) {
	s := testStore(t)
	selection := snapshot(t, s, "selection.json")
	if err := s.Select(Selection{"missing", "missing"}); err == nil {
		t.Fatal("must reject a missing selected node")
	}
	if string(selection) != string(snapshot(t, s, "selection.json")) {
		t.Fatal("invalid selection overwrote current selection")
	}
	var policy map[string]any
	json.Unmarshal(snapshot(t, s, "config.json"), &policy)
	policy["route"].(map[string]any)["rules"] = []any{map[string]any{"outbound": "manual-a"}}
	WriteJSON(filepath.Join(s.Dir, "config.json"), policy)
	output := filepath.Join(t.TempDir(), "runtime.json")
	os.WriteFile(output, []byte("previous valid config"), 0600)
	if err := s.WriteRuntime(output); err == nil {
		t.Fatal("must reject policy tied to a node ID")
	}
	data, _ := os.ReadFile(output)
	if string(data) != "previous valid config" {
		t.Fatal("failed compile overwrote runtime")
	}
}

func TestRefreshPreservesPolicyAndSelectionAndRejectsMissingNode(t *testing.T) {
	s := testStore(t)
	policy := snapshot(t, s, "config.json")
	path := filepath.Join(t.TempDir(), "provider.json")
	writeExport := func(server, tag string) {
		node := testNode("unused", server).Outbound
		node["tag"] = tag
		if err := WriteJSON(path, map[string]any{"outbounds": []any{node}}); err != nil {
			t.Fatal(err)
		}
	}
	writeExport("192.0.2.10", "hk")
	if _, err := s.ImportSubscription("a", "A", path); err != nil {
		t.Fatal(err)
	}
	s.Select(Selection{"a", "hk"})
	selection := snapshot(t, s, "selection.json")
	writeExport("192.0.2.11", "hk")
	if _, err := s.ImportSubscription("a", "A", path); err != nil {
		t.Fatal(err)
	}
	config, err := s.Compile()
	if err != nil || resolvedPolicyNode(t, config, DefaultPolicy)["server"] != "192.0.2.11" {
		t.Fatal("refresh did not update node connection parameters", err)
	}
	old := snapshot(t, s, "subscriptions.json")
	writeExport("192.0.2.12", "new-hk")
	if _, err := s.ImportSubscription("a", "A", path); err == nil {
		t.Fatal("must not silently lose currently selected node")
	}
	if string(old) != string(snapshot(t, s, "subscriptions.json")) || string(selection) != string(snapshot(t, s, "selection.json")) || string(policy) != string(snapshot(t, s, "config.json")) {
		t.Fatal("refresh modified unrelated data or replaced snapshot on failure")
	}
}
