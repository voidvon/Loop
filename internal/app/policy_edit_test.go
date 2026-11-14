package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
	"local/loop/internal/testutil"
)

func policyEditDraft(t *testing.T, before []byte) []byte {
	t.Helper()
	config, _ := profile.ParseDocument(before)
	groups, err := profile.PolicyGroups(config)
	if err != nil {
		t.Fatal(err)
	}
	groups = append(groups, profile.PolicyGroup{Name: "Custom", Builtins: []string{"direct", "reject"}, Nodes: []string{profile.NodeKey("manual", "a")}, Groups: []string{profile.DefaultPolicy}})
	draft, err := profile.ChangePolicyGroups(before, groups)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestLivePolicyEditsApplyAndRecoverInAllModes(t *testing.T) {
	for _, mode := range []profile.Mode{profile.ModeRule, profile.ModeGlobal, profile.ModeDirect} {
		for _, fail := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: "/success", true: "/failure"}[fail], func(t *testing.T) {
				s, core := testutil.CoreStore(t)
				s.SaveMode(mode)
				service := NewService(Options{Store: s, CorePath: core})
				path := filepath.Join(s.Dir, profile.MainConfigName)
				before, _ := os.ReadFile(path)
				backend := &ruleTestBackend{path: path}
				if fail {
					backend.failures = []error{errors.New("synthetic start failed"), nil}
				}
				result, err := service.SavePolicies(context.Background(), backend, policyEditDraft(t, before), before, true)
				wantError := fail
				starts := 1
				if fail {
					starts = 2
				}
				if !result.Running || (err != nil) != wantError || len(backend.starts) != starts {
					t.Fatal(result.Running, err, len(backend.starts))
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(after, result.Document) || bytes.Equal(after, before) != wantError {
					t.Fatal("document/rollback outcome mismatch")
				}
				oldConfig, _ := profile.ParseDocument(before)
				newConfig, _ := profile.ParseDocument(after)
				delete(oldConfig, "policy_groups")
				delete(newConfig, "policy_groups")
				if !reflect.DeepEqual(oldConfig, newConfig) {
					t.Fatal("policy edit changed routing or DNS")
				}
			})
		}
	}
}

func TestPolicyEditPreflightAndImplicitChoicePreservation(t *testing.T) {
	s, core := testutil.CoreStore(t)
	service := NewService(Options{Store: s, CorePath: core})
	path := filepath.Join(s.Dir, profile.MainConfigName)
	before, _ := os.ReadFile(path)
	backend := &ruleTestBackend{path: path}
	// A new builtin must not displace the implicit node selection in a country.
	n := profile.Node{ID: "us", Name: "US test", Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": 1}}
	if err := s.SaveManual(n); err != nil {
		t.Fatal(err)
	}
	config, _ := profile.ParseDocument(before)
	groups, _ := profile.PolicyGroups(config)
	for i := range groups {
		if groups[i].Name == "US" {
			groups[i].Builtins = []string{"direct", "reject"}
		}
	}
	draft, _ := profile.ChangePolicyGroups(before, groups)
	if _, err := service.SavePolicies(context.Background(), backend, draft, before, true); err != nil {
		t.Fatal(err)
	}
	choice, _ := s.StrategyChoice("US")
	if choice != profile.NodeKey("manual", "us") {
		t.Fatal("edit switched implicit choice", choice)
	}
	before, _ = os.ReadFile(path)
	// Neither stale saves nor removal of a selected member may interrupt traffic.
	for _, stale := range []bool{true, false} {
		config, _ = profile.ParseDocument(before)
		groups, _ = profile.PolicyGroups(config)
		for i := range groups {
			if groups[i].Name == "US" {
				groups[i].AllNodes = false
				groups[i].Filter = ""
				groups[i].Builtins = []string{"direct"}
			}
		}
		draft, _ = profile.ChangePolicyGroups(before, groups)
		expected := before
		if stale {
			draft = policyEditDraft(t, before)
			expected = []byte("stale")
		}
		stops := backend.stops
		result, err := service.SavePolicies(context.Background(), backend, draft, expected, true)
		if err == nil || !result.Running || backend.stops != stops {
			t.Fatal("preflight touched live backend", err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(after, before) {
			t.Fatal("failed preflight changed config")
		}
	}
}

func TestBuiltinStrategyUsesNativeCoreAndRoutingDiagnostics(t *testing.T) {
	s, core := testutil.CoreStore(t)
	config := map[string]any{
		"proxy_ports":   map[string]any{"default": 2080, "global": 2081, "global_enabled": false},
		"route":         map[string]any{"final": "Nested"},
		"policy_groups": []profile.PolicyGroup{{Name: profile.DefaultPolicy, AllNodes: true}, {Name: "Custom", Builtins: []string{"direct", "reject"}}, {Name: "Nested", Groups: []string{"Custom"}}},
	}
	if err := profile.WriteJSON(filepath.Join(s.Dir, profile.MainConfigName), config); err != nil {
		t.Fatal(err)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	service := NewService(Options{Store: s, CorePath: core})
	for _, key := range []string{"reject", "direct"} {
		if err := s.SelectStrategy("Custom", key); err != nil {
			t.Fatal(err)
		}
		runtime, err := s.Compile()
		if err != nil {
			t.Fatal(err)
		}
		if err := proxy.CheckConfiguration(context.Background(), core, runtime); err != nil {
			t.Fatal(err)
		}
		result, err := service.TestRouting(context.Background(), origin.URL)
		if err != nil || result.Rejected != (key == "reject") || result.Direct != (key == "direct") || len(result.Chain) != 3 {
			t.Fatal(key, result, err)
		}
		if key == "direct" && result.StatusCode != 204 {
			t.Fatal(result)
		}
	}
	targets, err := service.StrategyProbeTargets("Custom")
	if err == nil || len(targets) != 0 {
		t.Fatal("builtins must not be probed as proxy nodes", targets, err)
	}
}

func TestAvailableEditsAndDefaultChangesApplyOnline(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "rollback"}[fail], func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			service := NewService(Options{Store: s, CorePath: core})
			path := filepath.Join(s.Dir, profile.MainConfigName)
			before, _ := os.ReadFile(path)
			config, _ := profile.ParseDocument(before)
			groups, _ := profile.PolicyGroups(config)
			for i := range groups {
				if groups[i].Name == profile.DefaultPolicy {
					groups[i].Builtins = []string{"direct", "reject"}
				}
			}
			groups = append(groups, profile.PolicyGroup{Name: "Other", Builtins: []string{"direct"}})
			draft, err := profile.ChangePolicyGroups(before, groups)
			if err != nil {
				t.Fatal(err)
			}
			backend := &ruleTestBackend{path: path}
			if _, err := service.SavePolicies(context.Background(), backend, draft, before, true); err != nil {
				t.Fatal(err)
			}
			if choice, _ := s.StrategyChoice(profile.DefaultPolicy); choice != profile.NodeKey("manual", "a") {
				t.Fatal("lost legacy node choice")
			}
			if err := s.SelectStrategy(profile.DefaultPolicy, "direct"); err != nil {
				t.Fatal(err)
			}
			before, _ = os.ReadFile(path)
			config, _ = profile.ParseDocument(before)
			groups, _ = profile.PolicyGroups(config)
			for i := range groups {
				if groups[i].Name == profile.DefaultPolicy {
					groups[i] = profile.PolicyGroup{Name: profile.DefaultPolicy, Builtins: []string{"direct"}, Groups: []string{"Other"}}
				}
			}
			draft, err = profile.ChangePolicyGroups(before, groups)
			if err != nil {
				t.Fatal(err)
			}
			if fail {
				backend.failures = []error{errors.New("test startup failure"), nil}
			}
			result, err := service.SavePolicies(context.Background(), backend, draft, before, true)
			if !result.Running || (err != nil) != fail {
				t.Fatal(result.Running, err)
			}
			actual, err := s.Compile()
			if err != nil {
				t.Fatal(err)
			}
			if err := proxy.CheckConfiguration(context.Background(), core, actual); err != nil {
				t.Fatal(err)
			}
			choice, _ := s.StrategyChoice(profile.DefaultPolicy)
			if choice != "direct" {
				t.Fatal("builtin selection lost after editing", choice)
			}
			before, _ = os.ReadFile(path)
			draft, err = profile.ChangeDefaultPolicy(before, "Other")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SaveMode(profile.ModeGlobal); err != nil {
				t.Fatal(err)
			}
			if fail {
				backend.failures = []error{errors.New("test default failure"), nil}
			}
			result, err = service.SavePolicies(context.Background(), backend, draft, before, true)
			if !result.Running || (err != nil) != fail {
				t.Fatal(result.Running, err)
			}
			name, _ := s.DefaultStrategy()
			want := "Other"
			if fail {
				want = profile.DefaultPolicy
			}
			if name != want {
				t.Fatal("default change/rollback mismatch", name)
			}
			actual, err = s.Compile()
			if err != nil {
				t.Fatal(err)
			}
			if actual["route"].(map[string]any)["final"] != want {
				t.Fatal("global still uses fixed Available")
			}
			if err := proxy.CheckConfiguration(context.Background(), core, actual); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeletingAvailableRespectsLiveGlobalDependencies(t *testing.T) {
	for _, mode := range []profile.Mode{profile.ModeRule, profile.ModeGlobal} {
		t.Run(string(mode), func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			config := map[string]any{"proxy_ports": map[string]any{"default": 2080, "global": 2081, "global_enabled": false}, "route": map[string]any{"final": "direct"}, "policy_groups": []profile.PolicyGroup{{Name: profile.DefaultPolicy, Builtins: []string{"direct"}}, {Name: "Other", Builtins: []string{"direct"}}}}
			path := filepath.Join(s.Dir, profile.MainConfigName)
			if err := profile.WriteJSON(path, config); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveMode(mode); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			draft, err := s.RemovePolicyGroup(before, profile.DefaultPolicy)
			if err != nil {
				t.Fatal(err)
			}
			backend := &ruleTestBackend{path: path}
			service := NewService(Options{Store: s, CorePath: core})
			result, err := service.SavePolicies(context.Background(), backend, draft, before, true)
			if !result.Running || (err != nil) != (mode == profile.ModeGlobal) {
				t.Fatal(result.Running, err)
			}
			if mode == profile.ModeGlobal {
				if backend.stops != 0 {
					t.Fatal("missing global default stopped the connection")
				}
				draft, _ = profile.ChangeDefaultPolicy(before, "Other")
				result, err = service.SavePolicies(context.Background(), backend, draft, before, true)
				if err != nil {
					t.Fatal(err)
				}
				before = result.Document
				draft, err = s.RemovePolicyGroup(before, profile.DefaultPolicy)
				if err != nil {
					t.Fatal(err)
				}
				result, err = service.SavePolicies(context.Background(), backend, draft, before, true)
				if err != nil || !result.Running {
					t.Fatal("could not delete former default", err)
				}
			}
			states, err := s.PolicyStates()
			if err != nil || len(states) != 1 || states[0].Group.Name != "Other" {
				t.Fatal(states, err)
			}
		})
	}
}
