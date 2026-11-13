package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"

	"local/loop/internal/app"
)

type Client struct {
	mu        sync.Mutex
	instance  string
	transport *http.Transport
	http      *http.Client
}

func NewClient(stateDir string) (*Client, error) {
	dir, err := SocketDirectory(stateDir)
	if err != nil {
		return nil, err
	}
	return clientForSocket(filepath.Join(dir, "control.sock")), nil
}

func clientForSocket(path string) *Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}, MaxIdleConns: 2, MaxIdleConnsPerHost: 2}
	return &Client{transport: transport, http: &http.Client{Transport: transport}}
}

func (c *Client) Close() { c.transport.CloseIdleConnections() }

func (c *Client) call(ctx context.Context, path string, operation *app.Operation) (Envelope, error) {
	method := http.MethodGet
	var body io.Reader
	if operation != nil {
		data, err := json.Marshal(operation)
		if err != nil {
			return Envelope{}, err
		}
		body = bytes.NewReader(data)
		method = http.MethodPost
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, body)
	if err != nil {
		return Envelope{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	c.mu.Lock()
	instance := c.instance
	c.mu.Unlock()
	if instance != "" {
		request.Header.Set("X-Loop-Instance", instance)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Envelope{}, fmt.Errorf("无法连接 Loop 后台：%w", err)
	}
	defer response.Body.Close()
	var value Envelope
	if err := json.NewDecoder(io.LimitReader(response.Body, 12<<20)).Decode(&value); err != nil {
		return value, err
	}
	if value.Status.Protocol != ProtocolVersion {
		return value, errors.New("后台协议版本不兼容，请退出旧后台并重启")
	}
	if !value.OK {
		if response.StatusCode == http.StatusConflict {
			return value, ErrBusy
		}
		return value, errors.New(value.Error)
	}
	if value.Status.Instance != "" {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.instance != "" && c.instance != value.Status.Instance {
			return value, errors.New("后台已重新启动，请重新打开界面或重试命令")
		}
		c.instance = value.Status.Instance
	}
	return value, nil
}

func (c *Client) Status(ctx context.Context) (Snapshot, error) {
	value, err := c.call(ctx, "/v1/status", nil)
	return value.Status, err
}
func (c *Client) Execute(ctx context.Context, operation app.Operation) (app.OperationResult, error) {
	value, err := c.call(ctx, "/v1/action", &operation)
	return value.Result, err
}

func (c *Client) Action(ctx context.Context, operation app.Operation) (Envelope, error) {
	return c.call(ctx, "/v1/action", &operation)
}

func Compatible(state Snapshot, options app.Options) error {
	if state.Profiles != options.Store.Dir || state.Config != options.ConfigPath {
		return errors.New("后台正在使用另一份配置；请为独立配置指定不同的 --state-dir，或先退出原后台")
	}
	return nil
}
