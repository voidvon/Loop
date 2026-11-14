package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/profile"
)

// A list reuses its rows while scrolling. UpdateItem binds both callbacks to
// the row's current IDs, so a context action never targets a different node.
type managementRow struct {
	widget.BaseWidget
	content     fyne.CanvasObject
	onTap       func()
	onSecondary func(fyne.Position)
}

func newManagementRow(content fyne.CanvasObject) *managementRow {
	r := &managementRow{content: content}
	r.ExtendBaseWidget(r)
	return r
}
func (r *managementRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}
func (r *managementRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}
func (r *managementRow) TappedSecondary(e *fyne.PointEvent) {
	if r.onSecondary != nil {
		r.onSecondary(e.AbsolutePosition)
	}
}

func (m *nodeManager) hideContextMenu() {
	if m.contextMenu != nil {
		m.contextMenu.Hide()
		m.contextMenu = nil
	}
}

func (m *nodeManager) showContextMenu(menu *fyne.Menu, position fyne.Position) {
	m.hideContextMenu()
	if menu == nil {
		return
	}
	m.contextMenu = widget.NewPopUpMenu(menu, m.window.Canvas())
	m.contextMenu.ShowAtPosition(position)
}

func (m *nodeManager) sourceByID(id string) *profile.Subscription {
	for _, source := range m.sources {
		if source.ID == id {
			return &source
		}
	}
	return nil
}

func (m *nodeManager) manualByID(id string) *profile.Node {
	if source := m.sourceByID("manual"); source != nil {
		for _, node := range source.Nodes {
			if node.ID == id {
				return &node
			}
		}
	}
	return nil
}

// Guard actions as well as disabling menu items: the connection state can
// change after a menu opens. Resolve IDs again instead of retaining list indexes.
func (m *nodeManager) managementAction(label string, icon fyne.Resource, action func()) *fyne.MenuItem {
	item := fyne.NewMenuItem(label, func() {
		if !m.locked {
			action()
		}
	})
	item.Icon = icon
	item.Disabled = m.locked
	return item
}

func (m *nodeManager) nodeMenu(sourceID, nodeID string) *fyne.Menu {
	if sourceID != "manual" {
		if m.sourceByID(sourceID) == nil {
			return nil
		}
		info := fyne.NewMenuItem("订阅节点由订阅统一管理", nil)
		info.Disabled = true
		return fyne.NewMenu("节点", info)
	}
	if m.manualByID(nodeID) == nil {
		return nil
	}
	return fyne.NewMenu("节点",
		m.managementAction("编辑节点", iconEdit, func() {
			if node := m.manualByID(nodeID); node != nil {
				m.editManual(node)
			}
		}),
		m.managementAction("删除节点", theme.DeleteIcon(), func() {
			if node := m.manualByID(nodeID); node != nil {
				m.confirmDelete("删除节点", "删除“"+node.Name+"”？", func() error { return m.service.DeleteManual(nodeID) })
			}
		}),
	)
}

func (m *nodeManager) subscriptionMenu(sourceID string) *fyne.Menu {
	sub := m.sourceByID(sourceID)
	if sub == nil || sourceID == "manual" {
		return nil
	}
	update := m.managementAction("更新订阅", theme.ViewRefreshIcon(), func() {
		if source := m.sourceByID(sourceID); source != nil && source.URL != "" {
			m.fetchSubscription(*source)
		}
	})
	update.Disabled = update.Disabled || sub.URL == ""
	return fyne.NewMenu("订阅", update,
		m.managementAction("编辑订阅", iconEdit, func() {
			if source := m.sourceByID(sourceID); source != nil {
				m.editSubscription(source)
			}
		}),
		m.managementAction("删除订阅", theme.DeleteIcon(), func() {
			if source := m.sourceByID(sourceID); source != nil {
				m.confirmDelete("删除订阅", "删除“"+source.Name+"”及其中的节点？", func() error { return m.service.DeleteSubscription(sourceID) })
			}
		}),
	)
}
