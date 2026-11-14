package desktop

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/inbound"
	"local/loop/internal/profile"
)

type portEntry struct {
	widget.Entry
	commit func()
}

func newPortEntry(commit func()) *portEntry {
	e := &portEntry{commit: commit}
	e.ExtendBaseWidget(e)
	e.SetPlaceHolder("1024–65535")
	e.OnSubmitted = func(string) { commit() }
	return e
}

func (e *portEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.commit != nil {
		e.commit()
	}
}

type portsPane struct {
	root                                            fyne.CanvasObject
	controller                                      *documentController
	defaultPort, globalPort                         *portEntry
	globalEnabled                                   *toggleSwitch
	defaultAddress, globalAddress, strategy, status *widget.Label
	copyButtons                                     []*widget.Button
	baseline                                        []byte
	saved                                           inbound.Ports
	available, syncing                              bool
	loadError                                       error
}

func newPortsPane(controller *documentController) *portsPane {
	p := &portsPane{controller: controller, available: true}
	p.defaultPort, p.globalPort = newPortEntry(p.commit), newPortEntry(p.commit)
	p.globalEnabled = newToggleSwitch("启用全局代理入口", func(bool) { p.commit() })
	p.defaultAddress, p.globalAddress = quietLabel(""), quietLabel("")
	p.strategy, p.status = quietLabel(""), wrappedLabel("")
	copyAction := func(global bool, scheme, label string) fyne.CanvasObject {
		button := widget.NewButtonWithIcon(label, theme.ContentCopyIcon(), func() {
			port := p.saved.Default
			if global {
				port = p.saved.Global
			}
			fyne.CurrentApp().Clipboard().SetContent(scheme + "://" + inbound.Address(port))
		})
		button.Importance = widget.LowImportance
		p.copyButtons = append(p.copyButtons, button)
		return button
	}
	portRow := func(entry *portEntry, address *widget.Label) fyne.CanvasObject {
		return container.NewBorder(nil, nil, address, container.NewGridWrap(fyne.NewSize(132, 38), entry))
	}
	defaultCard := panel(container.New(layout.NewCustomPaddedVBoxLayout(8),
		newSectionHeader("默认代理", "跟随首页模式 · 系统代理使用此入口", iconGlobe, nil),
		portRow(p.defaultPort, p.defaultAddress),
		container.New(layout.NewCustomPaddedHBoxLayout(8), copyAction(false, "http", "复制 HTTP"), copyAction(false, "socks5", "复制 SOCKS5")),
	))
	globalCard := panel(container.New(layout.NewCustomPaddedVBoxLayout(8),
		newSectionHeader("全局代理", "独立于首页模式 · 局域网保持直连", iconShield, p.globalEnabled),
		portRow(p.globalPort, p.globalAddress), p.strategy,
		container.New(layout.NewCustomPaddedHBoxLayout(8), copyAction(true, "http", "复制 HTTP"), copyAction(true, "socks5", "复制 SOCKS5")),
	))
	p.root = container.NewVScroll(container.New(layout.NewCustomPaddedVBoxLayout(12),
		sectionTitle("代理端口"), defaultCard, globalCard,
		wrappedLabel("两个入口均支持 HTTP / SOCKS5，仅允许本机访问。首页模式控制默认入口和 TUN；全局入口始终使用默认代理策略。断开连接后两个入口一起停止。"),
		p.status,
	))
	return p
}

func (p *portsPane) hasEdits() bool {
	return p.baseline != nil && (p.defaultPort.Text != strconv.Itoa(p.saved.Default) || p.globalPort.Text != strconv.Itoa(p.saved.Global) || p.globalEnabled.Checked != p.saved.GlobalEnabled)
}

func (p *portsPane) Refresh() {
	if p.controller == nil || p.controller.working || p.hasEdits() {
		return
	}
	if p.controller.name == "" {
		p.status.SetText("完整配置调试模式的代理入口由配置文件管理，请在配置页修改。")
		p.updateButtons()
		return
	}
	data, err := os.ReadFile(p.controller.path)
	if err == nil {
		err = p.load(data)
	}
	p.loadError = err
	if err != nil {
		p.status.SetText("无法读取代理端口：" + err.Error())
	}
	p.updateButtons()
}

func (p *portsPane) load(data []byte) error {
	config, err := profile.ParseDocument(data)
	if err != nil {
		return err
	}
	ports, err := profile.ProxyPorts(config)
	if err != nil {
		return err
	}
	p.syncing = true
	defer func() { p.syncing = false }()
	changed := !bytes.Equal(data, p.baseline)
	p.baseline, p.saved = append([]byte(nil), data...), ports
	p.defaultPort.SetText(strconv.Itoa(ports.Default))
	p.globalPort.SetText(strconv.Itoa(ports.Global))
	p.globalEnabled.SetChecked(ports.GlobalEnabled)
	p.defaultAddress.SetText(inbound.Address(ports.Default))
	globalText := inbound.Address(ports.Global)
	if !ports.GlobalEnabled {
		globalText += " · 已关闭"
	}
	p.globalAddress.SetText(globalText)
	policy, err := profile.DefaultPolicyName(config)
	if err != nil {
		return err
	}
	if policy == "" {
		policy = "未设置"
	}
	p.strategy.SetText("默认策略 · " + policy)
	if changed {
		p.status.SetText("输入完成后按回车或离开输入框，自动保存。连接期间修改会自动重连。")
	}
	return nil
}

func (p *portsPane) SetAvailable(available bool) { p.available = available; p.updateButtons() }

func (p *portsPane) updateButtons() {
	ready := p.available && p.loadError == nil && p.baseline != nil && !p.controller.working && !p.controller.closed && !p.controller.dirty() && p.controller.name != ""
	setEnabled(p.defaultPort, ready)
	setEnabled(p.globalPort, ready)
	setEnabled(p.globalEnabled, ready)
	for i, button := range p.copyButtons {
		setEnabled(button, p.baseline != nil && (i < 2 || p.saved.GlobalEnabled))
	}
}

func (p *portsPane) commit() {
	if p.syncing || !p.hasEdits() {
		return
	}
	if !p.available || p.controller.working || p.controller.closed {
		return
	}
	if p.controller.dirty() {
		p.status.SetText("配置页有未保存的修改，请先保存或撤销后再修改端口。")
		return
	}
	parse := func(value string) (int, error) {
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, errors.New("端口必须是 1024–65535 的整数")
		}
		return port, nil
	}
	reject := func(err error) {
		p.syncing = true
		p.globalEnabled.SetChecked(p.saved.GlobalEnabled)
		p.syncing = false
		p.status.SetText(err.Error())
		p.updateButtons()
	}
	defaultPort, err := parse(p.defaultPort.Text)
	if err != nil {
		reject(err)
		return
	}
	globalPort, err := parse(p.globalPort.Text)
	if err != nil {
		reject(err)
		return
	}
	ports := inbound.Ports{Default: defaultPort, Global: globalPort, GlobalEnabled: p.globalEnabled.Checked}
	draft, err := profile.ChangeProxyPorts(p.baseline, ports)
	if err != nil {
		reject(err)
		return
	}
	p.controller.RunPorts(draft, p.baseline, func(err error) {
		// Read the actual outcome, including successful rollback. Synchronization
		// never triggers another save, and failed candidates never remain visible.
		data, readErr := os.ReadFile(p.controller.path)
		if readErr == nil {
			readErr = p.load(data)
		}
		p.loadError = readErr
		if err != nil {
			p.status.SetText("保存失败：" + err.Error())
		} else if readErr != nil {
			p.status.SetText("读取保存结果失败：" + readErr.Error())
		} else {
			p.status.SetText(fmt.Sprintf("已自动保存 · 默认 %d · 全局 %d", p.saved.Default, p.saved.Global))
		}
		p.updateButtons()
	})
}
