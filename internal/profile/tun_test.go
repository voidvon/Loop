package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTUNIndependentFromModesAndProfiles(t *testing.T) {
	s := testStore(t)
	baseline := map[string][]byte{}
	for _, name := range []string{"config.json", "nodes.json", "subscriptions.json", "selection.json"} {
		baseline[name], _ = os.ReadFile(filepath.Join(s.Dir, name))
	}
	if enabled, err := s.TUN(); enabled || err != nil {
		t.Fatal(enabled, err)
	}
	if err := s.SaveTUN(true); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{ModeRule, ModeDirect, ModeGlobal} {
		if err := s.SaveMode(mode); err != nil {
			t.Fatal(err)
		}
		if enabled, err := s.TUN(); !enabled || err != nil {
			t.Fatal("mode reset TUN", enabled, err)
		}
		config, err := s.Compile()
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(config)
		var value struct {
			Inbounds []struct {
				Type      string
				AutoRoute bool `json:"auto_route"`
			}
			Route struct {
				AutoDetect bool `json:"auto_detect_interface"`
				Rules      []map[string]any
			}
			DNS struct{ Servers []struct{ Type string } }
		}
		if err := json.Unmarshal(encoded, &value); err != nil {
			t.Fatal(err)
		}
		last := value.Inbounds[len(value.Inbounds)-1]
		if last.Type != "tun" || !last.AutoRoute || !value.Route.AutoDetect {
			t.Fatal("missing TUN or loop protection")
		}
		if len(value.Route.Rules) < 2 || value.Route.Rules[1]["action"] != "hijack-dns" {
			t.Fatal("DNS bypasses policy")
		}
		for _, server := range value.DNS.Servers {
			if server.Type == "local" {
				t.Fatal("recursive system DNS")
			}
		}
		for name, want := range baseline {
			got, _ := os.ReadFile(filepath.Join(s.Dir, name))
			if !bytes.Equal(got, want) {
				t.Fatal("TUN changed profile", name)
			}
		}
	}
	if err := s.SaveTUN(false); err != nil {
		t.Fatal(err)
	}
	if mode, _ := s.Mode(); mode != ModeGlobal {
		t.Fatal("TUN reset routing mode")
	}
	config, err := s.Compile()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(config)
	if bytes.Contains(data, []byte(`"type":"tun"`)) {
		t.Fatal("TUN still present")
	}
}

// sing-box check accepts a DNS detour to an empty direct outbound, but run
// rejects it. Start the generated DNS/routes for each mode with only a local
// mixed listener, avoiding TUN privileges and changes to the host network.
func TestTUNDNSStartsWithRealCore(t *testing.T) {
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	for _, mode := range []Mode{ModeRule, ModeDirect, ModeGlobal} {
		t.Run(string(mode), func(t *testing.T) {
			s := testStore(t)
			if err := s.SaveTUN(true); err != nil {
				t.Fatal(err)
			}
			config, err := s.CompileMode(mode)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			mixed := config["inbounds"].([]any)[0].(map[string]any)
			mixed["listen_port"] = port
			config["inbounds"] = []any{mixed}
			// The fixture's rule-set path is intentionally absent on disk.
			delete(config["route"].(map[string]any), "rule_set")
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			if err := WriteJSON(configPath, config); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(dir, "core.log")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, core, "run", "-c", configPath, "-D", dir)
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var runErr error
			go func() { runErr = cmd.Wait(); close(done) }()
			defer func() { cmd.Process.Signal(os.Interrupt); <-done }()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					output, _ := os.ReadFile(logPath)
					t.Fatalf("core failed to start: %v\n%s", runErr, output)
				case <-ctx.Done():
					output, _ := os.ReadFile(logPath)
					t.Fatalf("core startup timed out: %v\n%s", ctx.Err(), output)
				case <-ticker.C:
					output, err := os.ReadFile(logPath)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(output), "sing-box started") {
						return
					}
				}
			}
		})
	}
}
