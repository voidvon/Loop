package desktop

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/miniproxy/internal/profile"
)

type strategyPane struct {
	root                           fyne.CanvasObject
	window                         fyne.Window
	controller                     *documentController
	groups, members                *widget.List
	title, detail, status, current *widget.Label
	add, edit, remove, speed       *widget.Button
	states                         []profile.PolicyState
	baseline                       []byte
	selected                       string
	available, syncing             bool
	loadError                      error
	OnSelect                       func(string, string)
	OnBusy                         func(bool)
	OnChanged                      func()
	latencies                      map[string]probeDisplay
	probing, cancelRequested       bool
	closed                         atomic.Bool
	cancelProbe                    context.CancelFunc
	probeDone                      chan struct{}
	probeGroup, probeSummary       string
	probeComplete, probeTotal      int
}

func newStrategyPane(window fyne.Window, c *documentController, current *widget.Label) *strategyPane {
	p := &strategyPane{window: window, controller: c, current: current, selected: profile.DefaultPolicy, available: true, latencies: map[string]probeDisplay{}}
	p.title = sectionTitle("")
	p.detail = quietLabel("")
	p.status = quietLabel("")
	p.groups = widget.NewList(func() int { return len(p.states) }, func() fyne.CanvasObject {
		return strategyRow()
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		if id < 0 || id >= len(p.states) {
			return
		}
		state := p.states[id]
		row := obj.(*widget.RichText)
		label := p.memberName(state, state.Selected)
		if state.Error != "" {
			label = "未选择可用节点"
		}
		setStrategyRow(row, state.Group.Name, label)
	})
	p.groups.OnSelected = func(id widget.ListItemID) {
		if p.syncing || id < 0 || id >= len(p.states) {
			return
		}
		p.selected = p.states[id].Group.Name
		p.refreshDetail()
	}
	p.members = widget.NewList(func() int {
		state := p.state()
		if state == nil {
			return 0
		}
		return len(state.Members)
	}, func() fyne.CanvasObject {
		result := widget.NewLabel("")
		result.TextStyle.Bold = true
		return container.NewBorder(nil, nil, nil, container.NewHBox(result, widget.NewIcon(nil)), strategyRow())
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		state := p.state()
		if state == nil || id < 0 || id >= len(state.Members) {
			return
		}
		m := state.Members[id]
		row := obj.(*fyne.Container)
		text := row.Objects[0].(*widget.RichText)
		detail := m.Detail
		if m.IsGroup {
			for _, child := range p.states {
				if child.Group.Name == m.Key {
					detail = "策略组 · " + p.memberName(child, child.Selected)
					if child.Error != "" {
						detail = child.Error
					}
				}
			}
		}
		setStrategyRow(text, m.Name, detail)
		trailing := row.Objects[1].(*fyne.Container)
		label := trailing.Objects[0].(*widget.Label)
		value, importance := p.latencyLabel(m.Key)
		label.Importance = importance
		label.SetText(value)
		label.Refresh()
		icon := trailing.Objects[1].(*widget.Icon)
		icon.SetResource(nil)
		if m.Key == state.Selected {
			icon.SetResource(theme.ConfirmIcon())
		}
	})
	p.members.OnSelected = func(id widget.ListItemID) {
		if p.syncing {
			return
		}
		state := p.state()
		if state == nil || id < 0 || id >= len(state.Members) {
			return
		}
		group, key := state.Group.Name, state.Members[id].Key
		p.members.UnselectAll()
		if !p.canSelect() {
			return
		}
		if p.OnSelect != nil {
			p.OnSelect(group, key)
		}
	}
	p.add = widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() { p.editGroup(true) })
	p.edit = widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() { p.editGroup(false) })
	p.remove = widget.NewButtonWithIcon("", theme.DeleteIcon(), p.deleteGroup)
	p.speed = widget.NewButtonWithIcon("", latencyIcon, p.startLatency)
	right := container.NewBorder(container.NewVBox(container.NewBorder(nil, nil, p.title, container.NewHBox(p.speed, p.edit, p.remove), nil), p.detail, widget.NewSeparator()), nil, nil, nil, p.members)
	split := container.NewHSplit(panel(p.groups), panel(right))
	split.Offset = 0.28
	header := inset(0, 12, 0, 0, container.NewBorder(nil, nil, nil, container.NewCenter(p.add), pageHeading("策略")))
	p.root = container.NewBorder(header, p.status, nil, nil, split)
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
	p.groups.Refresh()
	index := 0
	for i, state := range p.states {
		if state.Group.Name == p.selected {
			index = i
		}
		if state.Group.Name == profile.DefaultPolicy {
			value := "Available  →  " + p.memberName(state, state.Selected)
			if state.Error != "" {
				value = "Available · 请在策略页选择节点"
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
		p.title.SetText("策略组")
		p.detail.SetText("暂无策略组")
	} else {
		p.title.SetText(state.Group.Name)
		text := fmt.Sprintf("手动选择 · %d 个子策略", len(state.Members))
		if state.Group.Name == profile.DefaultPolicy {
			text += " · 全局代理与默认分流出口"
		}
		if state.Error != "" {
			text = state.Error
		} else {
			text += " · 当前：" + p.memberName(*state, state.Selected)
		}
		p.detail.SetText(text)
	}
	p.members.Refresh()
	p.updateButtons()
}
func (p *strategyPane) canSelect() bool {
	return !p.closed.Load() && p.available && !p.probing && !p.controller.working && !p.controller.closed && !p.controller.dirty() && p.loadError == nil
}
func (p *strategyPane) writable() bool { return p.canSelect() && p.controller.available }
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
	setEnabled(p.edit, writable && state != nil && p.selected != profile.DefaultPolicy)
	setEnabled(p.remove, writable && state != nil && p.selected != profile.DefaultPolicy)
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
		done(fmt.Errorf("请停止连接并保存配置草稿后修改策略组"))
		return
	}
	draft, err := profile.ChangePolicyGroups(baseline, groups)
	if err != nil {
		done(err)
		return
	}
	p.controller.Run(draft, baseline, true, func(err error) {
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
	state := p.state()
	g := profile.PolicyGroup{AllNodes: true}
	if !add {
		if state == nil || state.Group.Name == profile.DefaultPolicy {
			return
		}
		g = state.Group
	}
	baseline := append([]byte(nil), p.baseline...)
	groups := make([]profile.PolicyGroup, len(p.states))
	refs := []string{}
	for i, state := range p.states {
		groups[i] = state.Group
		if state.Group.Name != g.Name {
			refs = append(refs, state.Group.Name)
		}
	}
	name := widget.NewEntry()
	name.SetText(g.Name)
	name.SetPlaceHolder("例如 Netflix")
	if !add {
		name.Disable()
	}
	all := widget.NewCheck("包含本地与全部订阅节点", nil)
	all.SetChecked(g.AllNodes)
	filter := widget.NewEntry()
	filter.SetText(g.Filter)
	filter.SetPlaceHolder("可选，按节点名称匹配正则表达式")
	references := widget.NewCheckGroup(refs, nil)
	references.SetSelected(g.Groups)
	message := wrappedLabel("")
	var editor *dialog.CustomDialog
	save := widget.NewButtonWithIcon("保存", theme.DocumentSaveIcon(), func() {
		next := profile.PolicyGroup{Name: strings.TrimSpace(name.Text), AllNodes: all.Checked, Filter: filter.Text, Groups: append([]string(nil), references.Selected...)}
		candidate := append([]profile.PolicyGroup(nil), groups...)
		if add {
			candidate = append(candidate, next)
		} else {
			for i := range candidate {
				if candidate[i].Name == g.Name {
					candidate[i] = next
				}
			}
		}
		p.commit(candidate, baseline, func(err error) {
			if err != nil {
				message.SetText(err.Error())
			} else {
				p.selected = next.Name
				p.Refresh()
				editor.Hide()
			}
		})
	})
	save.Importance = widget.HighImportance
	content := container.NewVBox(widget.NewForm(widget.NewFormItem("名称", name), widget.NewFormItem("节点来源", all), widget.NewFormItem("节点筛选", filter)), quietLabel("可引用的策略组"), references, wrappedLabel("新建应用策略后，请在规则页添加域名或 IP 规则并指向该策略。"))
	footer := container.NewVBox(message, container.NewHBox(layout.NewSpacer(), widget.NewButton("取消", func() { editor.Hide() }), save))
	editor = dialog.NewCustomWithoutButtons("策略组", container.NewBorder(nil, footer, nil, nil, container.NewVScroll(content)), p.window)
	editor.Resize(fyne.NewSize(560, 540))
	editor.Show()
}
func (p *strategyPane) deleteGroup() {
	if !p.writable() || p.selected == profile.DefaultPolicy {
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
			p.controller.Run(draft, baseline, true, func(err error) {
				if err != nil {
					dialog.ShowError(err, p.window)
				} else {
					p.selected = profile.DefaultPolicy
					p.Refresh()
				}
			})
		}
	}, p.window)
}

func strategyRow() *widget.RichText {
	row := widget.NewRichText(&widget.TextSegment{Text: "策略\n", Style: widget.RichTextStyleStrong}, &widget.TextSegment{Text: "当前选择", Style: widget.RichTextStyle{Inline: true, ColorName: theme.ColorNameDisabled, SizeName: theme.SizeNameCaptionText}})
	row.Truncation = fyne.TextTruncateEllipsis
	return row
}
func setStrategyRow(row *widget.RichText, title, detail string) {
	row.Segments[0].(*widget.TextSegment).Text = title + "\n"
	row.Segments[1].(*widget.TextSegment).Text = detail
	row.Refresh()
}
