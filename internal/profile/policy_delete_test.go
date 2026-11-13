package profile

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRemoveCountryPoliciesCleansOnlyOptionalReferences(t *testing.T) {
	s := testStore(t)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	unchanged := map[string][]byte{}
	for _, file := range []string{"nodes.json", "subscriptions.json", "selection.json"} {
		unchanged[file] = snapshot(t, s, file)
	}
	for _, country := range []string{"US", "KR", "JP", "SG", "TW", "HK"} {
		before := snapshot(t, s, MainConfigName)
		original, _ := ParseDocument(before)
		draft, err := s.RemovePolicyGroup(before, country)
		if err != nil {
			t.Fatalf("delete %s: %v", country, err)
		}
		config, _ := ParseDocument(draft)
		groups, err := PolicyGroups(config)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range groups {
			if group.Name == country {
				t.Fatal("deleted group remains")
			}
			for _, ref := range group.Groups {
				if ref == country {
					t.Fatal("dangling optional reference")
				}
			}
		}
		delete(original, "policy_groups")
		delete(config, "policy_groups")
		if !reflect.DeepEqual(original, config) {
			t.Fatal("delete changed DNS, rules or other settings")
		}
		if !bytes.Equal(before, snapshot(t, s, MainConfigName)) {
			t.Fatal("preparing deletion wrote the source")
		}
		if _, err := s.CompileDocument(MainConfigName, draft); err != nil {
			t.Fatal("deletion produced invalid runtime configuration", err)
		}
		var saved any
		json.Unmarshal(draft, &saved)
		if err := WriteJSON(filepath.Join(s.Dir, MainConfigName), saved); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Compile(); err != nil {
			t.Fatal("saved policy deletion broke selected traffic", err)
		}
	}
	for file, before := range unchanged {
		if !bytes.Equal(before, snapshot(t, s, file)) {
			t.Fatalf("delete changed %s", file)
		}
	}
}

func TestRemovePolicyReportsRoutingAndSelectionDependencies(t *testing.T) {
	for _, kind := range []string{"final", "rule", "dns", "download", "selected", "implicit", "Available", "missing"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			config, _ := s.Config()
			config["policy_groups"] = DefaultPolicyGroups()
			want := "US"
			name := "US"
			switch kind {
			case "final":
				config["route"].(map[string]any)["final"] = "US"
				want = "route.final"
			case "rule":
				config["route"].(map[string]any)["rules"] = []any{map[string]any{"type": "logical", "mode": "or", "rules": []any{map[string]any{"outbound": "US"}}}}
				want = "route.rules[0].rules[0].outbound"
			case "dns":
				config["dns"] = map[string]any{"servers": []any{map[string]any{"detour": "US"}}}
				want = "dns.servers[0].detour"
			case "download":
				config["route"].(map[string]any)["rule_set"] = []any{map[string]any{"download_detour": "US"}}
				want = "route.rule_set[0].download_detour"
			case "selected":
				if err := WriteJSON(filepath.Join(s.Dir, "policy-selections.json"), map[string]string{"YouTube": "US"}); err != nil {
					t.Fatal(err)
				}
				want = "YouTube 选中"
			case "implicit":
				config["policy_groups"] = append(DefaultPolicyGroups(), PolicyGroup{Name: "Work", Groups: []string{"US"}})
				want = "Work 选中"
			case "Available":
				name, want = DefaultPolicy, "不能删除默认策略"
			case "missing":
				name, want = "missing", "已不存在"
			}
			data, _ := json.Marshal(config)
			if draft, err := s.RemovePolicyGroup(data, name); err == nil || draft != nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected actionable dependency %q, got %v", want, err)
			}
		})
	}
}
