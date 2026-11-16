package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"local/loop/internal/tunhelper"
)

func tunTestPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "loop-direct-tun-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "socket"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptUnix()
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	server.SetDeadline(time.Now().Add(5 * time.Second))
	return client, server
}

func TestDirectTUNSessionStopWaitsForCleanupAndCanReconnect(t *testing.T) {
	for i := 0; i < 3; i++ {
		client, server := tunTestPair(t)
		session := &tunSession{token: strings.Repeat("a", 64), done: make(chan struct{})}
		disconnected, release, serverDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			defer server.Close()
			encoder := json.NewEncoder(server)
			if err := encoder.Encode(tunhelper.Message{Token: session.token}); err != nil {
				serverDone <- err
				return
			}
			var request tunhelper.Request
			if err := json.NewDecoder(server).Decode(&request); err != nil {
				serverDone <- err
				return
			}
			if request.Core != "fixed-test-core" {
				serverDone <- errors.New("missing configuration")
				return
			}
			if err := encoder.Encode(tunhelper.Message{Started: true, Log: "started\n"}); err != nil {
				serverDone <- err
				return
			}
			_, err := io.Copy(io.Discard, server)
			close(disconnected)
			<-release
			// Cleanup diagnostics must still be read after loopd half-closes.
			if err == nil {
				err = encoder.Encode(tunhelper.Message{Log: "cleaned\n"})
			}
			serverDone <- err
		}()
		var log bytes.Buffer
		if err := session.start(context.Background(), client, tunhelper.Request{Core: "fixed-test-core"}, &log); err != nil {
			t.Fatal(err)
		}
		backend := &Desktop{tun: session, done: session.done}
		if !backend.Running() || backend.cmd != nil {
			t.Fatal("direct TUN must run without a child process")
		}
		stopped := make(chan error, 1)
		go func() { stopped <- backend.Stop() }()
		select {
		case <-disconnected:
		case <-time.After(3 * time.Second):
			t.Fatal("stop did not notify privileged service")
		}
		select {
		case <-session.done:
			t.Fatal("session completed before privileged cleanup")
		default:
		}
		close(release)
		if err := <-stopped; err != nil {
			t.Fatal(err)
		}
		if err := <-serverDone; err != nil {
			t.Fatal(err)
		}
		if backend.Running() || backend.tun != nil || backend.done != nil {
			t.Fatal("stopped backend retained the session")
		}
		if log.String() != "started\ncleaned\n" {
			t.Fatal("lost diagnostics during cleanup", log.String())
		}
	}
}

func TestDirectTUNCancelDuringStartupReleasesConnection(t *testing.T) {
	client, server := tunTestPair(t)
	session := &tunSession{token: strings.Repeat("b", 64), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requestRead := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		defer server.Close()
		json.NewEncoder(server).Encode(tunhelper.Message{Token: session.token})
		var request tunhelper.Request
		err := json.NewDecoder(server).Decode(&request)
		close(requestRead)
		if err == nil {
			_, err = io.Copy(io.Discard, server)
		}
		serverDone <- err
	}()
	go func() { <-requestRead; cancel() }()
	if err := session.start(ctx, client, tunhelper.Request{}, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	backend := &Desktop{tun: session, done: session.done}
	if err := backend.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestDirectTUNRejectsWrongTokenAndReportsServiceFailure(t *testing.T) {
	for _, token := range []string{"wrong", strings.Repeat("c", 64)} {
		t.Run(token[:5], func(t *testing.T) {
			client, server := tunTestPair(t)
			session := &tunSession{token: strings.Repeat("c", 64), done: make(chan struct{})}
			go func() {
				defer server.Close()
				encoder := json.NewEncoder(server)
				encoder.Encode(tunhelper.Message{Token: token})
				if token == session.token {
					var request tunhelper.Request
					json.NewDecoder(server).Decode(&request)
					encoder.Encode(tunhelper.Message{Started: true})
					encoder.Encode(tunhelper.Message{Error: "TUN test core exited"})
				} else {
					io.Copy(io.Discard, server)
				}
			}()
			err := session.start(context.Background(), client, tunhelper.Request{}, io.Discard)
			if (err != nil) != (token != session.token) {
				t.Fatal("incorrect handshake result", err)
			}
			select {
			case <-session.done:
			case <-time.After(3 * time.Second):
				t.Fatal("service exit was not observed")
			}
			if session.exitError() == nil {
				t.Fatal("service error was lost")
			}
			if (&Desktop{tun: session, done: session.done}).Running() {
				t.Fatal("exited service still running")
			}
		})
	}
}
