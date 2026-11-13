package control

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func netListenUI(path string) (net.Listener, error) {
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

func newUIServer(show, quit func()) *http.Server {
	mux := http.NewServeMux()
	respond := func(action func()) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if action != nil {
				action()
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Envelope{OK: true, Status: Snapshot{Protocol: ProtocolVersion}})
		}
	}
	mux.HandleFunc("GET /show", respond(show))
	mux.HandleFunc("GET /request-quit", respond(quit))
	return &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second}
}

func RequestUIQuit(ctx context.Context, stateDir string) error {
	dir, err := SocketDirectory(stateDir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	client := clientForSocket(filepath.Join(dir, "ui.sock"))
	defer client.Close()
	_, err = client.call(ctx, "/request-quit", nil)
	return err
}

func requestShowUI(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	client := clientForSocket(path)
	defer client.Close()
	_, err := client.call(ctx, "/show", nil)
	return err
}

func FocusUI(ctx context.Context, stateDir string) error {
	dir, err := SocketDirectory(stateDir)
	if err != nil {
		return err
	}
	return requestShowUI(ctx, filepath.Join(dir, "ui.sock"))
}
