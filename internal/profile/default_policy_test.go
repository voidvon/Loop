package profile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func savePolicyDraft(t *testing.T, s Store, draft []byte) {
	t.Helper()
	if err := writePrivateFile(filepath.Join(s.Dir, MainConfigName), draft); err != nil {
		t.Fatal(err)
	}
}

func TestAvailableIsEditableAndSupportsEveryMemberType(t *testing.T) {
	s := testStore(t)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s, MainConfigName)
	config, _ := ParseDocument(before)
	groups, _ := PolicyGroups(config)
	for i := range groups {
		if groups[i].Name == DefaultPolicy {
			groups[i].Builtins = []string{"direct", "reject"}
			groups[i].Groups = []string{"US"}
		}
	}
	draft, err := ChangePolicyGroups(before, groups)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidatePolicyEdit(draft, before); err != nil {
		t.Fatal(err)
	}
	savePolicyDraft(t, s, draft)
	if err := s.PreservePolicyChoices(before); err != nil {
		t.Fatal(err)
	}
	if choice, _ := s.StrategyChoice(DefaultPolicy); choice != NodeKey("manual", "manual-a") {
		t.Fatal("edit lost legacy choice", choice)
	}
	us := testNode("us", "192.0.2.3")
	us.Name = "US node"
	if err := s.SaveManual(us); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"direct", "reject", "US", NodeKey("manual", "manual-b")} {
		if err := s.SelectStrategy(DefaultPolicy, key); err != nil {
			t.Fatal(key, err)
		}
		runtime, err := s.CompileMode(ModeGlobal)
		if err != nil {
			t.Fatal(key, err)
		}
		out := resolvedPolicyNode(t, runtime, DefaultPolicy)
		want := "vless"
		if key == "direct" {
			want = "direct"
		}
		if key == "reject" {
			want = "block"
		}
		if out["type"] != want {
			t.Fatal(key, out)
		}
	}
	// Old CLI node selection must override a formerly selected builtin.
	if err := s.SelectStrategy(DefaultPolicy, "reject"); err != nil {
		t.Fatal(err)
	}
	if err := s.Select(Selection{"manual", "manual-a"}); err != nil {
		t.Fatal(err)
	}
	if choice, _ := s.StrategyChoice(DefaultPolicy); choice != NodeKey("manual", "manual-a") {
		t.Fatal("CLI ignored explicit choice", choice)
	}
	if got, _ := s.Selection(); got != (Selection{"manual", "manual-a"}) {
		t.Fatal(got)
	}
}

func TestAvailableDeletionDoesNotReseedOrRedirectTraffic(t *testing.T) {
	s := testStore(t)
	config, _ := s.Config()
	config["route"] = map[string]any{"final": "Other"}
	config["policy_groups"] = []PolicyGroup{{Name: DefaultPolicy, AllNodes: true}, {Name: "Other", Builtins: []string{"direct"}, Groups: []string{DefaultPolicy}}}
	config["default_policy"] = "Other"
	if err := WriteJSON(filepath.Join(s.Dir, MainConfigName), config); err != nil {
		t.Fatal(err)
	}
	if err := s.SelectStrategy("Other", "direct"); err != nil {
		t.Fatal(err)
	}
	nodes := snapshot(t, s, "nodes.json")
	before := snapshot(t, s, MainConfigName)
	draft, err := s.RemovePolicyGroup(before, DefaultPolicy)
	if err != nil {
		t.Fatal(err)
	}
	savePolicyDraft(t, s, draft)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	states, err := s.PolicyStates()
	if err != nil || len(states) != 1 || states[0].Group.Name != "Other" || len(states[0].Members) != 1 {
		t.Fatal(states, err)
	}
	for _, mode := range []Mode{ModeDirect, ModeRule, ModeGlobal} {
		runtime, err := s.CompileMode(mode)
		if err != nil {
			t.Fatal(mode, err)
		}
		if mode == ModeGlobal && runtime["route"].(map[string]any)["final"] != "Other" {
			t.Fatal("global still depends on Available")
		}
		if runtime["default_policy"] != nil {
			t.Fatal("client field leaked to core")
		}
	}
	if !bytes.Equal(nodes, snapshot(t, s, "nodes.json")) {
		t.Fatal("deletion changed nodes")
	}
	// Deleting the last unused group is allowed. Global mode must ask for an
	// explicit replacement instead of silently selecting a node or direct.
	config, _ = s.Config()
	config["route"] = map[string]any{"final": "direct"}
	if err := WriteJSON(filepath.Join(s.Dir, MainConfigName), config); err != nil {
		t.Fatal(err)
	}
	draft, err = s.RemovePolicyGroup(snapshot(t, s, MainConfigName), "Other")
	if err != nil {
		t.Fatal(err)
	}
	savePolicyDraft(t, s, draft)
	if _, err := s.CompileMode(ModeRule); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompileMode(ModeGlobal); err == nil || !strings.Contains(err.Error(), "未设置") {
		t.Fatal("global silently fell back", err)
	}
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	states, err = s.PolicyStates()
	if err != nil || len(states) != 0 {
		t.Fatal("deleted defaults were recreated", states, err)
	}
}

func TestDefaultPolicyAliasesAndRuleDownloadsUseConfiguredGroup(t *testing.T) {
	s := testStore(t)
	config, _ := s.Config()
	config["policy_groups"] = []PolicyGroup{{Name: "Custom", Builtins: []string{"direct"}}}
	config["default_policy"] = "Custom"
	data, _ := json.Marshal(config)
	savePolicyDraft(t, s, data)
	compiled, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if compiled["route"].(map[string]any)["final"] != "Custom" {
		t.Fatal("legacy alias was not retargeted")
	}
	draft, err := ChangeRouting(data, RuleChange{Operation: "set-add", Subscription: RuleSubscription{Tag: "test", URL: "https://example.org/rules", Format: "binary", UpdateInterval: "1d", Target: "Custom"}})
	if err != nil {
		t.Fatal(err)
	}
	routing, err := ReadRoutingRules(draft)
	if err != nil {
		t.Fatal(err)
	}
	set := routing.RuleSets[len(routing.RuleSets)-1]
	if set["http_client"].(map[string]any)["detour"] != "Custom" {
		t.Fatal("download depends on deleted Available", set)
	}
	if _, err := s.RemovePolicyGroup(data, "Custom"); err == nil || !strings.Contains(err.Error(), "proxy") {
		t.Fatal("lost legacy alias dependency", err)
	}
	for _, bad := range []any{"missing", 42, "direct"} {
		config["default_policy"] = bad
		data, _ = json.Marshal(config)
		if _, err := s.CompileDocument(MainConfigName, data); err == nil {
			t.Fatal("accepted invalid default", bad)
		}
	}
	// An unused malformed Available does not poison unrelated routing.
	config["default_policy"] = "Custom"
	config["route"] = map[string]any{"final": "Custom"}
	config["policy_groups"] = []PolicyGroup{{Name: "Custom", Builtins: []string{"direct"}}, {Name: DefaultPolicy, AllNodes: true, Filter: "no-node-matches"}}
	data, _ = json.Marshal(config)
	savePolicyDraft(t, s, data)
	if _, err := s.Compile(); err != nil {
		t.Fatal("unused Available is still mandatory", err)
	}
	// Ordinary configurations also work with no legacy selection file.
	if err := os.Remove(filepath.Join(s.Dir, "selection.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Compile(); err != nil {
		t.Fatal(err)
	}
}
