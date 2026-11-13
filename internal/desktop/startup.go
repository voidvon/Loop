package desktop

import (
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"local/miniproxy/internal/autostart"
)

type startupService interface {
	Status() (autostart.Status, error)
	SetEnabled(bool) (autostart.Status, error)
	OpenSettings()
}

type startupPane struct {
	root                  fyne.CanvasObject
	toggle                *widget.Check
	message               *widget.Label
	settings              *widget.Button
	feedback              *fyne.Container
	service               startupService
	post                  func(func())
	state                 autostart.Status
	busy, syncing, closed bool
	stopped               atomic.Bool
}

func newStartupPane() *startupPane {
	p := &startupPane{post: fyne.Do}
	p.toggle = widget.NewCheck("启用", func(enabled bool) {
		if !p.syncing && !p.busy && !p.closed && p.service != nil {
			p.run(func() (autostart.Status, error) { return p.service.SetEnabled(enabled) })
		}
	})
	p.toggle.Disable()
	p.message = quietLabel("请使用打包后的 MiniProxy.app 设置登录自启。")
	p.settings = widget.NewButton("打开登录项设置", func() {
		if !p.closed && !p.busy && p.service != nil {
			p.service.OpenSettings()
		}
	})
	p.settings.Hide()
	detail := quietLabel("进入 macOS 后自动打开主应用")
	detail.Wrapping = fyne.TextWrapOff
	row := container.NewBorder(nil, nil, nil, container.NewCenter(p.toggle), container.NewVBox(
		sectionTitle("登录时启动 MiniProxy"), detail,
	))
	p.feedback = container.NewVBox(p.message, container.NewHBox(p.settings))
	card := panel(inset(12, 12, 12, 12, container.NewVBox(
		row, p.feedback,
	)))
	p.root = container.NewVScroll(container.NewVBox(card))
	return p
}

func (p *startupPane) attach(service startupService) {
	p.service = service
	p.Refresh()
}

func (p *startupPane) Refresh() {
	if !p.closed && !p.busy && p.service != nil {
		p.run(p.service.Status)
	}
}

func (p *startupPane) run(work func() (autostart.Status, error)) {
	p.busy = true
	p.toggle.Disable()
	p.settings.Disable()
	p.message.Importance = widget.LowImportance
	p.message.SetText("正在更新登录项状态…")
	p.message.Show()
	p.feedback.Show()
	p.root.Refresh()
	go func() {
		state, err := work()
		if p.stopped.Load() {
			return
		}
		p.post(func() {
			if p.closed {
				return
			}
			p.busy = false
			p.apply(state, err)
		})
	}()
}

func (p *startupPane) apply(state autostart.Status, err error) {
	p.state = state
	p.syncing = true
	// A pending registration stays checked so it can also be cancelled. The
	// warning explicitly distinguishes registration from an effective login item.
	p.toggle.SetChecked(state == autostart.Enabled || state == autostart.RequiresApproval)
	p.syncing = false
	setEnabled(p.toggle, state != autostart.Unavailable)
	p.settings.Enable()
	p.settings.Hide()
	p.message.Importance = widget.LowImportance
	p.message.Hide()
	p.feedback.Hide()
	if state == autostart.RequiresApproval {
		p.message.Importance = widget.WarningImportance
		p.message.SetText("尚未生效，请在 macOS 登录项设置中允许 MiniProxy。")
		p.message.Show()
		p.settings.Show()
		p.feedback.Show()
	}
	if err != nil {
		p.message.Importance = widget.DangerImportance
		p.message.SetText("登录项设置失败：" + err.Error())
		p.message.Show()
		p.feedback.Show()
	}
	p.root.Refresh()
}

func (p *startupPane) Close() {
	p.closed = true
	p.stopped.Store(true)
}
