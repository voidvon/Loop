package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

type ruleTestBackend struct {
	path      string
	starts    [][]byte
	failures  []error
	stops     int
	stopError error
	onStart   func()
}

func (b *ruleTestBackend) Start(ctx context.Context) error {
	data, err := os.ReadFile(b.path)
	if err != nil {
		return err
	}
	b.starts = append(b.starts, data)
	if b.onStart != nil {
		b.onStart()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(b.failures) > 0 {
		err := b.failures[0]
		b.failures = b.failures[1:]
		return err
	}
	return nil
}
func (b *ruleTestBackend) Stop() error { b.stops++; return b.stopError }

func TestSaveRulesKeepsConnectionAndRollsBackFailedRestart(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		failures                             []error
		stopError                            error
		wantRunning, wantError, wantOriginal bool
		starts                               int
	}{
		{name: "success", wantRunning: true, starts: 1},
		{name: "restart failed", failures: []error{errors.New("new failed"), nil}, wantRunning: true, wantError: true, wantOriginal: true, starts: 2},
		{name: "restart timed out", failures: []error{context.DeadlineExceeded, nil}, wantRunning: true, wantError: true, wantOriginal: true, starts: 2},
		{name: "both failed", failures: []error{errors.New("new failed"), errors.New("old failed")}, wantError: true, wantOriginal: true, starts: 2},
		{name: "stop failed", stopError: errors.New("cleanup failed"), wantError: true, wantOriginal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			service := NewService(Options{Store: s, CorePath: core})
			path := filepath.Join(s.Dir, profile.MainConfigName)
			before, _ := os.ReadFile(path)
			rule, _ := profile.BuildBasicRule("domain_suffix", "example.org", "direct")
			draft, err := profile.ChangeRouting(before, profile.RuleChange{Operation: "add", Rule: rule})
			if err != nil {
				t.Fatal(err)
			}
			unchanged := map[string][]byte{}
			for _, name := range []string{"nodes.json", "subscriptions.json", "selection.json"} {
				unchanged[name], _ = os.ReadFile(filepath.Join(s.Dir, name))
			}
			backend := &ruleTestBackend{path: path, failures: tc.failures, stopError: tc.stopError}
			result, err := service.SaveRules(context.Background(), backend, draft, before, true)
			if result.Running != tc.wantRunning || (err != nil) != tc.wantError || len(backend.starts) != tc.starts {
				t.Fatal(result.Running, err, len(backend.starts))
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, result.Document) || bytes.Equal(before, after) != tc.wantOriginal {
				t.Fatal("source/result disagree with application outcome")
			}
			if len(backend.starts) > 0 && !bytes.Contains(backend.starts[0], []byte("example.org")) {
				t.Fatal("started old rules")
			}
			if len(backend.starts) > 1 && !bytes.Equal(backend.starts[1], before) {
				t.Fatal("recovery didn't start original config")
			}
			backup, _ := os.ReadFile(path + ".bak")
			if !bytes.Equal(backup, before) {
				t.Fatal("rollback replaced the original backup")
			}
			for name, data := range unchanged {
				after, _ := os.ReadFile(filepath.Join(s.Dir, name))
				if !bytes.Equal(data, after) {
					t.Fatal("rule save changed", name)
				}
			}
		})
	}
}

func TestSaveRulesPreflightAndGlobalOverridesNeverStopConnection(t *testing.T) {
	for _, kind := range []string{"invalid", "stale", "cancelled", "empty strategy", "direct", "global", "offline"} {
		t.Run(kind, func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			service := NewService(Options{Store: s, CorePath: core})
			path := filepath.Join(s.Dir, profile.MainConfigName)
			before, _ := os.ReadFile(path)
			draft, _ := profile.ChangeRouting(before, profile.RuleChange{Operation: "final", Final: "direct"})
			expected := before
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantError := true
			switch kind {
			case "invalid":
				draft = []byte(`{"route":`)
			case "stale":
				expected = []byte("stale")
			case "cancelled":
				cancel()
			case "empty strategy":
				draft, _ = profile.ChangeRouting(before, profile.RuleChange{Operation: "final", Final: "US"})
			case "direct":
				s.SaveMode(profile.ModeDirect)
				wantError = false
			case "global":
				s.SaveMode(profile.ModeGlobal)
				wantError = false
			case "offline":
				wantError = false
			}
			backend := &ruleTestBackend{path: path}
			result, err := service.SaveRules(ctx, backend, draft, expected, kind != "offline")
			if (err != nil) != wantError || result.Running != (kind != "offline") || backend.stops != 0 || len(backend.starts) != 0 {
				t.Fatal(result.Running, err, backend.stops, len(backend.starts))
			}
			after, _ := os.ReadFile(path)
			if wantError && !bytes.Equal(before, after) {
				t.Fatal("preflight failure changed source")
			}
			if !wantError && !bytes.Equal(result.Document, after) {
				t.Fatal("successful save wasn't synchronized")
			}
		})
	}
}

func TestSaveRulesShutdownAndExternalEditDuringFailedRestart(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "shutdown", true: "external edit"}[external], func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			service := NewService(Options{Store: s, CorePath: core})
			path := filepath.Join(s.Dir, profile.MainConfigName)
			before, _ := os.ReadFile(path)
			draft, _ := profile.ChangeRouting(before, profile.RuleChange{Operation: "final", Final: "direct"})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			edited := append(append([]byte(nil), before...), '\n')
			backend := &ruleTestBackend{path: path, failures: []error{errors.New("failed")}, onStart: func() {
				if external {
					os.WriteFile(path, edited, 0600)
				} else {
					cancel()
				}
			}}
			result, err := service.SaveRules(ctx, backend, draft, before, true)
			if err == nil || result.Running || len(backend.starts) != 1 {
				t.Fatal("restarted after shutdown or conflict", err)
			}
			after, _ := os.ReadFile(path)
			if external {
				if !bytes.Equal(after, edited) || !strings.Contains(err.Error(), "其他操作修改") {
					t.Fatal("overwrote external edit", err)
				}
			} else if !bytes.Equal(after, before) {
				t.Fatal("shutdown didn't restore source")
			}
		})
	}
}

func TestRemoteRuleSubscriptionsPassCoreValidationAndReconnect(t *testing.T) {
	for _, format := range []string{"source", "binary"} {
		t.Run(format, func(t *testing.T) {
			store, core := testutil.CoreStore(t)
			service := NewService(Options{Store: store, CorePath: core})
			path := filepath.Join(store.Dir, profile.MainConfigName)
			before, _ := os.ReadFile(path)
			draft, err := profile.ChangeRouting(before, profile.RuleChange{Operation: "set-add", Subscription: profile.RuleSubscription{Tag: "test-remote", URL: "https://example.org/rules", Format: format, UpdateInterval: "1d", Target: profile.DefaultPolicy, AfterResolve: true}})
			if err != nil {
				t.Fatal(err)
			}
			backend := &ruleTestBackend{path: path}
			result, err := service.SaveRules(context.Background(), backend, draft, before, true)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Running || backend.stops != 1 || len(backend.starts) != 1 {
				t.Fatal("subscription didn't preserve connection intent")
			}
			rules, err := profile.ReadRoutingRules(result.Document)
			if err != nil || rules.RuleSets[len(rules.RuleSets)-1]["format"] != format {
				t.Fatal("subscription wasn't saved", err)
			}
		})
	}
}
