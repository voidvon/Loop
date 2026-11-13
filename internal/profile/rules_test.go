package profile

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestBasicRulesNormalizeAndValidateInputs(t *testing.T) {
	for _, tc := range []struct {
		kind, input, target string
		want                []string
	}{
		{"domain", "Example.COM.\n例子.cn\nexample.com", "proxy", []string{"example.com", "xn--fsqu00a.cn"}},
		{"domain_suffix", ".example.com,cn", "direct", []string{".example.com", "cn"}},
		{"domain_keyword", "example，news", "reject", []string{"example", "news"}},
		{"ip_cidr", "192.0.2.129/24\n2001:db8::1\n192.0.2.1", "direct", []string{"192.0.2.0/24", "2001:db8::1/128", "192.0.2.1/32"}},
		{"ip_is_private", "", "direct", nil},
	} {
		rule, err := BuildBasicRule(tc.kind, tc.input, tc.target)
		if err != nil {
			t.Fatal(err)
		}
		basic, ok := ReadBasicRule(rule)
		if !ok || basic.Kind != tc.kind || basic.Target != tc.target || !reflect.DeepEqual(basic.Values, tc.want) {
			t.Fatalf("wrong normalized rule: %+v", basic)
		}
	}
	for _, tc := range []struct{ kind, input, target string }{
		{"domain", "https://example.com/path", "proxy"},
		{"domain_suffix", "*.example.com", "direct"},
		{"domain", "bad..example", "direct"},
		{"domain", "example.com:443", "direct"},
		{"domain", "", "direct"},
		{"ip_cidr", "300.1.2.3", "proxy"},
		{"ip_cidr", "192.0.2.0/64", "proxy"},
		{"ip_cidr", "fe80::1%en0", "direct"},
		{"network", "tcp", "direct"},
		{"domain", "example.com", "some-provider-node"},
	} {
		if _, err := BuildBasicRule(tc.kind, tc.input, tc.target); err == nil {
			t.Fatal("accepted invalid rule", tc)
		}
	}
}

func TestRuleChangesPreserveOtherConfigAndAdvancedRules(t *testing.T) {
	s := testStore(t)
	original := snapshot(t, s, MainConfigName)
	config, _ := ParseDocument(original)
	advanced := map[string]any{"type": "logical", "mode": "and", "rules": []any{map[string]any{"domain_suffix": "example.net"}, map[string]any{"port": 443}}, "action": "route", "outbound": "proxy"}
	config["route"].(map[string]any)["rules"] = []any{advanced, map[string]any{"action": "resolve", "strategy": "ipv4_only"}, map[string]any{"ip_is_private": true, "action": "route", "outbound": "direct"}}
	config["experimental"] = map[string]any{"preserved_number": json.Number("18446744073709551615")}
	original, _ = json.Marshal(config)
	before, _ := ReadRoutingRules(original)
	data, err := ChangeRouting(original, RuleChange{Operation: "final", Final: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := ReadRoutingRules(data)
	if routing.Final != "direct" || !reflect.DeepEqual(routing.Rules, before.Rules) {
		t.Fatal("Final changed route rules")
	}
	rule, _ := BuildBasicRule("domain_suffix", "example.com", "reject")
	data, err = ChangeRouting(data, RuleChange{Operation: "add", Rule: rule})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ = ReadRoutingRules(data)
	if routing.Rules[0]["action"] != "reject" || !reflect.DeepEqual(routing.Rules[1], before.Rules[0]) {
		t.Fatal("adding a rule changed advanced conditions or priority")
	}
	data, err = ChangeRouting(data, RuleChange{Operation: "move", Index: 0, To: 3})
	if err != nil {
		t.Fatal(err)
	}
	routing, _ = ReadRoutingRules(data)
	if routing.Rules[3]["action"] != "reject" || !reflect.DeepEqual(routing.Rules[0], before.Rules[0]) {
		t.Fatal("move lost a rule or changed other conditions")
	}
	replacement, _ := BuildBasicRule("domain_suffix", "changed.example", "proxy")
	data, err = ChangeRouting(data, RuleChange{Operation: "replace", Index: 3, Rule: replacement})
	if err != nil {
		t.Fatal(err)
	}
	data, err = ChangeRouting(data, RuleChange{Operation: "delete", Index: 3})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := ParseDocument(data)
	for key, want := range config {
		if key != "route" && !reflect.DeepEqual(after[key], want) {
			t.Fatal("rule editor changed unrelated config", key)
		}
	}
	routing, _ = ReadRoutingRules(data)
	if !reflect.DeepEqual(routing.Rules, before.Rules) || !bytes.Contains(data, []byte("18446744073709551615")) {
		t.Fatal("roundtrip lost advanced rule data")
	}
	if _, err := ChangeRouting(data, RuleChange{Operation: "replace", Index: 0, Rule: replacement}); err == nil {
		t.Fatal("basic form overwrote an advanced rule")
	}
	if _, ok := ReadBasicRule(map[string]any{"domain": "example.com", "port": 443, "action": "route", "outbound": "direct"}); ok {
		t.Fatal("form would discard extra match conditions")
	}
	for _, change := range []RuleChange{{Operation: "final", Final: "reject"}, {Operation: "delete", Index: -1}, {Operation: "move", Index: 0, To: 100}} {
		if _, err := ChangeRouting(data, change); err == nil {
			t.Fatal("accepted invalid change", change)
		}
	}
}

func TestNewIPRulesFollowUnconditionalResolution(t *testing.T) {
	ip, _ := BuildBasicRule("ip_cidr", "192.0.2.1", "proxy")
	for _, source := range []string{
		`{"route":{"final":"proxy","rules":[]}}`,
		`{"route":{"final":"proxy","rules":[{"domain_suffix":"cn","outbound":"direct"},{"action":"resolve","strategy":"ipv4_only"},{"rule_set":"cn","outbound":"direct"}]}}`,
		`{"route":{"final":"proxy","rules":[{"action":"resolve","domain_suffix":"cn"}]}}`,
	} {
		data, err := ChangeRouting([]byte(source), RuleChange{Operation: "add", Rule: ip})
		if err != nil {
			t.Fatal(err)
		}
		routing, _ := ReadRoutingRules(data)
		found := false
		for i, rule := range routing.Rules {
			if rule["ip_cidr"] != nil {
				found = true
				if i == 0 || routing.Rules[i-1]["action"] != "resolve" || !unconditionalResolve(routing.Rules[i-1]) {
					t.Fatal("IP rule inserted before resolution")
				}
			}
		}
		if !found {
			t.Fatal("IP rule disappeared")
		}
	}
}
