package profile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resolvedPolicyNode(t *testing.T, config map[string]any, tag string) map[string]any {
	t.Helper()
	outbounds := map[string]map[string]any{}
	for _, out := range config["outbounds"].([]any) {
		value := out.(map[string]any)
		outbounds[value["tag"].(string)] = value
	}
	for i := 0; i < len(outbounds); i++ {
		out := outbounds[tag]
		if out == nil {
			t.Fatalf("missing outlet %s", tag)
		}
		if out["type"] != "selector" {
			return out
		}
		tag = out["default"].(string)
	}
	t.Fatal("selector cycle")
	return nil
}
func TestStrategyMigrationAndIndependentSelections(t *testing.T) {
	s := testStore(t)
	old := snapshot(t, s, MainConfigName)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	migrated := snapshot(t, s, MainConfigName)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(migrated, snapshot(t, s, MainConfigName)) || !bytes.Equal(old, snapshot(t, s, MainConfigName+".pre-policies.bak")) {
		t.Fatal("migration not idempotent or missing backup")
	}
	rules, _ := ReadRoutingRules(migrated)
	if rules.Final != DefaultPolicy {
		t.Fatal("Final not migrated")
	}
	config, _ := s.Config()
	groups, _ := PolicyGroups(config)
	if len(groups) != 9 {
		t.Fatal("missing default groups")
	}
	if err := s.SelectStrategy("YouTube", NodeKey("manual", "manual-b")); err != nil {
		t.Fatal(err)
	}
	if err := s.SelectStrategy(DefaultPolicy, NodeKey("manual", "manual-a")); err != nil {
		t.Fatal(err)
	}
	runtime, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if resolvedPolicyNode(t, runtime, "YouTube")["server"] != "192.0.2.2" || resolvedPolicyNode(t, runtime, "Telegram")["server"] != "192.0.2.1" {
		t.Fatal("group selections not independent")
	}
	s.SelectStrategy(DefaultPolicy, NodeKey("manual", "manual-b"))
	runtime, err = s.CompileMode(ModeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if runtime["route"].(map[string]any)["final"] != DefaultPolicy || resolvedPolicyNode(t, runtime, DefaultPolicy)["server"] != "192.0.2.2" {
		t.Fatal("global doesn't use Available")
	}
	if !bytes.Equal(migrated, snapshot(t, s, MainConfigName)) {
		t.Fatal("selection modified routing")
	}
	if runtime["policy_groups"] != nil {
		t.Fatal("client-only configuration leaked to core")
	}
}
func TestPolicyGraphValidationAndMissingMembers(t *testing.T) {
	s := testStore(t)
	data := snapshot(t, s, MainConfigName)
	for _, groups := range [][]PolicyGroup{
		{{Name: DefaultPolicy, AllNodes: true}, {Name: "a", Groups: []string{"b"}}, {Name: "b", Groups: []string{"a"}}},
		{{Name: DefaultPolicy, AllNodes: true}, {Name: "a", Groups: []string{"missing"}}},
		{{Name: DefaultPolicy, AllNodes: true}, {Name: "a", AllNodes: true, Filter: "["}},
		{{Name: DefaultPolicy, AllNodes: true}, {Name: DefaultPolicy, AllNodes: true}},
		{{Name: DefaultPolicy, Groups: []string{"a"}}, {Name: "a", AllNodes: true}},
	} {
		if _, err := ChangePolicyGroups(data, groups); err == nil {
			t.Fatal("accepted invalid graph", groups)
		}
	}
	if err := s.SelectStrategy("Telegram", "HK"); err == nil {
		t.Fatal("selected empty country")
	}
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	data = snapshot(t, s, MainConfigName)
	config, _ := ParseDocument(data)
	groups, _ := PolicyGroups(config)
	kept := []PolicyGroup{}
	for _, g := range groups {
		if g.Name != "YouTube" {
			kept = append(kept, g)
		}
	}
	if _, err := ChangePolicyGroups(data, kept); err == nil {
		t.Fatal("deleted routing dependency")
	}
	route := config["route"].(map[string]any)
	route["final"] = "HK"
	WriteJSON(filepath.Join(s.Dir, MainConfigName), config)
	if _, err := s.Compile(); err == nil || !strings.Contains(err.Error(), "HK") {
		t.Fatal("empty group silently fell back", err)
	}
	if _, err := s.CompileMode(ModeGlobal); err != nil {
		t.Fatal("global should ignore application routing", err)
	}
}
func TestCountryExpansionAndSubscriptionIdentity(t *testing.T) {
	s := testStore(t)
	a := testNode("same", "192.0.2.10")
	a.Name = "香港 HK 01"
	b := testNode("same", "192.0.2.11")
	b.Name = "日本 JP 01"
	for _, sub := range []Subscription{{ID: "one", Name: "One", Nodes: []Node{a}}, {ID: "two", Name: "Two", Nodes: []Node{b}}} {
		if err := s.SaveSubscription(sub); err != nil {
			t.Fatal(err)
		}
	}
	if NodeKey("one", "same") == NodeKey("two", "same") {
		t.Fatal("node identity collision")
	}
	if err := s.SelectStrategy("Telegram", "JP"); err != nil {
		t.Fatal(err)
	}
	runtime, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if resolvedPolicyNode(t, runtime, "Telegram")["server"] != "192.0.2.11" {
		t.Fatal("country filter used wrong source")
	}
	if err := s.SelectStrategy("YouTube", NodeKey("one", "same")); err != nil {
		t.Fatal(err)
	}
	replacement := testNode("changed", "192.0.2.12")
	replacement.Name = "HK new"
	if err := s.SaveSubscription(Subscription{ID: "one", Name: "One", Nodes: []Node{replacement}}); err == nil {
		t.Fatal("refresh lost another group's selected node")
	}
	if err := s.DeleteSubscription("two"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Compile(); err != nil {
		t.Fatal("unused empty JP referenced only by unreferenced Telegram is omitted", err)
	}
	// Once an empty strategy is a route dependency, compilation must fail.
	config, _ := s.Config()
	config["route"].(map[string]any)["final"] = "Telegram"
	WriteJSON(filepath.Join(s.Dir, MainConfigName), config)
	if _, err := s.Compile(); err == nil {
		t.Fatal("deleted selected node silently switched")
	}
}
func TestCustomStrategyRuleTargets(t *testing.T) {
	s := testStore(t)
	data := snapshot(t, s, MainConfigName)
	groups := append(DefaultPolicyGroups(), PolicyGroup{Name: "Work", Groups: []string{DefaultPolicy}})
	draft, err := ChangePolicyGroups(data, groups)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := BuildBasicRule("domain_suffix", "example.com", "Work", "Work")
	if err != nil {
		t.Fatal(err)
	}
	draft, err = ChangeRouting(draft, RuleChange{Operation: "add", Rule: rule})
	if err != nil {
		t.Fatal(err)
	}
	draft, err = ChangeRouting(draft, RuleChange{Operation: "final", Final: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompileDocument(MainConfigName, draft); err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	json.Unmarshal(draft, &config)
	delete(config, "policy_groups")
	bad, _ := json.Marshal(config)
	if _, err := s.CompileDocument(MainConfigName, bad); err == nil {
		t.Fatal("missing policy reference accepted")
	}
	// An editor draft validates even after all node files disappear.
	for _, name := range []string{"nodes.json", "subscriptions.json", "selection.json"} {
		os.Remove(filepath.Join(s.Dir, name))
	}
	if _, err := s.CompileDocument(MainConfigName, draft); err != nil {
		t.Fatal("draft validation requires nodes", err)
	}
}
