// Package control owns the background runtime and its local, versioned API.
// Neither the daemon nor its CLI imports Fyne.
package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

const ProtocolVersion = 1

var ErrBusy = errors.New("后台正在处理其他操作，请稍后重试")

type Snapshot struct {
	Protocol       int                      `json:"protocol"`
	Instance       string                   `json:"instance"`
	PID            int                      `json:"pid"`
	Revision       uint64                   `json:"revision"`
	Running        bool                     `json:"running"`
	Busy           bool                     `json:"busy"`
	Closing        bool                     `json:"closing"`
	Mode           profile.Mode             `json:"mode"`
	TUN            bool                     `json:"tun"`
	Profiles       string                   `json:"profiles,omitempty"`
	Config         string                   `json:"config,omitempty"`
	CoreError      string                   `json:"core_error,omitempty"`
	Error          string                   `json:"error,omitempty"`
	PolicyError    string                   `json:"policy_error,omitempty"`
	Policies       []profile.PolicyState    `json:"policies,omitempty"`
	Tray           app.TrayPreferences      `json:"tray"`
	TrayConfigured bool                     `json:"tray_configured"`
	Benchmark      *proxy.BenchmarkProgress `json:"benchmark,omitempty"`
}

type Controller struct {
	Service  *app.Service
	backend  proxy.Backend
	ctx      context.Context
	cancel   context.CancelFunc
	op       sync.Mutex
	mu       sync.Mutex
	state    Snapshot
	Changed  chan struct{}
	Done     chan struct{}
	ShowUI   func() error
	doneOnce sync.Once
}

func New(options app.Options) *Controller {
	s := app.NewService(options)
	d := &proxy.Desktop{CorePath: s.CorePath, ConfigPath: options.ConfigPath, StateDir: options.StateDir, HelperPath: options.HelperPath}
	if options.ConfigPath == "" {
		d.PrepareConfig = func() (string, error) {
			return app.PrepareProfile(s.Store, filepath.Join(options.StateDir, "runtime", "config.json"))
		}
	}
	c := NewWithBackend(s, d)
	d.OnExit = func(err error) {
		c.mu.Lock()
		c.state.Running = false
		if err != nil {
			c.state.Error = err.Error()
		}
		c.changedLocked()
		c.mu.Unlock()
	}
	return c
}

func NewWithBackend(s *app.Service, backend proxy.Backend) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Controller{Service: s, backend: backend, ctx: ctx, cancel: cancel, Changed: make(chan struct{}, 1), Done: make(chan struct{})}
	c.state = Snapshot{Protocol: ProtocolVersion, Instance: profile.NewID(), PID: os.Getpid(), Profiles: s.Store.Dir, Config: s.ConfigPath, Tray: app.DefaultTrayPreferences()}
	var prefs struct {
		Tray app.TrayPreferences `json:"tray"`
	}
	if readPrivateJSON(filepath.Join(s.StateDir, "desktop.json"), &prefs) == nil {
		c.state.Tray, c.state.TrayConfigured = prefs.Tray, true
	}
	c.refresh(false, nil)
	return c
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Controller) changedLocked() {
	c.state.Revision++
	select {
	case c.Changed <- struct{}{}:
	default:
	}
}

func (c *Controller) refresh(running bool, operationErr error) {
	mode, tun := profile.ModeRule, false
	var policies []profile.PolicyState
	var policyErr error
	if c.Service.ConfigPath == "" {
		mode, policyErr = c.Service.Store.Mode()
		tun, _ = c.Service.Store.TUN()
		if states, err := c.Service.Store.PolicyStates(); err != nil {
			policyErr = errors.Join(policyErr, err)
		} else {
			policies = states
		}
	}
	if live, ok := c.backend.(interface{ Running() bool }); ok {
		running = live.Running()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Running, c.state.Mode, c.state.TUN, c.state.Policies = running, mode, tun, policies
	c.state.Busy = false
	c.state.Error, c.state.PolicyError, c.state.CoreError = "", "", ""
	if operationErr != nil {
		c.state.Error = operationErr.Error()
	}
	if policyErr != nil {
		c.state.PolicyError = policyErr.Error()
	}
	if err := c.Service.CoreError(); err != nil {
		c.state.CoreError = err.Error()
	}
	c.changedLocked()
}

func (c *Controller) Execute(requestCtx context.Context, request app.Operation) (result app.OperationResult, err error) {
	if request.Action == "show_ui" {
		if c.ShowUI == nil {
			return result, errors.New("未配置图形界面启动器")
		}
		return result, c.ShowUI()
	}
	if !c.op.TryLock() {
		return result, ErrBusy
	}
	defer c.op.Unlock()
	c.mu.Lock()
	if c.state.Closing {
		c.mu.Unlock()
		return result, errors.New("后台正在退出")
	}
	running := c.state.Running
	c.state.Busy = true
	c.changedLocked()
	c.mu.Unlock()
	defer func() {
		if request.Action == "test_routing" || request.Action == "benchmark" {
			// A diagnostic failure is reported in its pane. Preserve live runtime
			// status and any core-exit error delivered while the probe was running.
			c.mu.Lock()
			c.state.Busy = false
			c.changedLocked()
			c.mu.Unlock()
			return
		}
		c.refresh(running, err)
	}()
	// Connection transactions survive loss of the UI/CLI socket. Only shutdown
	// or their deadline cancels them; a disappearing frontend cannot break rollback.
	ctx, cancel := context.WithTimeout(c.ctx, 3*time.Minute)
	if request.Action == "benchmark" {
		cancel()
		ctx, cancel = context.WithCancel(c.ctx)
	}
	defer cancel()
	// Cancelling a validation, import or probe cancels that task. Connection
	// and recovery transactions are independent of the frontend lifetime.
	switch request.Action {
	case "validate_document", "save_document", "save_manual", "update_subscription", "measure_latencies", "test_routing", "benchmark":
		requestWork, done := context.WithCancel(ctx)
		stop := context.AfterFunc(requestCtx, done)
		if requestCtx.Err() != nil {
			done()
		}
		defer stop()
		defer done()
		ctx = requestWork
	}
	stoppedOnly := func() error {
		if running {
			return errors.New("请先停止代理连接后修改此设置")
		}
		return nil
	}
	s := c.Service
	switch request.Action {
	case "connect":
		if request.Enabled == nil {
			return result, errors.New("缺少 enabled")
		}
		if *request.Enabled == running {
			return result, nil
		}
		if *request.Enabled {
			startCtx, done := context.WithTimeout(ctx, 60*time.Second)
			defer done()
			err = c.backend.Start(startCtx)
			running = err == nil
		} else {
			err = c.backend.Stop()
			running = false
		}
	case "set_mode":
		if s.ConfigPath != "" {
			return result, errors.New("完整配置模式不支持切换分流模式")
		}
		running, err = app.SwitchMode(ctx, c.backend, s.Store, request.Mode, running)
	case "select_policy":
		if s.ConfigPath != "" {
			return result, errors.New("完整配置模式不支持策略选择")
		}
		running, err = app.SwitchStrategy(ctx, c.backend, s.Store, request.Group, request.Key, running)
	case "set_tun":
		if s.ConfigPath != "" {
			return result, errors.New("完整配置模式不支持切换 TUN")
		}
		if request.Enabled == nil {
			return result, errors.New("缺少 enabled")
		}
		running, err = app.SwitchTUN(ctx, c.backend, s.Store, *request.Enabled, running)
	case "save_policies":
		result.Policies, err = s.SavePolicies(ctx, c.backend, request.Data, request.Expected, running)
		running = result.Policies.Running
	case "save_rules":
		result.Rules, err = s.SaveRules(ctx, c.backend, request.Data, request.Expected, running)
		running = result.Rules.Running
	case "validate_document":
		name, _ := s.Document()
		err = s.ValidateDocument(ctx, name, request.Data)
	case "save_document":
		if err = stoppedOnly(); err != nil {
			return result, err
		}
		result.Document, err = s.SaveDocument(ctx, request.Data, request.Expected)
	case "save_manual":
		if err = stoppedOnly(); err != nil {
			return result, err
		}
		if request.Node == nil {
			return result, errors.New("缺少节点")
		}
		err = s.SaveManual(ctx, *request.Node)
	case "update_subscription":
		if err = stoppedOnly(); err != nil {
			return result, err
		}
		if request.Subscription == nil {
			return result, errors.New("缺少订阅")
		}
		err = s.UpdateSubscription(ctx, *request.Subscription)
	case "delete_manual":
		if err = stoppedOnly(); err != nil {
			return result, err
		}
		err = s.DeleteManual(request.ID)
	case "delete_subscription":
		if err = stoppedOnly(); err != nil {
			return result, err
		}
		err = s.DeleteSubscription(request.ID)
	case "measure_latencies":
		var resultsMu sync.Mutex
		err = s.MeasureLatencies(ctx, request.Targets, func(key string, value proxy.LatencyResult) {
			item := app.LatencyValue{Key: key, Duration: value.Duration, Timeout: errors.Is(value.Err, context.DeadlineExceeded), Cancelled: errors.Is(value.Err, context.Canceled)}
			if value.Err != nil {
				item.Error = value.Err.Error()
			}
			resultsMu.Lock()
			result.Latencies = append(result.Latencies, item)
			resultsMu.Unlock()
		})
	case "test_routing":
		result.Routing, err = s.TestRouting(ctx, request.URL)
	case "benchmark":
		if request.Benchmark == nil {
			return result, errors.New("缺少批量测速参数")
		}
		c.mu.Lock()
		c.state.Benchmark = nil
		c.mu.Unlock()
		result.Benchmark, err = s.RunBenchmark(ctx, *request.Benchmark, func(progress proxy.BenchmarkProgress) {
			c.mu.Lock()
			c.state.Benchmark = &progress
			c.changedLocked()
			c.mu.Unlock()
		})
	case "set_tray":
		if request.Tray == nil {
			return result, errors.New("缺少菜单栏设置")
		}
		if err = profile.WriteJSON(filepath.Join(s.StateDir, "desktop.json"), struct {
			Tray app.TrayPreferences `json:"tray"`
		}{*request.Tray}); err == nil {
			c.mu.Lock()
			c.state.Tray, c.state.TrayConfigured = *request.Tray, true
			c.mu.Unlock()
		}
	case "shutdown":
		err = c.backend.Stop()
		running = false
		if err == nil {
			c.mu.Lock()
			c.state.Closing = true
			c.mu.Unlock()
			c.doneOnce.Do(func() { close(c.Done) })
		}
	default:
		err = fmt.Errorf("不支持的操作：%s", request.Action)
	}
	return result, err
}

func (c *Controller) Close() error {
	c.cancel()
	c.op.Lock()
	defer c.op.Unlock()
	err := c.backend.Stop()
	c.mu.Lock()
	c.state.Closing = true
	c.mu.Unlock()
	c.refresh(false, err)
	return err
}
