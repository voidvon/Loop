package profile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func ruleSubscriptionFixture() []byte {
	return []byte(`{
 "dns":{"rules":[{"rule_set":"cn","server":"dns-direct"}]},
 "route":{"final":"proxy","rules":[
 {"domain_suffix":"local","outbound":"direct"},
 {"rule_set":"cn","action":"route","outbound":"direct"},
 {"action":"resolve","strategy":"ipv4_only"},
 {"ip_cidr":"192.0.2.0/24","outbound":"proxy"}],
 "rule_set":[{"type":"remote","tag":"cn","format":"binary","url":"https://example.org/cn.srs","initial_path":"cn.srs","http_client":"existing-client","update_interval":"1d"}]},
 "experimental":{"cache_file":{"enabled":true}},"custom_number":18446744073709551615}`)
}

func TestRuleSubscriptionLifecyclePreservesConfigAndPriority(t *testing.T) {
	source := ruleSubscriptionFixture()
	sub := RuleSubscription{Tag: "test-set", URL: "https://example.org/rules.json", Format: "source", UpdateInterval: "6h", Target: "direct", AfterResolve: true}
	added, err := ChangeRouting(source, RuleChange{Operation: "set-add", Subscription: sub})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := ReadRoutingRules(added)
	if len(routing.RuleSets) != 2 || len(routing.Rules) != 5 || routing.Rules[2]["action"] != "resolve" || routing.Rules[3]["rule_set"] != "test-set" || routing.Rules[4]["ip_cidr"] == nil {
		t.Fatal("subscription lost global priority", string(added))
	}
	sub.URL, sub.Format, sub.Target = "https://example.org/new.srs", "binary", "reject"
	replaced, err := ChangeRouting(added, RuleChange{Operation: "set-replace", Index: 1, Subscription: sub})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ = ReadRoutingRules(replaced)
	if routing.Rules[3]["action"] != "reject" || routing.Rules[3]["outbound"] != nil {
		t.Fatal("wrong target update")
	}
	removed, err := ChangeRouting(replaced, RuleChange{Operation: "set-delete", Index: 1})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := ParseDocument(source)
	after, _ := ParseDocument(removed)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("subscription roundtrip modified unrelated config", string(removed))
	}
}

func TestRuleSubscriptionProtectsDependenciesAndUnknownFields(t *testing.T) {
	source := ruleSubscriptionFixture()
	if _, err := ChangeRouting(source, RuleChange{Operation: "set-delete", Index: 0}); err == nil || !strings.Contains(err.Error(), "dns") {
		t.Fatal("deleted DNS dependency", err)
	}
	sub := RuleSubscription{Tag: "cn", URL: "https://example.org/cn.srs", Format: "binary", UpdateInterval: "7d", Target: "proxy"}
	updated, err := ChangeRouting(source, RuleChange{Operation: "set-replace", Index: 0, Subscription: sub})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := ReadRoutingRules(updated)
	if routing.RuleSets[0]["http_client"] != "existing-client" || routing.RuleSets[0]["initial_path"] != "cn.srs" {
		t.Fatal("editing target discarded metadata")
	}
	sub.URL = "https://example.org/new.srs"
	updated, err = ChangeRouting(updated, RuleChange{Operation: "set-replace", Index: 0, Subscription: sub})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ = ReadRoutingRules(updated)
	if routing.RuleSets[0]["initial_path"] != nil {
		t.Fatal("URL change retained stale snapshot")
	}
	object, _ := ParseDocument(source)
	delete(object, "dns")
	route := object["route"].(map[string]any)
	route["rules"] = []any{map[string]any{"type": "logical", "mode": "and", "rules": []any{map[string]any{"rule_set": "cn"}, map[string]any{"port": 443}}, "outbound": "proxy"}}
	complex, _ := json.Marshal(object)
	for _, operation := range []string{"set-delete", "set-replace"} {
		if _, err := ChangeRouting(complex, RuleChange{Operation: operation, Index: 0, Subscription: sub}); err == nil {
			t.Fatal("simplified complex dependency", operation)
		}
	}
	sub.Target = "" // Metadata-only edits must preserve complex routing exactly.
	updated, err = ChangeRouting(complex, RuleChange{Operation: "set-replace", Index: 0, Subscription: sub})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := ParseDocument(updated)
	before, _ := ParseDocument(complex)
	if !reflect.DeepEqual(after["route"].(map[string]any)["rules"], before["route"].(map[string]any)["rules"]) {
		t.Fatal("metadata edit rewrote logical condition")
	}
}

func TestRuleSubscriptionRejectsInvalidOrAmbiguousChanges(t *testing.T) {
	valid := RuleSubscription{Tag: "new", URL: "https://example.org/new.srs", Format: "binary", UpdateInterval: "1d", Target: "direct"}
	for _, mutate := range []func(*RuleSubscription){
		func(s *RuleSubscription) { s.Tag = "cn" }, func(s *RuleSubscription) { s.Tag = "bad name" }, func(s *RuleSubscription) { s.URL = "file:///tmp/rules" }, func(s *RuleSubscription) { s.URL = "https://" }, func(s *RuleSubscription) { s.UpdateInterval = "0h" }, func(s *RuleSubscription) { s.Format = "clash" }, func(s *RuleSubscription) { s.Target = "a-node" },
	} {
		sub := valid
		mutate(&sub)
		if _, err := ChangeRouting(ruleSubscriptionFixture(), RuleChange{Operation: "set-add", Subscription: sub}); err == nil {
			t.Fatal("accepted invalid subscription", sub)
		}
	}
	// Source rules without a resolve step receive one for IP matches.
	data, err := ChangeRouting([]byte(`{"route":{"final":"proxy","rules":[]}}`), RuleChange{Operation: "set-add", Subscription: RuleSubscription{Tag: "ip", URL: valid.URL, Format: "binary", UpdateInterval: "1d", Target: "proxy", AfterResolve: true}})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := ReadRoutingRules(data)
	if len(routing.Rules) != 2 || routing.Rules[0]["action"] != "resolve" || routing.Rules[1]["rule_set"] != "ip" {
		t.Fatal("IP subscription missing resolution")
	}
}
