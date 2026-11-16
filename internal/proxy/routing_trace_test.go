package proxy

import (
	"reflect"
	"testing"
)

func TestRoutingTraceResolvedDestination(t *testing.T) {
	for _, tc := range []struct {
		name, resolved, destination string
	}{
		{"IPv4", "192.0.2.10 192.0.2.11", "192.0.2.11:443"},
		{"IPv6", "192.0.2.10 2001:db8::1", "[2001:db8:0:0::1]:443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := map[string]any{
				"route": map[string]any{"final": "Available", "rules": []any{
					map[string]any{"action": "resolve"},
					map[string]any{"domain_suffix": "example.com", "action": "route", "outbound": "US"},
				}},
				"outbounds": []any{
					map[string]any{"type": "selector", "tag": "Available", "default": "US"},
					map[string]any{"type": "selector", "tag": "US", "default": "node"},
					map[string]any{"type": "trojan", "tag": "node"},
				},
			}
			trace := &routingTrace{config: config}
			// DNS and destination traffic share a context and can use the same
			// outbound. Only the router's resolved addresses identify the latter.
			prefix := "INFO[0000] [123 0ms] "
			trace.Write([]byte(prefix + "inbound/mixed[mixed-in]: [routing-test] inbound connection to www.example.com:443\n"))
			trace.Write([]byte(prefix + "router: match[0] => resolve\n"))
			trace.Write([]byte(prefix + "outbound/trojan[node]: outbound connection to 1.1.1.1:443\n"))
			trace.Write([]byte(prefix + "dns: lookup succeed for www.example.com: 192.0.2.99\n"))
			trace.Write([]byte("DEBUG[0000] [456 0ms] router: resolved [192.0.2.98]\n"))
			trace.Write([]byte(prefix + "router: resolved [" + tc.resolved + "]\n"))
			for _, destination := range []string{"1.1.1.1:443", "192.0.2.99:443", "192.0.2.98:443", "192.0.2.10:8443"} {
				trace.Write([]byte(prefix + "outbound/trojan[node]: outbound connection to " + destination + "\n"))
			}
			var result RoutingResult
			trace.apply(&result)
			if result.Outbound != "" || len(result.Chain) != 0 || len(trace.outputs) != 0 {
				t.Fatalf("unrelated traffic identified as request: %+v", result)
			}
			trace.Write([]byte(prefix + "outbound/trojan[node]: outbound connection to " + tc.destination + "\n"))
			trace.apply(&result)
			if result.Outbound != "node" || result.Rule != "FINAL" || !reflect.DeepEqual(result.Chain, []string{"Available", "US", "node"}) {
				t.Fatalf("resolved final route: %+v", result)
			}
		})
	}
}

func TestRoutingTraceAddressOverrideClearsResolvedDestination(t *testing.T) {
	trace := &routingTrace{config: map[string]any{
		"route": map[string]any{"rules": []any{
			map[string]any{"action": "route-options", "override_address": "other.example.com"},
		}},
	}}
	trace.Write([]byte("INFO [123 0ms] inbound/mixed[mixed-in]: [routing-test] inbound connection to www.example.com:443\n" +
		"DEBUG [123 0ms] router: resolved [192.0.2.10]\n" +
		"DEBUG [123 0ms] router: match[0] => route-options\n" +
		"INFO [123 0ms] outbound/trojan[node]: outbound connection to 192.0.2.10:443\n"))
	if len(trace.outputs) != 0 || trace.destination != "other.example.com:443" {
		t.Fatalf("retained addresses before override: %+v", trace)
	}
}
