package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"local/loop/internal/app"
)

type Envelope struct {
	OK     bool                `json:"ok"`
	Error  string              `json:"error,omitempty"`
	Status Snapshot            `json:"status"`
	Result app.OperationResult `json:"result"`
}

type Server struct {
	Controller *Controller
	listener   net.Listener
	http       *http.Server
	lock       *os.File
	path       string
}

func NewServer(controller *Controller) (*Server, error) {
	if err := os.MkdirAll(controller.Service.StateDir, 0700); err != nil {
		return nil, err
	}
	dir, err := SocketDirectory(controller.Service.StateDir)
	if err != nil {
		return nil, err
	}
	lock, err := lockFile(filepath.Join(dir, "daemon.lock"))
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "control.sock")
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			unlockFile(lock)
			return nil, errors.New("本地通信地址被普通文件占用")
		}
		if err = os.Remove(path); err != nil {
			unlockFile(lock)
			return nil, err
		}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		unlockFile(lock)
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		unlockFile(lock)
		return nil, err
	}
	s := &Server{Controller: controller, listener: listener, lock: lock, path: path}
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, BaseContext: func(net.Listener) context.Context { return controller.ctx }}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, code int, result app.OperationResult, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		value := Envelope{OK: err == nil, Status: s.Controller.Snapshot(), Result: result}
		if err != nil {
			value.Error = err.Error()
		}
		_ = json.NewEncoder(w).Encode(value)
	}
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, app.OperationResult{}, nil) })
	mux.HandleFunc("POST /v1/action", func(w http.ResponseWriter, r *http.Request) {
		var operation app.Operation
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&operation); err != nil {
			reply(w, 400, app.OperationResult{}, err)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			reply(w, 400, app.OperationResult{}, errors.New("请求只能包含一个 JSON 对象"))
			return
		}
		result, err := s.Controller.Execute(r.Context(), operation)
		code := http.StatusOK
		if errors.Is(err, ErrBusy) {
			code = http.StatusConflict
		} else if err != nil {
			code = http.StatusBadRequest
		}
		reply(w, code, result, err)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if instance := r.Header.Get("X-Loop-Instance"); instance != "" && instance != s.Controller.Snapshot().Instance {
			reply(w, http.StatusPreconditionFailed, app.OperationResult{}, errors.New("后台已重新启动，请重新打开界面或重试命令"))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) Serve(ctx context.Context) error {
	defer unlockFile(s.lock)
	defer os.Remove(s.path)
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- s.http.Serve(s.listener) }()
	select {
	case <-ctx.Done():
		s.Controller.cancel()
	case <-s.Controller.Done:
	case err := <-errorsCh:
		return errors.Join(err, s.Controller.Close())
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		_ = s.http.Close()
	}
	return s.Controller.Close()
}
