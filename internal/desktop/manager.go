package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/app"
	"local/loop/internal/profile"
)

type nodeManager struct {
	stopped    atomic.Bool
	OnBusy     func(bool)
	cancelWork context.CancelFunc
	window     fyne.Window
	store      profile.Store
	service    *app.Service
	root       fyne.CanvasObject

	sources                  []profile.Subscription
	nodeItems                []profile.Node
	sourceIndex              int
	locked, loading          bool
	message                  *widget.Label
	localSource              *choiceButton
	summary                  *widget.Label
	sourceHeader             *infoRow
	emptyNodes, emptySources *widget.Label
	add                      *widget.Button
	nodes                    *widget.GridWrap
	subscriptions            *widget.List
	contextMenu              *widget.PopUpMenu
}

func newNodeManager(window fyne.Window, service *app.Service) *nodeManager {
	m := &nodeManager{window: window, store: service.Store, service: service}
	m.message = quietLabel("右键本地节点或订阅可进行管理。")
	m.message.Wrapping = fyne.TextWrapWord
	m.nodes = widget.NewGridWrap(func() int { return len(m.nodeItems) }, func() fyne.CanvasObject {
		return newManagementRow(newMemberCard())
	}, func(id widget.GridWrapItemID, object fyne.CanvasObject) {
		if id < 0 || id >= len(m.nodeItems) || m.sourceIndex < 0 || m.sourceIndex >= len(m.sources) {
			return
		}
		node := m.nodeItems[id]
		row := object.(*managementRow)
		kind, icon := nodeCardDetails(node)
		row.content.(*memberCard).update(node.Name, kind, "--", widget.LowImportance, icon, false)
		sourceID := m.sources[m.sourceIndex].ID
		row.onTap = func() { m.nodes.Select(id) }
		row.onSecondary = func(position fyne.Position) { m.showContextMenu(m.nodeMenu(sourceID, node.ID), position) }
	})
	m.subscriptions = widget.NewList(func() int { return max(0, len(m.sources)-1) }, func() fyne.CanvasObject {
		return newManagementRow(newCollectionRow(theme.NewColoredResource(iconCloud, colorAccent), 20, 7))
	}, func(id widget.ListItemID, object fyne.CanvasObject) {
		sub := m.sources[id+1]
		item := object.(*managementRow)
		item.content.(*infoRow).setText(sub.Name, fmt.Sprintf("%d 个节点", len(sub.Nodes)))
		item.onTap = func() { m.subscriptions.Select(id) }
		item.onSecondary = func(position fyne.Position) { m.showContextMenu(m.subscriptionMenu(sub.ID), position) }
	})
	m.subscriptions.OnSelected = func(id widget.ListItemID) {
		if !m.loading {
			m.selectSource(id + 1)
		}
	}
	m.buildView()
	m.refresh()
	return m
}

func (m *nodeManager) fail(err error) { dialog.ShowError(err, m.window) }

func (m *nodeManager) refresh() {
	m.hideContextMenu()
	sources, err := m.store.Sources()
	if err != nil {
		m.fail(err)
		return
	}
	previous := "manual"
	if m.sourceIndex < len(m.sources) {
		previous = m.sources[m.sourceIndex].ID
	}
	m.sources = sources
	m.sourceIndex = 0
	for i, source := range sources {
		if source.ID == previous {
			m.sourceIndex = i
		}

	}
	m.syncSources()
	m.refreshNodes()
}

func (m *nodeManager) refreshNodes() {
	m.nodes.UnselectAll()
	m.nodeItems = nil
	if m.sourceIndex >= 0 && m.sourceIndex < len(m.sources) {
		m.nodeItems = m.sources[m.sourceIndex].Nodes
	}
	m.nodes.Refresh()
	m.nodes.ScrollToTop()
	m.refreshSourceDetails()
}

func setEnabled(widget interface {
	Enable()
	Disable()
}, enabled bool) {
	if enabled {
		widget.Enable()
	} else {
		widget.Disable()
	}
}

func (m *nodeManager) SetLocked(value bool) {
	m.locked = value
	if value {
		m.message.SetText("连接期间可浏览，停止连接后可编辑或删除。")
	} else {
		m.message.SetText("右键本地节点或订阅可进行管理。")
	}
	if value {
		m.hideContextMenu()
	}
	setEnabled(m.add, !value)
}

func (m *nodeManager) confirmDelete(title, message string, remove func() error) {
	if m.locked {
		return
	}
	dialog.ShowConfirm(title, message, func(ok bool) {
		if ok && !m.locked {
			if err := remove(); err != nil {
				m.fail(err)
			}
			m.refresh()
		}
	}, m.window)
}

func (m *nodeManager) editSubscription(existing *profile.Subscription) {
	if m.locked {
		return
	}
	name, address := widget.NewEntry(), widget.NewPasswordEntry()
	name.SetPlaceHolder("例如：我的订阅")
	address.SetPlaceHolder("https://…")
	kind := widget.NewSelect([]string{"Clash YAML"}, nil)
	kind.SetSelected("Clash YAML")
	sub := profile.Subscription{ID: profile.NewID(), Format: "clash"}
	if existing != nil {
		sub = *existing
		name.SetText(sub.Name)
		address.SetText(sub.URL)
	}
	name.Validator = func(value string) error {
		if strings.TrimSpace(value) == "" {
			return errors.New("请填写订阅名称")
		}
		return nil
	}
	address.Validator = profile.ValidateSubscriptionURL
	form := dialog.NewForm("订阅设置", "获取并保存", "取消", []*widget.FormItem{
		widget.NewFormItem("名称", name), widget.NewFormItem("订阅类型", kind), widget.NewFormItem("订阅 URL", address),
	}, func(ok bool) {
		if !ok || m.locked {
			return
		}
		sub.Name, sub.URL, sub.Format = strings.TrimSpace(name.Text), strings.TrimSpace(address.Text), "clash"
		m.fetchSubscription(sub)
	}, m.window)
	form.Resize(fyne.NewSize(560, 260))
	form.Show()
}

func (m *nodeManager) fetchSubscription(sub profile.Subscription) {
	if m.locked {
		return
	}
	m.background("正在获取并检查订阅…", func(ctx context.Context) error {
		return m.service.UpdateSubscription(ctx, sub)
	}, func() {
		m.refresh()
		m.message.SetText("订阅“" + sub.Name + "”已保存，分流规则保持不变。")
		for i, source := range m.sources {
			if source.ID == sub.ID {
				m.selectSource(i)
				break
			}
		}
	})
}

func (m *nodeManager) background(title string, work func(context.Context) error, success func()) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	m.cancelWork = cancel
	m.SetLocked(true)
	if m.OnBusy != nil {
		m.OnBusy(true)
	}
	progress := dialog.NewCustomWithoutButtons(title, container.NewVBox(widget.NewProgressBarInfinite(), widget.NewButton("取消", cancel)), m.window)
	progress.Resize(fyne.NewSize(400, 130))
	progress.Show()
	go func() {
		err := work(ctx)
		cancel()
		if m.stopped.Load() {
			return
		}
		fyne.Do(func() {
			if m.stopped.Load() {
				return
			}
			progress.Hide()
			m.cancelWork = nil
			m.SetLocked(false)
			if m.OnBusy != nil {
				m.OnBusy(false)
			}
			if err != nil {
				m.fail(err)
				return
			}
			success()
		})
	}()
}

func (m *nodeManager) Close() {
	m.stopped.Store(true)
	if m.cancelWork != nil {
		m.cancelWork()
	}
}
