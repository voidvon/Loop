package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

type testBackend struct {
	mu               sync.Mutex
	running          bool
	starts, stops    int
	entered, release chan struct{}
	failNext         bool
}

func (b *testBackend) Start(ctx context.Context) error {
	b.mu.Lock()
	b.starts++
	fail := b.failNext
	b.failNext = false
	b.mu.Unlock()
	if b.entered != nil {
		select {
		case b.entered <- struct{}{}:
		default:
		}
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if fail {
		return errors.New("synthetic startup failure")
	}
	b.mu.Lock()
	b.running = true
	b.mu.Unlock()
	return nil
}
func (b *testBackend) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stops++
	b.running = false
	return nil
}
func (b *testBackend) Running() bool      { b.mu.Lock(); defer b.mu.Unlock(); return b.running }
func (b *testBackend) counts() (int, int) { b.mu.Lock(); defer b.mu.Unlock(); return b.starts, b.stops }
func newTestController(t *testing.T, b *testBackend) *Controller {
	t.Helper()
	s := testutil.NewStore(t)
	if err := s.UpgradePolicyGroups(); err != nil {
		t.Fatal(err)
	}
	return NewWithBackend(app.NewService(app.Options{Store: s, StateDir: t.TempDir(), CorePath: "/unused/core"}), b)
}
func startTestServer(t *testing.T, c *Controller) (*Client, <-chan error) {
	t.Helper()
	s, err := NewServer(c)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	client, err := NewClient(c.Service.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, done
}
func boolValue(v bool) *bool { return &v }
func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestDetachedClientAndMultipleFrontends(t *testing.T) {
	backend := &testBackend{entered: make(chan struct{}, 1), release: make(chan struct{})}
	c := newTestController(t, backend)
	client, _ := startTestServer(t, c)
	initial, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Execute(ctx, app.Operation{Action: "connect", Enabled: boolValue(true)})
		result <- err
	}()
	select {
	case <-backend.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("start not entered")
	}
	if !c.Snapshot().Busy {
		t.Fatal("accepted operation is not busy")
	}
	if _, err := c.Execute(context.Background(), app.Operation{Action: "set_mode", Mode: profile.ModeDirect}); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent mutation accepted", err)
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("cancelled frontend did not return")
	}
	close(backend.release)
	waitFor(t, func() bool { return !c.Snapshot().Busy && c.Snapshot().Running })
	// A new UI/CLI sees the same runtime without starting another core.
	second, _ := NewClient(c.Service.StateDir)
	defer second.Close()
	state, err := second.Status(context.Background())
	if err != nil || !state.Running || state.Instance != initial.Instance {
		t.Fatal(state, err)
	}
	if _, err := second.Execute(context.Background(), app.Operation{Action: "connect", Enabled: boolValue(true)}); err != nil {
		t.Fatal(err)
	}
	if starts, _ := backend.counts(); starts != 1 {
		t.Fatal("idempotent connect started twice")
	}
	if _, err := second.Execute(context.Background(), app.Operation{Action: "set_mode", Mode: profile.ModeDirect}); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Mode != profile.ModeDirect || !backend.Running() {
		t.Fatal("mode transition lost connection")
	}
	if _, err := second.Execute(context.Background(), app.Operation{Action: "set_tun", Enabled: boolValue(true)}); err == nil {
		t.Fatal("live TUN mutation accepted")
	}
	if _, err := second.Execute(context.Background(), app.Operation{Action: "connect", Enabled: boolValue(false)}); err != nil {
		t.Fatal(err)
	}
	if c.Snapshot().Running {
		t.Fatal("disconnect left stale running state")
	}
}

func TestDaemonLockPermissionsAndShutdown(t *testing.T) {
	c := newTestController(t, &testBackend{})
	client, _ := startTestServer(t, c)
	dir, err := SocketDirectory(c.Service.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, "control.sock"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatal("insecure local endpoint", err)
		}
	}
	if _, err := NewServer(c); err == nil {
		t.Fatal("second daemon acquired same lock")
	}
	called := false
	if err := WithOfflineMutation(c.Service.StateDir, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("offline command raced running daemon")
	}
	value, err := client.Action(context.Background(), app.Operation{Action: "shutdown"})
	if err != nil || !value.Status.Closing {
		t.Fatal(value, err)
	}
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(dir, "control.sock")); return os.IsNotExist(err) })
	waitFor(t, func() bool { return WithOfflineMutation(c.Service.StateDir, func() error { return nil }) == nil })
}

func TestStrictAPIAndInstanceGuard(t *testing.T) {
	c := newTestController(t, &testBackend{})
	defer c.Close()
	handler := (&Server{Controller: c}).Handler()
	for _, tc := range []struct {
		method, body string
		code         int
	}{
		{"POST", `{"action":"set_tun","enabled":true,"unknown":1}`, 400},
		{"POST", `{"action":"set_tun","enabled":true} {}`, 400},
		{"POST", `{"action":"connect"}`, 400},
		{"POST", `null`, 400},
		{"GET", "", 405},
		{"POST", `{"action":"set_tun","enabled":true,"name":"` + string(bytes.Repeat([]byte("a"), 8<<20)) + `"}`, 400},
	} {
		r := httptest.NewRequest(tc.method, "/v1/action", bytes.NewBufferString(tc.body))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s => %d", tc.method, w.Code)
		}
	}
	request := httptest.NewRequest("POST", "/v1/action", bytes.NewBufferString(`{"action":"set_tun","enabled":true}`))
	request.Header.Set("X-Loop-Instance", "old-daemon")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != http.StatusPreconditionFailed || c.Snapshot().TUN {
		t.Fatal("stale frontend mutated new daemon")
	}
	data, _ := json.Marshal(c.Snapshot())
	if bytes.Contains(data, []byte("password")) || bytes.Contains(data, []byte("outbound")) {
		t.Fatal("status exposed raw node credentials")
	}
}

func TestClientCannotControlRestartedDaemon(t *testing.T) {
	c := newTestController(t, &testBackend{})
	client, _ := startTestServer(t, c)
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Simulate replacement under the same endpoint; the pinned client must fail.
	c.mu.Lock()
	c.state.Instance = profile.NewID()
	c.mu.Unlock()
	if _, err := client.Execute(context.Background(), app.Operation{Action: "set_tun", Enabled: boolValue(true)}); err == nil || c.Snapshot().TUN {
		t.Fatal("old client changed restarted daemon")
	}
	fresh, _ := NewClient(c.Service.StateDir)
	defer fresh.Close()
	if _, err := fresh.Execute(context.Background(), app.Operation{Action: "set_tun", Enabled: boolValue(true)}); err != nil {
		t.Fatal(err)
	}
}

func TestTrayPersistenceAndUIEndpoint(t *testing.T) {
	c := newTestController(t, &testBackend{})
	defer c.Close()
	prefs := app.TrayPreferences{}
	if _, err := c.Execute(context.Background(), app.Operation{Action: "set_tray", Tray: &prefs}); err != nil {
		t.Fatal(err)
	}
	reopened := NewWithBackend(c.Service, &testBackend{})
	defer reopened.Close()
	if !reopened.Snapshot().TrayConfigured || reopened.Snapshot().Tray != prefs {
		t.Fatal("background preferences lost")
	}
	show, quit := make(chan struct{}, 1), make(chan struct{}, 1)
	cleanup, err := UIEndpoint(c.Service.StateDir, func() { show <- struct{}{} }, func() { quit <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := UIEndpoint(c.Service.StateDir, nil, nil); err == nil {
		t.Fatal("duplicate UI accepted")
	}
	if err := FocusUI(context.Background(), c.Service.StateDir); err != nil {
		t.Fatal(err)
	}
	if err := RequestUIQuit(context.Background(), c.Service.StateDir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-show:
	default:
		t.Fatal("show not delivered")
	}
	select {
	case <-quit:
	default:
		t.Fatal("quit not delivered")
	}
}

func TestRefuseNonSocketAtEndpoint(t *testing.T) {
	c := newTestController(t, &testBackend{})
	defer c.Close()
	dir, _ := SocketDirectory(c.Service.StateDir)
	path := filepath.Join(dir, "control.sock")
	if err := os.WriteFile(path, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	if _, err := NewServer(c); err == nil {
		t.Fatal("overwrote ordinary file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "retain" {
		t.Fatal("file corrupted")
	}
}

func TestRulesRollbackAcrossRPC(t *testing.T) {
	store, core := testutil.CoreStore(t)
	backend := &testBackend{}
	c := NewWithBackend(app.NewService(app.Options{Store: store, CorePath: core, StateDir: t.TempDir()}), backend)
	client, _ := startTestServer(t, c)
	if _, err := client.Execute(context.Background(), app.Operation{Action: "connect", Enabled: boolValue(true)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir, profile.MainConfigName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := profile.ChangeRouting(before, profile.RuleChange{Operation: "final", Final: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.failNext = true
	backend.mu.Unlock()
	result, err := client.Execute(context.Background(), app.Operation{Action: "save_rules", Data: draft, Expected: before})
	if err == nil || !result.Rules.Running || !bytes.Equal(result.Rules.Document, before) {
		t.Fatal("RPC lost rollback outcome", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) || !c.Snapshot().Running {
		t.Fatal("rollback did not restore config and connection")
	}
}

func TestCancelledDocumentRequestCannotWrite(t *testing.T) {
	c := newTestController(t, &testBackend{})
	defer c.Close()
	path := filepath.Join(c.Service.Store.Dir, profile.MainConfigName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Execute(ctx, app.Operation{Action: "save_document", Data: before, Expected: before}); !errors.Is(err, context.Canceled) {
		t.Fatal("document cancellation was ignored", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) {
		t.Fatal("cancelled request wrote document")
	}
}
