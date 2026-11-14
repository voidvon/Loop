package tunhelper

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func managedTestConfig() map[string]any {
	return map[string]any{
		"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": 2080}, map[string]any{"type": "tun", "address": []string{"172.19.0.1/30"}, "auto_route": true}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}
}

type testService struct {
	socket, stopped string
	released        atomic.Int32
}

func startTestService(t *testing.T, peer int) *testService {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "loop-service-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := &testService{socket: filepath.Join(dir, "service.sock"), stopped: filepath.Join(dir, "stopped")}
	core := filepath.Join(dir, "installed-core")
	script := "#!/bin/sh\ntrap 'echo stopped >> " + s.stopped + "; exit 0' INT TERM\nprintf 'sing-box started\\n'\nwhile :; do /bin/sleep 0.05; done\n"
	if err := os.WriteFile(core, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveService(ctx, listener, serviceInstallation{UID: 1001, CoreSHA: "pinned"}, core,
			func(net.Conn) (int, error) { return peer, nil }, func() error { return nil },
			func(map[string]any) (func(), <-chan struct{}, error) { return func() { s.released.Add(1) }, nil, nil })
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("service did not clean up")
		}
	})
	return s
}

func (s *testService) connect(t *testing.T) (*net.UnixConn, *json.Decoder) {
	t.Helper()
	conn, err := net.Dial("unix", s.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	if err := json.NewEncoder(conn).Encode(serviceBootstrap{Action: "connect", Token: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	return conn.(*net.UnixConn), json.NewDecoder(conn)
}

func startServiceCore(t *testing.T, conn net.Conn, decoder *json.Decoder) {
	t.Helper()
	var hello Message
	if err := decoder.Decode(&hello); err != nil || hello.Error != "" || hello.Token != strings.Repeat("a", 64) {
		t.Fatal(hello, err)
	}
	config, _ := json.Marshal(managedTestConfig())
	// The installed core must be used, regardless of this untrusted request.
	if err := json.NewEncoder(conn).Encode(Request{Core: "/bin/false", Config: config}); err != nil {
		t.Fatal(err)
	}
	for {
		var message Message
		if err := decoder.Decode(&message); err != nil || message.Error != "" {
			t.Fatal(message, err)
		}
		if message.Started {
			break
		}
	}
}

func TestInstalledServiceSupportsRepeatedConnectionsAndWaitsForCleanup(t *testing.T) {
	s := startTestService(t, 1001)
	for i := 1; i <= 3; i++ {
		conn, decoder := s.connect(t)
		startServiceCore(t, conn, decoder)
		other, otherDecoder := s.connect(t)
		var busy Message
		if err := otherDecoder.Decode(&busy); err != nil || !strings.Contains(busy.Error, "已有连接") {
			t.Fatal("second tunnel accepted", busy, err)
		}
		other.Close()
		conn.CloseWrite()
		for {
			var message Message
			err := decoder.Decode(&message)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if s.released.Load() != int32(i) {
			t.Fatal("EOF arrived before DNS cleanup")
		}
		status, err := serviceStatusAt(context.Background(), s.socket, func(net.Conn) error { return nil })
		if err != nil || status.Active || status.Protocol != ServiceProtocol || status.CoreSHA != "pinned" {
			t.Fatal("service lost readiness after disconnect", status, err)
		}
		conn.Close()
	}
	data, _ := os.ReadFile(s.stopped)
	if strings.Count(string(data), "stopped") != 3 {
		t.Fatal("core was not gracefully stopped on every reconnect")
	}
}

func TestInstalledServiceRejectsAnotherUID(t *testing.T) {
	s := startTestService(t, 1002)
	if _, err := serviceStatusAt(context.Background(), s.socket, func(net.Conn) error { return nil }); err == nil {
		t.Fatal("another UID accessed service")
	}
	if s.released.Load() != 0 {
		t.Fatal("unauthorized request started a core")
	}
}

func TestServiceRelayAcknowledgesPrivilegedCleanup(t *testing.T) {
	s := startTestService(t, 1001)
	listener, err := net.Listen("unix", filepath.Join(filepath.Dir(s.socket), "relay.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	outer, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	local, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	remote, _ := s.connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- relayService(ctx, local.(*net.UnixConn), remote) }()
	outer.SetDeadline(time.Now().Add(5 * time.Second))
	startServiceCore(t, outer, json.NewDecoder(outer))
	outer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("relay did not finish cleanup")
	}
	if s.released.Load() != 1 {
		t.Fatal("relay exited before DNS cleanup")
	}
}
