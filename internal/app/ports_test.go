package app

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"local/loop/internal/inbound"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func availablePorts(t *testing.T) inbound.Ports {
	t.Helper()
	a, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	return inbound.Ports{Default: a.Addr().(*net.TCPAddr).Port, Global: b.Addr().(*net.TCPAddr).Port, GlobalEnabled: true}
}

func TestPortChangesApplyAndRollbackInEveryMode(t *testing.T) {
	for _, mode := range []profile.Mode{profile.ModeDirect, profile.ModeRule, profile.ModeGlobal} {
		for _, fail := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: "/success", true: "/rollback"}[fail], func(t *testing.T) {
				store, core := testutil.CoreStore(t)
				store.SaveMode(mode)
				service := NewService(Options{Store: store, CorePath: core})
				path := filepath.Join(store.Dir, profile.MainConfigName)
				before, _ := os.ReadFile(path)
				ports := availablePorts(t)
				draft, err := profile.ChangeProxyPorts(before, ports)
				if err != nil {
					t.Fatal(err)
				}
				backend := &ruleTestBackend{path: path}
				if fail {
					backend.failures = []error{errors.New("candidate bind lost race"), nil}
				}
				result, err := service.SaveProxyPorts(context.Background(), backend, draft, before, true)
				starts := 1
				if fail {
					starts = 2
				}
				if !result.Running || (err != nil) != fail || len(backend.starts) != starts {
					t.Fatal(result.Running, err, len(backend.starts))
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(result.Document, after) || bytes.Equal(before, after) != fail {
					t.Fatal("source and transaction outcome diverged")
				}
				if fail && !bytes.Equal(backend.starts[1], before) {
					t.Fatal("recovery did not start previous ports")
				}
				old, _ := profile.ParseDocument(before)
				actual, _ := profile.ParseDocument(after)
				delete(old, "proxy_ports")
				delete(actual, "proxy_ports")
				if !reflect.DeepEqual(old, actual) {
					t.Fatal("port change modified routing")
				}
			})
		}
	}
}

func TestPortPreflightNeverInterruptsConnection(t *testing.T) {
	store, core := testutil.CoreStore(t)
	service := NewService(Options{Store: store, CorePath: core})
	path := filepath.Join(store.Dir, profile.MainConfigName)
	before, _ := os.ReadFile(path)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	for _, name := range []string{"occupied default", "occupied global", "stale", "unrelated edit", "duplicate", "invalid"} {
		t.Run(name, func(t *testing.T) {
			ports := availablePorts(t)
			switch name {
			case "occupied default":
				ports.Default = occupied.Addr().(*net.TCPAddr).Port
			case "occupied global":
				ports.Global = occupied.Addr().(*net.TCPAddr).Port
			case "duplicate":
				ports.Global = ports.Default
			case "invalid":
				ports.Global = 0
			}
			draft, err := profile.ChangeProxyPorts(before, ports)
			if name == "duplicate" || name == "invalid" {
				if err == nil {
					t.Fatal("invalid settings accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := before
			if name == "stale" {
				expected = append(append([]byte(nil), before...), '\n')
			}
			if name == "unrelated edit" {
				draft, err = profile.ChangeDefaultPolicy(draft, "US")
				if err != nil {
					t.Fatal(err)
				}
			}
			backend := &ruleTestBackend{path: path}
			result, err := service.SaveProxyPorts(context.Background(), backend, draft, expected, true)
			if err == nil || !result.Running || backend.stops != 0 || len(backend.starts) != 0 {
				t.Fatal("preflight interrupted connection", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected edit was persisted")
			}
		})
	}
}

func TestDisabledGlobalPortRetainsConfigurationAndCanBeOccupied(t *testing.T) {
	store, core := testutil.CoreStore(t)
	service := NewService(Options{Store: store, CorePath: core})
	path := filepath.Join(store.Dir, profile.MainConfigName)
	before, _ := os.ReadFile(path)
	ports := availablePorts(t)
	occupied, err := net.Listen("tcp", inbound.Address(ports.Global))
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	ports.GlobalEnabled = false
	draft, _ := profile.ChangeProxyPorts(before, ports)
	result, err := service.SaveProxyPorts(context.Background(), nil, draft, before, false)
	if err != nil || result.Running {
		t.Fatal(result, err)
	}
	actual, err := store.ProxyPorts()
	if err != nil || actual != ports {
		t.Fatal("disabled port lost", actual, err)
	}
	ports.GlobalEnabled = true
	draft, _ = profile.ChangeProxyPorts(result.Document, ports)
	if _, err := service.SaveProxyPorts(context.Background(), nil, draft, result.Document, false); err == nil {
		t.Fatal("occupied port could be enabled")
	}
}

func TestStrategySelectionStillAppliesToGlobalPortInDirectMode(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "rollback"}[fail], func(t *testing.T) {
			store, core := testutil.CoreStore(t)
			store.SaveMode(profile.ModeDirect)
			if err := store.SaveManual(profile.Node{ID: "b", Name: "B", Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": 9}}); err != nil {
				t.Fatal(err)
			}
			backend := &ruleTestBackend{path: filepath.Join(store.Dir, profile.MainConfigName)}
			if fail {
				backend.failures = []error{errors.New("failed new choice"), nil}
			}
			running, err := SwitchStrategy(context.Background(), backend, store, profile.DefaultPolicy, profile.NodeKey("manual", "b"), true)
			if !running || (err != nil) != fail || len(backend.starts) == 0 {
				t.Fatal(running, err, len(backend.starts), core)
			}
			want := profile.NodeKey("manual", "b")
			if fail {
				want = profile.NodeKey("manual", "a")
			}
			choice, err := store.StrategyChoice(profile.DefaultPolicy)
			if err != nil || choice != want {
				t.Fatal(choice, err)
			}
		})
	}
}
