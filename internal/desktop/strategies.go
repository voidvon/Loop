package desktop

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/profile"
)

type strategyPane struct {
	root                      fyne.CanvasObject
	window                    fyne.Window
	controller                *documentController
	groups                    *widget.List
	members                   *widget.GridWrap
	summary                   *widget.Label
	header                    *infoRow
	status, current           *widget.Label
	add, edit, remove, speed  *widget.Button
	setDefault                *widget.Button
	defaultPolicy             string
	states                    []profile.PolicyState
	rows                      []strategyMemberRow
	baseline                  []byte
	selected                  string
	available, syncing        bool
	loadError                 error
	OnSelect                  func(string, string)
	OnBusy                    func(bool)
	OnChanged                 func()
	latencies                 map[string]probeDisplay
	probing, cancelRequested  bool
	closed                    atomic.Bool
	cancelProbe               context.CancelFunc
	probeDone                 chan struct{}
	probeGroup, probeSummary  string
	probeComplete, probeTotal int
}

func newStrategyPane(window fyne.Window, c *documentController, current *widget.Label) *strategyPane {
	p := &strategyPane{window: window, controller: c, current: current, selected: profile.DefaultPolicy, available: true, latencies: map[string]probeDisplay{}}
	p.status = quietLabel("")
	p.summary = quietLabel("")
	p.groups = widget.NewList(func() int { return len(p.states) }, func() fyne.CanvasObject {
		row := newCollectionRow(strategyGroupIcon("Available"), 28, 7)
		kind := quietLabel("select")
		kind.Wrapping = fyne.TextWrapOff
		row.setTrailing(homeLabel(kind))
		return row
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		if id < 0 || id >= len(p.states) {
			return
		}
		state := p.states[id]
		row := obj.(*infoRow)
		label := p.memberName(state, state.Selected)
		if state.Error != "" {
			label = "暂无可用子策略"
		}
		row.setText(state.Group.Name, label)
		row.setIcon(strategyGroupIcon(state.Group.Name))
	})
	p.groups.OnSelected = func(id widget.ListItemID) {
		if p.syncing || id < 0 || id >= len(p.states) {
			return
		}
		p.selected = p.states[id].Group.Name
		p.refreshDetail()
		p.members.ScrollToTop()
	}
	p.members = widget.NewGridWrap(func() int { return len(p.rows) }, func() fyne.CanvasObject {
		return newMemberCard()
	}, func(id widget.GridWrapItemID, obj fyne.CanvasObject) {
		state := p.state()
		if state == nil || id < 0 || id >= len(p.rows) {
			return
		}
		m := p.rows[id].member
		detail := strategyMemberDetail(m)
		if m.IsGroup {
			for _, child := range p.states {
				if child.Group.Name == m.Key {
					detail = "策略组 · " + p.memberName(child, child.Selected)
					if child.Error != "" {
						detail = "策略组 · 暂无可用成员"
					}
				}
			}
		}
		value, importance := p.latencyLabel(m.Key)
		if value == "" {
			value = "--"
		}
		obj.(*memberCard).update(m.Name, detail, value, importance, strategyMemberIcon(m), m.Key == state.Selected)
	})
	p.members.OnSelected = func(id widget.ListItemID) {
		if p.syncing {
			return
		}
		state := p.state()
		if state == nil || id < 0 || id >= len(p.rows) {
			return
		}
		group, key := state.Group.Name, p.rows[id].member.Key
		p.members.UnselectAll()
		if key == "" {
			return
		}
		if !p.canSelect() {
			return
		}
		if p.OnSelect != nil {
			p.OnSelect(group, key)
		}
	}
	p.add = widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() { p.editGroup(true) })
	p.edit = widget.NewButtonWithIcon("", iconEdit, func() { p.editGroup(false) })
	p.remove = widget.NewButtonWithIcon("", theme.NewColoredResource(iconTrash, theme.ColorNameError), p.deleteGroup)
	p.speed = widget.NewButtonWithIcon("", latencyIcon, p.startLatency)
	p.setDefault = widget.NewButton("设为默认", p.makeDefault)
	p.setDefault.Importance = widget.LowImportance
	actions := container.NewHBox(container.NewThemeOverride(p.setDefault, compactActionTheme{fyne.CurrentApp().Settings().Theme()}),
		collectionAction(p.speed, colorSecondary), collectionAction(p.edit, colorAccent), collectionAction(p.remove, theme.ColorNameError))
	p.header = newSectionHeader("策略组", "", strategyGroupIcon("Available"), actions)
	right := container.NewBorder(inset(0, 20, 0, 0, p.header), nil, nil, nil,
		collectionGrid(p.members, 12, 16, ""))
	columns := collectionColumns(collectionSidebar(p.groups), collectionDetail(right))
	header := collectionPageHeader("策略组", p.summary, collectionAction(p.add, colorAccent))
	p.root = container.NewBorder(header, p.status, nil, nil, columns)
	p.Refresh()
	return p
}
func (p *strategyPane) state() *profile.PolicyState {
	for i := range p.states {
		if p.states[i].Group.Name == p.selected {
			return &p.states[i]
		}
	}
	return nil
}
func (p *strategyPane) memberName(state profile.PolicyState, key string) string {
	for _, m := range state.Members {
		if m.Key == key {
			return m.Name
		}
	}
	return "未选择"
}
func (p *strategyPane) Refresh() {
	defer func() {
		if p.OnChanged != nil {
			p.OnChanged()
		}
	}()
	p.syncing = true
	defer func() { p.syncing = false }()
	p.baseline, p.loadError = os.ReadFile(p.controller.path)
	if p.loadError == nil {
		p.states, p.loadError = p.controller.service.Store.PolicyStates()
	}
	p.defaultPolicy = ""
	if p.loadError == nil {
		var config map[string]any
		config, p.loadError = profile.ParseDocument(p.baseline)
		if p.loadError == nil {
			p.defaultPolicy, p.loadError = profile.DefaultPolicyName(config)
		}
	}
	total := 0
	if sources, err := p.controller.service.Store.Sources(); err == nil {
		for _, source := range sources {
			total += len(source.Nodes)
		}
	}
	p.summary.SetText(fmt.Sprintf("%d 个策略组 · %d 个节点", len(p.states), total))
	p.current.SetText("未设置默认策略 · 请在策略页设置")
	p.groups.Refresh()
	index := 0
	for i, state := range p.states {
		if state.Group.Name == p.selected {
			index = i
		}
		if state.Group.Name == p.defaultPolicy {
			value := p.defaultPolicy + "  →  " + p.memberName(state, state.Selected)
			if state.Error != "" {
				value = p.defaultPolicy + " · 请在策略页选择成员"
			}
			p.current.SetText(value)
		}
	}
	if len(p.states) > 0 {
		p.selected = p.states[index].Group.Name
		p.groups.Select(index)
		if index == 0 {
			p.groups.ScrollToTop()
		}
	}
	p.refreshDetail()
}
func (p *strategyPane) refreshDetail() {
	state := p.state()
	if state == nil {
		p.header.setText("策略组", "暂无策略组")
	} else {
		text := fmt.Sprintf("手动选择 · %d 个子策略", len(state.Members))
		if state.Error != "" {
			text = state.Error
		}
		p.header.setText(state.Group.Name, text)
		p.header.setIcon(strategyGroupIcon(state.Group.Name))
	}
	p.rows = nil
	for _, row := range strategyMemberRows(state) {
		if row.heading == "" {
			p.rows = append(p.rows, row)
		}
	}
	p.members.Refresh()
	p.updateButtons()
}
func (p *strategyPane) canSelect() bool {
	return !p.closed.Load() && p.available && !p.probing && !p.controller.working && !p.controller.closed && !p.controller.dirty() && p.loadError == nil
}
func (p *strategyPane) writable() bool { return p.canSelect() && p.controller.policiesAvailable() }
func (p *strategyPane) updateButtons() {
	if p.add == nil {
		return
	}
	writable := p.writable()
	state := p.state()
	p.speed.SetIcon(latencyIcon)
	if p.probing {
		p.speed.SetIcon(theme.MediaStopIcon())
	}
	setEnabled(p.speed, !p.closed.Load() && ((p.probing && !p.cancelRequested) || (p.canSelect() && state != nil && len(state.Members) > 0)))
	setEnabled(p.add, writable)
	setEnabled(p.edit, writable && state != nil)
	setEnabled(p.remove, writable && state != nil)
	p.setDefault.SetText("设为默认")
	if p.selected == p.defaultPolicy {
		p.setDefault.SetText("默认策略")
	}
	setEnabled(p.setDefault, writable && state != nil && state.Error == "" && p.selected != p.defaultPolicy)
	text := ""
	if p.probeSummary != "" {
		text = p.probeSummary
	}
	if p.probing {
		text = p.probeStatus()
	} else if p.loadError != nil {
		text = p.loadError.Error()
	} else if p.controller.dirty() {
		text = "配置页有未保存的修改，请先保存或重新载入。"
	} else if !p.available || p.controller.working {
		text = "请等待当前操作完成。"
	}
	p.status.SetText(text)
	if text == "" {
		p.status.Hide()
	} else {
		p.status.Show()
	}
}
func (p *strategyPane) commit(groups []profile.PolicyGroup, baseline []byte, done func(error)) {
	if !p.writable() {
		done(fmt.Errorf("请先保存配置草稿，并等待当前操作完成"))
		return
	}
	draft, err := profile.ChangePolicyGroups(baseline, groups)
	if err != nil {
		done(err)
		return
	}
	p.controller.RunPolicies(draft, baseline, func(err error) {
		if err == nil {
			p.Refresh()
		}
		done(err)
	})
}
func (p *strategyPane) editGroup(add bool) {
	if !p.writable() {
		return
	}
	editor, err := newStrategyGroupEditor(p, add)
	if err != nil {
		dialog.ShowError(err, p.window)
		return
	}
	editor.dialog.Show()
}
func (p *strategyPane) deleteGroup() {
	if !p.writable() {
		return
	}
	name := p.selected
	baseline := append([]byte(nil), p.baseline...)
	draft, err := p.controller.service.Store.RemovePolicyGroup(baseline, name)
	if err != nil {
		dialog.ShowError(err, p.window)
		return
	}
	dialog.ShowConfirm("删除策略组", "删除 "+name+"？将同时从其他策略组的可选列表中移除，保留节点和订阅。", func(ok bool) {
		if ok && p.writable() {
			// Recheck choices in case another group was selected while confirming.
			if _, err := p.controller.service.Store.RemovePolicyGroup(baseline, name); err != nil {
				dialog.ShowError(err, p.window)
				return
			}
			p.controller.RunPolicies(draft, baseline, func(err error) {
				if err != nil {
					dialog.ShowError(err, p.window)
				} else {
					p.selected = ""
					p.Refresh()
				}
			})
		}
	}, p.window)
}

func (p *strategyPane) makeDefault() {
	if !p.writable() || p.state() == nil {
		return
	}
	baseline := append([]byte(nil), p.baseline...)
	draft, err := profile.ChangeDefaultPolicy(baseline, p.selected)
	if err != nil {
		dialog.ShowError(err, p.window)
		return
	}
	p.controller.RunPolicies(draft, baseline, func(err error) {
		if err != nil {
			dialog.ShowError(err, p.window)
		}
		p.Refresh()
	})
}
