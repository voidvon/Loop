package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func routingTestConfig(node ProbeTarget, rules []any) map[string]any {
	out := map[string]any{}
	for k, v := range node.Outbound {
		out[k] = v
	}
	out["tag"] = node.Key
	return map[string]any{
		"inbounds": []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080}, map[string]any{"type": "tun", "auto_route": true}},
		"outbounds": []any{out, map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "selector", "tag": "US", "outbounds": []string{node.Key}, "default": node.Key},
			map[string]any{"type": "selector", "tag": "Available", "outbounds": []string{"US"}, "default": "US"}},
		"dns":          map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "local"}}, "final": "local"},
		"route":        map[string]any{"default_domain_resolver": "local", "final": "Available", "rules": rules},
		"experimental": map[string]any{"cache_file": map[string]any{"enabled": true, "path": "/must-not-write"}, "clash_api": map[string]any{"external_controller": "127.0.0.1:9090"}},
	}
}

func TestRoutingProberActualRulesSelectorsAndHTTPResponse(t *testing.T) {
	core := latencyTestCore(t)
	var hits, originHits atomic.Int32
	node := latencyTestNode(t, "test-node", &hits)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/next", 302)
			return
		}
		w.Header().Set("Connection", "close") // No live connection left to poll.
		w.WriteHeader(403)
	}))
	defer origin.Close()
	domainURL := strings.Replace(origin.URL, "127.0.0.1", "localhost", 1)
	for _, tc := range []struct {
		name, url, want string
		rules           []any
		proxy, rejected bool
		status          int
	}{
		{name: "final", url: origin.URL, want: "test-node", proxy: true, status: 403},
		{name: "domain direct", url: domainURL, want: "direct", rules: []any{map[string]any{"domain": []string{"localhost"}, "action": "route", "outbound": "direct"}}, status: 403},
		{name: "domain proxy", url: domainURL, want: "test-node", rules: []any{map[string]any{"domain_suffix": []string{"localhost"}, "action": "route", "outbound": "Available"}}, proxy: true, status: 403},
		{name: "ip direct", url: origin.URL, want: "direct", rules: []any{map[string]any{"ip_cidr": []string{"127.0.0.0/8"}, "action": "route", "outbound": "direct"}}, status: 403},
		{name: "resolve before ip", url: domainURL, want: "direct", rules: []any{map[string]any{"action": "resolve"}, map[string]any{"ip_cidr": []string{"127.0.0.0/8", "::1/128"}, "action": "route", "outbound": "direct"}}, status: 403},
		{name: "sniff then route", url: domainURL, want: "test-node", rules: []any{map[string]any{"action": "sniff"}, map[string]any{"domain": []string{"localhost"}, "action": "route", "outbound": "Available"}}, proxy: true, status: 403},
		{name: "unconditional route", url: origin.URL, want: "direct", rules: []any{map[string]any{"action": "route", "outbound": "direct"}}, status: 403},
		{name: "reject", url: domainURL, rejected: true, rules: []any{map[string]any{"domain": []string{"localhost"}, "action": "reject"}}},
		{name: "redirect", url: origin.URL + "/redirect", want: "test-node", proxy: true, status: 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, beforeOrigin := hits.Load(), originHits.Load()
			config := routingTestConfig(node, tc.rules)
			original, _ := json.Marshal(config)
			result, err := (RoutingProber{CorePath: core, Timeout: 3 * time.Second}).Run(context.Background(), config, tc.url)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outbound != tc.want || result.Rejected != tc.rejected || result.StatusCode != tc.status {
				t.Fatalf("unexpected result: %+v", result)
			}
			if tc.rejected {
				if result.Error == "" || result.Duration != 0 || originHits.Load() != beforeOrigin {
					t.Fatalf("reject: %+v", result)
				}
			} else {
				if result.Error != "" || result.Duration <= 0 {
					t.Fatalf("response: %+v", result)
				}
				if originHits.Load() != beforeOrigin+1 {
					t.Fatal("redirect followed or request duplicated")
				}
			}
			if tc.proxy {
				if hits.Load() != before+1 || !reflect.DeepEqual(result.Chain, []string{"Available", "US", "test-node"}) {
					t.Fatalf("did not use selected node: %+v", result)
				}
			} else if hits.Load() != before {
				t.Fatal("direct/reject request used proxy")
			}
			if result.Direct != (tc.want == "direct") {
				t.Fatal("direct flag", result)
			}
			if tc.rules == nil && result.Rule != "FINAL" {
				t.Fatal("final rule", result)
			}
			after, _ := json.Marshal(config)
			if string(after) != string(original) {
				t.Fatal("modified caller config")
			}
		})
	}
}

func TestRoutingProberRuleSetAndFailedNode(t *testing.T) {
	core := latencyTestCore(t)
	var hits atomic.Int32
	node := latencyTestNode(t, "node", &hits)
	setPath := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(setPath, []byte(`{"version":3,"rules":[{"domain":["localhost"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	config := routingTestConfig(node, []any{map[string]any{"rule_set": []string{"test-set"}, "action": "route", "outbound": "US"}})
	config["route"].(map[string]any)["rule_set"] = []any{map[string]any{"type": "local", "tag": "test-set", "format": "source", "path": setPath}}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	value := strings.Replace(origin.URL, "127.0.0.1", "localhost", 1)
	result, err := (RoutingProber{CorePath: core, Timeout: 3 * time.Second}).Run(context.Background(), config, value)
	if err != nil || result.Error != "" || result.Outbound != "node" || !strings.Contains(result.Rule, "test-set") || !reflect.DeepEqual(result.Chain, []string{"US", "node"}) {
		t.Fatalf("rule set: %+v %v", result, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	bad := ProbeTarget{Key: "failed-node", Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": port}}
	result, err = (RoutingProber{CorePath: core, Timeout: time.Second}).Run(context.Background(), routingTestConfig(bad, nil), origin.URL)
	if err != nil || result.Error == "" || result.Outbound != "failed-node" || result.Duration != 0 {
		t.Fatalf("failed node lost route: %+v %v", result, err)
	}
}

func TestRoutingProberCancelTimeoutAndCleanup(t *testing.T) {
	core := latencyTestCore(t)
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	for _, cancelTest := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "cancel"}[cancelTest], func(t *testing.T) {
			entered := make(chan struct{})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
			defer origin.Close()
			var hits atomic.Int32
			node := latencyTestNode(t, "node", &hits)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelTest {
				go func() { <-entered; cancel() }()
			}
			result, err := (RoutingProber{CorePath: core, Timeout: 500 * time.Millisecond}).Run(ctx, routingTestConfig(node, nil), origin.URL)
			if cancelTest {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil || result.Error != "测试超时" || result.Outbound != "node" {
				t.Fatalf("timeout: %+v %v", result, err)
			}
			paths, _ := filepath.Glob(filepath.Join(temporary, "loop-routing-*"))
			if len(paths) != 0 {
				t.Fatal("temporary core files leaked", paths)
			}
		})
	}
}

func TestNormalizeRoutingURL(t *testing.T) {
	for _, value := range []string{"", "file:///etc/passwd", "ftp://example.com", "https://user:secret@example.com", "https://example.com:99999", "http://x:0", "http://exa mple.com"} {
		if _, err := NormalizeRoutingURL(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	value, err := NormalizeRoutingURL(" example.com/path?q=x#fragment ")
	if err != nil || value != "https://example.com/path?q=x" {
		t.Fatal(value, err)
	}
}

func TestRoutingRemoteRuleDownloadDoesNotContaminateResult(t *testing.T) {
	core := latencyTestCore(t)
	var hits atomic.Int32
	node := latencyTestNode(t, "node", &hits)
	subscription := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":3,"rules":[{"domain":["localhost"]}]}`))
	}))
	defer subscription.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	config := routingTestConfig(node, []any{map[string]any{"rule_set": []string{"remote-set"}, "action": "route", "outbound": "US"}})
	config["route"].(map[string]any)["rule_set"] = []any{map[string]any{"type": "remote", "tag": "remote-set", "format": "source", "url": subscription.URL, "download_detour": "direct"}}
	result, err := (RoutingProber{CorePath: core, Timeout: 3 * time.Second}).Run(context.Background(), config, strings.Replace(origin.URL, "127.0.0.1", "localhost", 1))
	if err != nil || result.Error != "" || result.Outbound != "node" || !strings.Contains(result.Rule, "remote-set") {
		t.Fatal(result, err)
	}
	if hits.Load() != 1 {
		t.Fatal("wrong proxy request count", hits.Load())
	}
}

func TestRoutingTLSFailureRetainsRouteWithoutBypassingCertificateValidation(t *testing.T) {
	core := latencyTestCore(t)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS request reached origin") }))
	defer origin.Close()
	var hits atomic.Int32
	node := latencyTestNode(t, "node", &hits)
	result, err := (RoutingProber{CorePath: core, Timeout: 3 * time.Second}).Run(context.Background(), routingTestConfig(node, nil), origin.URL)
	if err != nil || result.Error == "" || result.Outbound != "node" || result.StatusCode != 0 || result.Duration != 0 {
		t.Fatal(result, err)
	}
}
