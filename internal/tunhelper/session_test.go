package tunhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSupervisorStopsCoreOnClientLossAndDNSFailure(t *testing.T) {
	for _, failDNS := range []bool{false, true} {
		t.Run(fmt.Sprint(failDNS), func(t *testing.T) {
			dir := t.TempDir()
			stopped := filepath.Join(dir, "stopped")
			core := filepath.Join(dir, "fake-core")
			script := "#!/bin/sh\ntrap 'echo stopped > " + stopped + "; exit 0' INT TERM\nprintf 'sing-box started\\n'\nwhile :; do /bin/sleep 0.05; done\n"
			if err := os.WriteFile(core, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			server, client := net.Pipe()
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			result := make(chan error, 1)
			released := make(chan struct{})
			go func() {
				result <- serve(ctx, server, "test", func(map[string]any) (func(), <-chan struct{}, error) {
					if failDNS {
						return nil, nil, errors.New("DNS failed")
					}
					return func() { close(released) }, nil, nil
				})
			}()
			client.SetDeadline(time.Now().Add(8 * time.Second))
			decoder := json.NewDecoder(client)
			var hello Message
			if err := decoder.Decode(&hello); err != nil || hello.Token != "test" {
				t.Fatal(hello, err)
			}
			if err := json.NewEncoder(client).Encode(Request{Core: core, Config: json.RawMessage(`{"log":{"output":"/invalid/path"},"experimental":{"cache_file":{"enabled":true}}}`)}); err != nil {
				t.Fatal(err)
			}
			for {
				var msg Message
				if err := decoder.Decode(&msg); err != nil {
					t.Fatal(err)
				}
				if failDNS && strings.Contains(msg.Error, "DNS failed") {
					break
				}
				if !failDNS && msg.Started {
					break
				}
			}
			client.Close() // Simulates a GUI crash, without a stop message.
			select {
			case err := <-result:
				if (err != nil) != failDNS {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("supervisor left core running")
			}
			if _, err := os.Stat(stopped); err != nil {
				t.Fatal("core did not receive graceful shutdown", err)
			}
			if !failDNS {
				select {
				case <-released:
				default:
					t.Fatal("DNS lease leaked")
				}
			}
		})
	}
}

// Exercises the actual core's startup log and shutdown protocol, without TUN,
// elevated privileges, network requests, or changes to host proxy/DNS settings.
func TestSupervisorWithRealCore(t *testing.T) {
	core, err := exec.LookPath("sing-box")
	if err != nil {
		t.Skip("sing-box is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config, _ := json.Marshal(map[string]any{
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": port}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
	})
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, "real", func(map[string]any) (func(), <-chan struct{}, error) { return func() {}, nil, nil })
	}()
	client.SetDeadline(time.Now().Add(8 * time.Second))
	decoder := json.NewDecoder(client)
	var hello Message
	if err := decoder.Decode(&hello); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(client).Encode(Request{Core: core, Config: config}); err != nil {
		t.Fatal(err)
	}
	for {
		var message Message
		if err := decoder.Decode(&message); err != nil {
			t.Fatal(err)
		}
		if message.Error != "" {
			t.Fatal(message.Error)
		}
		if message.Started {
			break
		}
	}
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal("started before listener became ready", err)
	}
	conn.Close()
	client.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("core did not stop")
	}
	listener, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatal("core listener leaked", err)
	}
	listener.Close()
}
