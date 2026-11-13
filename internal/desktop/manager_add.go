package desktop

import (
	"context"
	"errors"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/profile"
)

// Source creation is a small link-only form; the existing node editor remains
// responsible for editing imported nodes without losing advanced fields.
type sourceEditor struct {
	manager                              *nodeManager
	dialog                               *dialog.CustomDialog
	tabs                                 *container.AppTabs
	nodeName, nodeLink, subName, subLink *widget.Entry
	message                              *widget.Label
	save                                 *widget.Button
}

func (m *nodeManager) addSource() {
	if m.locked {
		return
	}
	m.hideContextMenu()
	newSourceEditor(m).dialog.Show()
}

func newSourceEditor(m *nodeManager) *sourceEditor {
	e := &sourceEditor{manager: m, nodeName: widget.NewEntry(), nodeLink: widget.NewPasswordEntry(), subName: widget.NewEntry(), subLink: widget.NewPasswordEntry()}
	e.nodeName.SetPlaceHolder("可选，留空使用链接中的名称")
	e.nodeLink.SetPlaceHolder("vless://…")
	e.subName.SetPlaceHolder("例如：我的订阅")
	e.subLink.SetPlaceHolder("https://…")
	nodePage := container.NewVBox(wrappedLabel("粘贴一个 VLESS 节点分享链接，保存到本地节点。"), widget.NewForm(
		widget.NewFormItem("节点链接", e.nodeLink), widget.NewFormItem("名称", e.nodeName)))
	subPage := container.NewVBox(wrappedLabel("使用 Clash YAML 订阅链接，获取并校验后保存节点。"), widget.NewForm(
		widget.NewFormItem("订阅链接", e.subLink), widget.NewFormItem("名称", e.subName)))
	e.tabs = container.NewAppTabs(container.NewTabItem("单个节点", nodePage), container.NewTabItem("订阅链接", subPage))
	e.message = wrappedLabel("")
	e.message.Importance = widget.WarningImportance
	e.save = widget.NewButtonWithIcon("检查并添加", theme.ContentAddIcon(), e.submit)
	e.save.Importance = widget.HighImportance
	e.tabs.OnSelected = func(item *container.TabItem) {
		e.message.SetText("")
		if item == e.tabs.Items[1] {
			e.save.SetText("获取并添加")
		} else {
			e.save.SetText("检查并添加")
		}
	}
	footer := container.NewVBox(e.message, container.NewHBox(layout.NewSpacer(), widget.NewButton("取消", func() { e.dialog.Hide() }), e.save))
	e.dialog = dialog.NewCustomWithoutButtons("添加节点或订阅", container.NewBorder(nil, footer, nil, nil, e.tabs), m.window)
	e.dialog.Resize(fyne.NewSize(560, 340))
	return e
}

func parseVLESSImport(link, name string) (profile.Node, error) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(link, "vless://") {
		return profile.Node{}, errors.New("单个节点目前仅支持 vless:// 分享链接")
	}
	if strings.ContainsAny(link, "\r\n\t ") {
		return profile.Node{}, errors.New("请填写单个完整的 VLESS 链接，链接中不能包含空格或换行")
	}
	node, err := profile.ParseNodeLink(link)
	if err != nil {
		return profile.Node{}, err
	}
	if name = strings.TrimSpace(name); name != "" {
		node.Name = name
	}
	return node, nil
}

func (e *sourceEditor) submit() {
	m := e.manager
	// A previously opened dialog must respect locks acquired by other pages.
	if m.locked {
		e.message.SetText("请停止连接并等待当前操作完成后添加。")
		return
	}
	e.message.SetText("")
	if e.tabs.SelectedIndex() == 0 {
		node, err := parseVLESSImport(e.nodeLink.Text, e.nodeName.Text)
		if err != nil {
			e.message.SetText(err.Error())
			return
		}
		e.run("正在检查节点配置…", "manual", "节点已添加，可在策略页选择使用。", func(ctx context.Context) error {
			return m.service.SaveManual(ctx, node)
		})
		return
	}
	name, address := strings.TrimSpace(e.subName.Text), strings.TrimSpace(e.subLink.Text)
	if name == "" {
		e.message.SetText("请填写订阅名称")
		return
	}
	if err := profile.ValidateSubscriptionURL(address); err != nil {
		e.message.SetText(err.Error())
		return
	}
	sub := profile.Subscription{ID: profile.NewID(), Name: name, URL: address, Format: "clash"}
	e.run("正在获取并检查订阅…", sub.ID, "订阅已添加，可在策略页选择使用。", func(ctx context.Context) error {
		return m.service.UpdateSubscription(ctx, sub)
	})
}

func (e *sourceEditor) run(title, sourceID, message string, work func(context.Context) error) {
	m := e.manager
	m.background(title, work, func() {
		e.dialog.Hide()
		m.refresh()
		for i, source := range m.sources {
			if source.ID == sourceID {
				m.selectSource(i)
				break
			}
		}
		m.message.SetText(message)
	})
}
