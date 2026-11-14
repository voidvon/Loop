package profile

import (
	"path/filepath"
	"testing"
)

func TestExplicitMembersSourcesAndBuiltins(t *testing.T) {
	s := testStore(t)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	node := testNode("same", "192.0.2.10")
	node.Name = "HK subscription"
	for _, id := range []string{"one", "two"} {
		if err := s.SaveSubscription(Subscription{ID: id, Name: id, Nodes: []Node{node}}); err != nil {
			t.Fatal(err)
		}
	}
	config, _ := s.Config()
	groups, _ := PolicyGroups(config)
	groups = append(groups, PolicyGroup{Name: "Custom", Builtins: []string{"direct", "reject"}, Nodes: []string{NodeKey("manual", "manual-a"), NodeKey("one", "same")}, Sources: []string{"one"}, Filter: "HK"}, PolicyGroup{Name: "Nested", Groups: []string{"Custom"}})
	data, err := ChangePolicyGroups(snapshot(t, s, MainConfigName), groups)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(filepath.Join(s.Dir, MainConfigName), data); err != nil {
		t.Fatal(err)
	}
	states, err := s.PolicyStates()
	if err != nil {
		t.Fatal(err)
	}
	custom := states[len(states)-2]
	if len(custom.Members) != 4 {
		t.Fatal("overlap wasn't deduplicated or source filter leaked", custom.Members)
	}
	for _, member := range custom.Members {
		if member.Key == NodeKey("two", "same") {
			t.Fatal("selected node from another source")
		}
		if member.IsBuiltin && member.Name != "DIRECT" && member.Name != "REJECT" {
			t.Fatal(member)
		}
	}
	for _, key := range []string{"direct", "reject", NodeKey("one", "same")} {
		if err := s.SelectStrategy("Custom", key); err != nil {
			t.Fatal(err)
		}
		runtime, err := s.Compile()
		if err != nil {
			t.Fatal(err)
		}
		leaf := resolvedPolicyNode(t, runtime, "Nested")
		if key == "direct" && leaf["type"] != "direct" || key == "reject" && leaf["type"] != "block" || key == NodeKey("one", "same") && leaf["server"] != "192.0.2.10" {
			t.Fatal(key, leaf)
		}
	}
	next := testNode("new", "192.0.2.11")
	next.Name = "HK new"
	if err := s.SaveSubscription(Subscription{ID: "one", Name: "one", Nodes: []Node{node, next}}); err != nil {
		t.Fatal(err)
	}
	states, _ = s.PolicyStates()
	if len(states[len(states)-2].Members) != 5 {
		t.Fatal("source selection didn't expand on update")
	}
	if err := s.SelectStrategy("Custom", NodeKey("two", "same")); err == nil {
		t.Fatal("selected excluded provider")
	}
}

func TestPolicyMemberValidationAndSelectedRemoval(t *testing.T) {
	s := testStore(t)
	before := snapshot(t, s, MainConfigName)
	for _, bad := range []PolicyGroup{
		{Name: "bad", Builtins: []string{"REJECT-IMG"}},
		{Name: "bad", Nodes: []string{"node-not-valid"}},
		{Name: "bad", Sources: []string{""}},
		{Name: "DIRECT", AllNodes: true},
	} {
		groups := append(DefaultPolicyGroups(), bad)
		if _, err := ChangePolicyGroups(before, groups); err == nil {
			t.Fatal("accepted invalid member", bad)
		}
	}
	groups := DefaultPolicyGroups()
	for i := range groups {
		if groups[i].Name == "YouTube" {
			groups[i] = PolicyGroup{Name: "YouTube", Builtins: []string{"reject"}}
		}
	}
	draft, err := ChangePolicyGroups(before, groups)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidatePolicyEdit(draft, before); err == nil {
		t.Fatal("removed currently selected Available")
	}
}
