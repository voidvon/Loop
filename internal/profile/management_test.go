package profile

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestClashExtractsNodesOnly(t *testing.T) {
	data := []byte(`rules: [MATCH,DIRECT]
dns: {enable: false}
proxies:
  - {name: Reality, type: vless, server: example.com, port: 443, uuid: test, tls: true, flow: xtls-rprx-vision, reality-opts: {public-key: public, short-id: ab}}
  - {name: Trojan, type: trojan, server: example.com, port: 443, password: secret, sni: tls.example, skip-cert-verify: true, network: ws, ws-opts: {path: /ws, headers: {Host: ws.example}}}
  - {name: Hy2, type: hysteria2, server: example.com, port: 443, password: secret, ports: '2000-3000,4000', obfs: salamander, obfs-password: hidden}
`)
	nodes, err := ParseClash(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes", len(nodes))
	}
	tls := nodes[0].Outbound["tls"].(map[string]any)
	if tls["utls"].(map[string]any)["fingerprint"] != "chrome" || tls["reality"].(map[string]any)["public_key"] != "public" {
		t.Fatal("Reality lost")
	}
	trojan := nodes[1].Outbound
	if trojan["tls"].(map[string]any)["insecure"] != true || trojan["transport"].(map[string]any)["path"] != "/ws" {
		t.Fatal("TLS/transport lost")
	}
	if nodes[2].Outbound["server_port"] != nil || !reflect.DeepEqual(nodes[2].Outbound["server_ports"], []string{"2000:3000", "4000"}) {
		t.Fatal("port hopping lost")
	}
	s := testStore(t)
	before := snapshot(t, s, "config.json")
	if err := s.SaveSubscription(Subscription{ID: "provider", Name: "Provider", Format: "clash", Nodes: nodes}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, snapshot(t, s, "config.json")) {
		t.Fatal("import changed policy")
	}
	if err := s.Select(Selection{"provider", "Reality"}); err != nil {
		t.Fatal(err)
	}
	config, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if config["route"].(map[string]any)["final"] != DefaultPolicy {
		t.Fatal("provider policy escaped")
	}
}

func TestFetchClashNegotiationAndPrivateErrors(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "clash.meta" || r.URL.Query().Get("token") != "private-value" {
			t.Error("bad negotiation")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, "proxies:\n  - {name: A, type: trojan, server: example.com, port: 443, password: hidden}\n")
	}))
	defer server.Close()
	address := server.URL + "?token=private-value"
	if nodes, err := FetchClash(context.Background(), address); err != nil || len(nodes) != 1 {
		t.Fatalf("fetch: %v", err)
	}
	status = 403
	if _, err := FetchClash(context.Background(), address); err == nil || strings.Contains(err.Error(), "private-value") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("unsafe/missing error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchClash(ctx, address); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestInvalidClashRejected(t *testing.T) {
	for _, data := range []string{
		"password: secret: invalid:",
		"proxies: []",
		"proxies: [{name: A, type: unsupported, server: example.com, port: 443}]",
		"proxies: [{name: A, type: ss, server: example.com, port: 443, plugin: v2ray-plugin}]",
		"proxies: [{name: A, type: trojan, server: example.com, port: 443}, {name: A, type: trojan, server: example.com, port: 443}]",
		strings.Repeat(" ", maxSubscriptionSize+1),
	} {
		if _, err := ParseClash([]byte(data)); err == nil {
			t.Fatal("accepted invalid subscription")
		}
	}
}

func TestManagementPreservesPolicyAndSelection(t *testing.T) {
	s := testStore(t)
	policy := snapshot(t, s, "config.json")
	node := testNode("manual-a", "192.0.2.99")
	original, _ := s.CompileNode(node)
	if node.Outbound["tag"] != "foreign-tag" || node.Outbound["domain_resolver"] != "foreign-dns" {
		t.Fatal("CompileNode mutated input")
	}
	original["outbounds"].([]any)[0].(map[string]any)["server"] = "changed"
	if node.Outbound["server"] != "192.0.2.99" {
		t.Fatal("CompileNode aliases input")
	}
	if err := s.SaveManual(node); err != nil {
		t.Fatal(err)
	}
	if selection, _ := s.Selection(); selection != (Selection{"manual", "manual-a"}) {
		t.Fatal("edit changed selection")
	}
	sub := Subscription{ID: "provider", Name: "Provider", Nodes: []Node{testNode("a", "192.0.2.5")}}
	if err := s.SaveSubscription(sub); err != nil {
		t.Fatal(err)
	}
	if err := s.Select(Selection{"provider", "a"}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s, "subscriptions.json")
	sub.Nodes = []Node{testNode("b", "192.0.2.6")}
	if err := s.SaveSubscription(sub); err == nil {
		t.Fatal("removed selected node")
	}
	if !bytes.Equal(before, snapshot(t, s, "subscriptions.json")) {
		t.Fatal("failed refresh wrote snapshot")
	}
	if err := s.DeleteSubscription("provider"); err != nil {
		t.Fatal(err)
	}
	if selection, _ := s.Selection(); selection != (Selection{}) {
		t.Fatal("delete failed to clear selection")
	}
	if _, err := s.Compile(); err == nil {
		t.Fatal("silently fell back")
	}
	if err := s.Select(Selection{"manual", "manual-a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteManual("manual-a"); err != nil {
		t.Fatal(err)
	}
	if selection, _ := s.Selection(); selection != (Selection{}) {
		t.Fatal("manual delete failed to clear selection")
	}
	if !bytes.Equal(policy, snapshot(t, s, "config.json")) {
		t.Fatal("management changed policy")
	}
}

func TestNodeLinks(t *testing.T) {
	reality, err := ParseNodeLink("vless://00000000-0000-0000-0000-000000000001@example.com:443?security=reality&pbk=public&sid=ab&flow=xtls-rprx-vision&sni=tls.example#Test")
	if err != nil {
		t.Fatal(err)
	}
	if reality.Name != "Test" || reality.Outbound["tls"].(map[string]any)["reality"] == nil {
		t.Fatal("lost link fields")
	}
	for _, link := range []string{
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:secret")) + "@example.com:443#SS",
		"ss://" + base64.RawStdEncoding.EncodeToString([]byte("aes-128-gcm:secret@example.com:443")) + "#SS",
	} {
		node, err := ParseNodeLink(link)
		if err != nil {
			t.Fatal(err)
		}
		if node.Outbound["method"] != "aes-128-gcm" || node.Outbound["password"] != "secret" {
			t.Fatal("bad SS conversion")
		}
	}
	if _, err := ParseNodeLink("vless://secret@example.com:443?security=reality"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("bad private error")
	}
}
