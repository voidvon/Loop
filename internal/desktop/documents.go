package desktop

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/app"
)

// documentController coordinates UI jobs for both the editor and rule forms.
// Validation and persistence belong to app.Service, not to either view.
// UI fields and callbacks are accessed on Fyne's main goroutine. The stopped
// flag lets shutdown wait for background cleanup without posting new UI work.
type documentController struct {
	window                     fyne.Window
	service                    *app.Service
	name, path                 string
	available, working, closed bool
	cancel                     context.CancelFunc
	HasDraft                   func() bool
	OnSaved                    func([]byte)
	OnBusy                     func(bool)
	OnStateChanged             func()
	Context                    context.Context
	Running                    func() bool
	SaveRules                  func(context.Context, []byte, []byte, bool) (app.RuleSaveResult, error)
	OnRulesApplied             func(app.RuleSaveResult)
	stopped                    atomic.Bool
	done                       chan struct{}
}

func newDocumentController(window fyne.Window, service *app.Service) *documentController {
	name, path := service.Document()
	return &documentController{window: window, service: service, name: name, path: path, available: true}
}

func (c *documentController) dirty() bool { return c.HasDraft != nil && c.HasDraft() }

func (c *documentController) changed() {
	if c.OnStateChanged != nil {
		c.OnStateChanged()
	}
}

func (c *documentController) SetAvailable(available bool) { c.available = available; c.changed() }

func (c *documentController) Run(data, expected []byte, save bool, completed func(error)) {
	c.run(data, expected, save, false, completed)
}

func (c *documentController) rulesAvailable() bool {
	return c.available || c.SaveRules != nil
}

func (c *documentController) RunRules(data, expected []byte, completed func(error)) {
	c.run(data, expected, true, true, completed)
}

func (c *documentController) run(data, expected []byte, save, rules bool, completed func(error)) {
	if c.closed || c.working || (!c.available && !(rules && c.SaveRules != nil)) {
		completed(errors.New("请停止连接并等待其他操作完成后保存"))
		return
	}
	draft, baseline := append([]byte(nil), data...), append([]byte(nil), expected...)
	parent := c.Context
	if parent == nil {
		parent = context.Background()
	}
	wasRunning := c.Running != nil && c.Running()
	ctx, cancel := context.WithCancel(parent)
	if !rules || c.SaveRules == nil {
		cancel()
		ctx, cancel = context.WithTimeout(parent, 20*time.Second)
	}
	c.cancel, c.working = cancel, true
	c.done = make(chan struct{})
	done := c.done
	if c.OnBusy != nil {
		c.OnBusy(true)
	}
	c.changed()
	title := "正在校验配置"
	content := container.NewVBox(widget.NewProgressBarInfinite())
	if rules && wasRunning && c.SaveRules != nil {
		title = "正在保存并应用规则"
		content.Add(widget.NewLabel("如需重连，将自动恢复连接。"))
	} else {
		content.Add(widget.NewButton("取消", cancel))
	}
	progress := dialog.NewCustomWithoutButtons(title, content, c.window)
	progress.Show()
	go func() {
		defer close(done)
		var saved []byte
		var err error
		var ruleResult app.RuleSaveResult
		if rules && c.SaveRules != nil {
			ruleResult, err = c.SaveRules(ctx, draft, baseline, wasRunning)
			saved = ruleResult.Document
		} else if save {
			saved, err = c.service.SaveDocument(ctx, draft, baseline)
		} else {
			err = c.service.ValidateDocument(ctx, c.name, draft)
		}
		cancel()
		if c.stopped.Load() {
			return
		}
		fyne.Do(func() {
			progress.Hide()
			c.working, c.cancel = false, nil
			if c.closed {
				return
			}
			if saved != nil && save && c.OnSaved != nil {
				c.OnSaved(saved)
			}
			if rules && c.SaveRules != nil && c.OnRulesApplied != nil {
				c.OnRulesApplied(ruleResult)
			}
			if c.OnBusy != nil {
				c.OnBusy(false)
			}
			c.changed()
			completed(err)
		})
	}()
}

func (c *documentController) Close() {
	c.closed = true
	c.stopped.Store(true)
	if c.cancel != nil {
		c.cancel()
	}
	if c.done != nil {
		<-c.done
	}
}
