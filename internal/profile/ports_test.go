package profile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/proxy"
	"local/loop/internal/inbound"
)

func enableTestGlobalPort(t *testing.T, s Store) {
	t.Helper()
	data, err := ChangeProxyPorts(snapshot(t, s, MainConfigName), inbound.DefaultPorts())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, MainConfigName), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalPortIsIndependentOfAllHomepageModes(t *testing.T) {
	s := testStore(t)
	enableTestGlobalPort(t, s)
	before := snapshot(t, s, MainConfigName)
	for _, mode := range []Mode{ModeRule, ModeDirect, ModeGlobal} {
		config, err := s.CompileMode(mode)
		if err != nil {
			t.Fatal(mode, err)
		}
		encoded, _ := json.Marshal(config)
		endpoints, err := inbound.Parse(encoded)
		if err != nil || endpoints.Global != "127.0.0.1:2081" {
			t.Fatal(mode, endpoints, err)
		}
		route := config["route"].(map[string]any)
		rules := route["rules"].([]any)
		if rules[3].(map[string]any)["inbound"] != inbound.GlobalTag || rules[3].(map[string]any)["outbound"] != DefaultPolicy {
			t.Fatal(mode, "global port lost priority")
		}
		if mode == ModeDirect && route["final"] != "direct" {
			t.Fatal("default port stopped following direct mode")
		}
		dns := config["dns"].(map[string]any)
		if dns["rules"].([]any)[1].(map[string]any)["inbound"] != inbound.GlobalTag {
			t.Fatal("global DNS leaked into default policy/cache")
		}
		if config["proxy_ports"] != nil {
			t.Fatal("client settings leaked into core config")
		}
	}
	if !bytes.Equal(before, snapshot(t, s, MainConfigName)) {
		t.Fatal("compilation modified source")
	}
	// The strategy name is a reference, not a hardcoded Available dependency.
	renamed, _ := s.Config()
	renamed["policy_groups"] = []PolicyGroup{{Name: "Proxy", AllNodes: true}}
	renamed["default_policy"] = "Proxy"
	renamed["route"] = map[string]any{"final": "direct"}
	delete(renamed, "dns")
	if err := WriteJSON(filepath.Join(s.Dir, MainConfigName), renamed); err != nil {
		t.Fatal(err)
	}
	config, err := s.CompileMode(ModeDirect)
	if err != nil {
		t.Fatal(err)
	}
	if config["route"].(map[string]any)["rules"].([]any)[3].(map[string]any)["outbound"] != "Proxy" {
		t.Fatal("global did not follow renamed default")
	}
}

func TestGlobalPortRequiresAUsableDefaultEvenInDirectMode(t *testing.T) {
	s := testStore(t)
	enableTestGlobalPort(t, s)
	config, _ := s.Config()
	config["default_policy"] = ""
	config["route"] = map[string]any{"final": "direct"}
	delete(config, "dns")
	WriteJSON(filepath.Join(s.Dir, MainConfigName), config)
	if _, err := s.CompileMode(ModeDirect); err == nil {
		t.Fatal("global silently became direct")
	}
	ports := inbound.DefaultPorts()
	ports.GlobalEnabled = false
	data, _ := ChangeProxyPorts(snapshot(t, s, MainConfigName), ports)
	os.WriteFile(filepath.Join(s.Dir, MainConfigName), data, 0600)
	if _, err := s.CompileMode(ModeDirect); err != nil {
		t.Fatal(err)
	}
}

// Start only loopback mixed listeners with synthetic upstreams. This exercises
// real core routing without modifying system proxy, DNS, TUN or user profiles.
func TestDualPortsWithRealCore(t *testing.T) {
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	var proxyHits atomic.Int32
	upstream := testPortProxy(t, &proxyHits)
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	host, upstreamPort, _ := net.SplitHostPort(upstreamURL.Host)
	portNumber, _ := strconv.Atoi(upstreamPort)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer origin.Close()
	for _, mode := range []Mode{ModeRule, ModeDirect, ModeGlobal} {
		t.Run(string(mode), func(t *testing.T) {
			s := testStore(t)
			enableTestGlobalPort(t, s)
			WriteJSON(filepath.Join(s.Dir, "nodes.json"), NodeList{Nodes: []Node{{ID: "manual-a", Name: "Local fixture", Outbound: map[string]any{"type": "http", "server": host, "server_port": portNumber}}}})
			source, _ := s.Config()
			source["route"] = map[string]any{"default_domain_resolver": "dns-direct", "final": "direct", "rules": []any{map[string]any{"ip_cidr": "198.51.100.0/24", "action": "reject"}}}
			WriteJSON(filepath.Join(s.Dir, MainConfigName), source)
			config, err := s.CompileMode(mode)
			if err != nil {
				t.Fatal(err)
			}
			ports := freeTestPorts(t)
			config["inbounds"] = ports.Inbounds()
			stop := startLocalCore(t, core, config, ports)
			defer stop()
			request := func(port int, target string) (int, error) {
				proxyURL, _ := url.Parse("http://" + inbound.Address(port))
				transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
				defer transport.CloseIdleConnections()
				client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
				response, err := client.Get(target)
				if err != nil {
					return 0, err
				}
				defer response.Body.Close()
				return response.StatusCode, nil
			}
			before := proxyHits.Load()
			if code, err := request(ports.Global, "http://198.51.100.7/test"); err != nil || code != 204 || proxyHits.Load() != before+1 {
				t.Fatal("fixed proxy inlet", code, err)
			}
			for _, port := range []int{ports.Default, ports.Global} {
				if code, err := request(port, origin.URL); err != nil || code != 204 {
					t.Fatal("LAN bypass", code, err)
				}
			}
			if proxyHits.Load() != before+1 {
				t.Fatal("private addresses sent to proxy")
			}
			if mode == ModeRule {
				if code, err := request(ports.Default, "http://198.51.100.7/test"); err == nil && code == 204 {
					t.Fatal("default inlet bypassed saved reject rule")
				}
			} else if mode == ModeGlobal {
				if code, err := request(ports.Default, "http://198.51.100.7/test"); err != nil || code != 204 {
					t.Fatal("homepage global mode", code, err)
				}
			}
			socks, err := proxy.SOCKS5("tcp", inbound.Address(ports.Global), nil, &net.Dialer{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{DialContext: socks.(proxy.ContextDialer).DialContext, DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			before = proxyHits.Load()
			response, err := client.Get("http://198.51.100.7/socks5")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 204 || proxyHits.Load() != before+1 {
				t.Fatal("SOCKS5 did not use fixed global inlet", response.StatusCode)
			}
		})
	}
}

func freeTestPorts(t *testing.T) inbound.Ports {
	t.Helper()
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	return inbound.Ports{Default: first.Addr().(*net.TCPAddr).Port, Global: second.Addr().(*net.TCPAddr).Port, GlobalEnabled: true}
}

func startLocalCore(t *testing.T, core string, config map[string]any, ports inbound.Ports) func() {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := WriteJSON(path, config); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	log, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, core, "run", "-c", path, "-D", dir)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() { cmd.Process.Signal(os.Interrupt); <-done; cancel(); log.Close() }
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			cancel()
			log.Close()
			output, _ := os.ReadFile(log.Name())
			t.Fatalf("core startup: %v\n%s", err, output)
		case <-deadline.C:
			stop()
			output, _ := os.ReadFile(log.Name())
			t.Fatalf("core readiness timed out\n%s", output)
		case <-tick.C:
			ready := true
			for _, port := range []int{ports.Default, ports.Global} {
				conn, err := net.DialTimeout("tcp", inbound.Address(port), 50*time.Millisecond)
				if err != nil {
					ready = false
					break
				}
				conn.Close()
			}
			if ready {
				return stop
			}
		}
	}
}

func testPortProxy(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Error("core did not use HTTP CONNECT")
			w.WriteHeader(405)
			return
		}
		hits.Add(1)
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 200 Connection established\r\n\r\n")
		rw.Flush()
		request, err := http.ReadRequest(bufio.NewReader(rw))
		if err != nil {
			t.Error(err)
			return
		}
		request.Body.Close()
		fmt.Fprint(rw, "HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
		rw.Flush()
	}))
}

func TestGlobalPortDNSAndCacheStaySeparateFromSavedRules(t *testing.T) {
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	var defaultQueries, globalQueries, proxyHits atomic.Int32
	defaultDNS := testPortDNS(t, [4]byte{198, 51, 100, 2}, &defaultQueries)
	globalDNS := testPortDNS(t, [4]byte{198, 51, 100, 3}, &globalQueries)
	upstream := testPortProxy(t, &proxyHits)
	defer upstream.Close()
	host, port, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	upstreamPort, _ := strconv.Atoi(port)
	s := testStore(t)
	enableTestGlobalPort(t, s)
	WriteJSON(filepath.Join(s.Dir, "nodes.json"), NodeList{Nodes: []Node{{ID: "manual-a", Name: "Fixture", Outbound: map[string]any{"type": "http", "server": host, "server_port": upstreamPort}}}})
	source, _ := s.Config()
	source["dns"] = map[string]any{
		"servers": []any{map[string]any{"type": "udp", "tag": "fixture-default", "server": "127.0.0.1", "server_port": defaultDNS}},
		"rules":   []any{map[string]any{"domain_suffix": "test", "action": "route", "server": "fixture-default"}},
		"final":   "fixture-default", "strategy": "ipv4_only",
	}
	source["route"] = map[string]any{"default_domain_resolver": "fixture-default", "final": DefaultPolicy, "rules": []any{map[string]any{"action": "resolve"}}}
	WriteJSON(filepath.Join(s.Dir, MainConfigName), source)
	config, err := s.CompileMode(ModeRule)
	if err != nil {
		t.Fatal(err)
	}
	// Replace only the generated resolver transport with a local synthetic DNS.
	// Rules, resolver identity, precedence and cache behavior remain generated.
	servers := config["dns"].(map[string]any)["servers"].([]any)
	for i, item := range servers {
		server := item.(map[string]any)
		if server["tag"] == "loop-global-dns" {
			servers[i] = map[string]any{"type": "udp", "tag": "loop-global-dns", "server": "127.0.0.1", "server_port": globalDNS}
		}
	}
	ports := freeTestPorts(t)
	config["inbounds"] = ports.Inbounds()
	stop := startLocalCore(t, core, config, ports)
	defer stop()
	request := func(port int) {
		proxyURL, _ := url.Parse("http://" + inbound.Address(port))
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
		response, err := client.Get("http://proxy-ports.test/test")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 204 {
			t.Fatal(response.StatusCode)
		}
	}
	request(ports.Default)
	defaultCount := defaultQueries.Load()
	if defaultCount == 0 || globalQueries.Load() != 0 {
		t.Fatal("default DNS policy was overridden", defaultCount, globalQueries.Load())
	}
	request(ports.Global)
	globalCount := globalQueries.Load()
	if globalCount == 0 || defaultQueries.Load() != defaultCount {
		t.Fatal("global DNS used default cache or resolver", defaultQueries.Load(), globalCount)
	}
	request(ports.Default)
	request(ports.Global)
	if defaultQueries.Load() != defaultCount || globalQueries.Load() != globalCount || proxyHits.Load() != 4 {
		t.Fatal("DNS caches were not retained independently")
	}
}

func testPortDNS(t *testing.T, address [4]byte, hits *atomic.Int32) int {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, client, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil {
				continue
			}
			hits.Add(1)
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true}, Questions: query.Questions}
			for _, question := range query.Questions {
				if question.Type == dnsmessage.TypeA {
					response.Answers = append(response.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: address}})
				}
			}
			data, err := response.Pack()
			if err == nil {
				conn.WriteTo(data, client)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port
}
