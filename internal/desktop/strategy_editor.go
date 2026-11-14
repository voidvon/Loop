package desktop

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/profile"
)

type configuredStrategyMember struct {
	id, kind, key, name, detail string
	icon                        fyne.Resource
}

// Only this private draft changes until Save. The chooser has a second draft,
// so cancelling either dialog cannot mutate the stored policy or live choices.
type strategyGroupEditor struct {
	pane                 *strategyPane
	dialog               *dialog.CustomDialog
	chooser              *strategyMemberChooser
	root                 fyne.CanvasObject
	name, filter         *widget.Entry
	message              *widget.Label
	add, save            *widget.Button
	members              *widget.List
	memberHeader         *infoRow
	empty                *widget.Label
	advanced             *widget.Accordion
	draft                profile.PolicyGroup
	groups               []profile.PolicyGroup
	baseline             []byte
	sources              []profile.Subscription
	rows                 []configuredStrategyMember
	adding, busy, closed bool
}

type strategyMemberChooser struct {
	dialog *dialog.CustomDialog
	picker *strategyMemberPicker
	search *widget.Entry
	add    *widget.Button
}

func cloneStrategyGroup(group profile.PolicyGroup) profile.PolicyGroup {
	group.Builtins = slices.Clone(group.Builtins)
	group.Nodes = slices.Clone(group.Nodes)
	group.Sources = slices.Clone(group.Sources)
	group.Groups = slices.Clone(group.Groups)
	return group
}

func newStrategyGroupEditor(p *strategyPane, adding bool) (*strategyGroupEditor, error) {
	e := &strategyGroupEditor{pane: p, adding: adding, baseline: slices.Clone(p.baseline)}
	if !adding {
		if state := p.state(); state != nil {
			e.draft = cloneStrategyGroup(state.Group)
		} else {
			return nil, fmt.Errorf("请先选择策略组")
		}
	}
	for _, state := range p.states {
		e.groups = append(e.groups, state.Group)
	}
	var err error
	e.sources, err = p.controller.service.Store.Sources()
	if err != nil {
		return nil, err
	}
	e.name, e.filter = widget.NewEntry(), widget.NewEntry()
	e.name.SetText(e.draft.Name)
	e.name.SetPlaceHolder("例如 Netflix")
	if !adding {
		e.name.Disable()
	}
	e.filter.SetText(e.draft.Filter)
	e.filter.SetPlaceHolder("可选正则，例如 (?i)香港|HK")
	e.message = wrappedLabel("")
	e.message.Importance = widget.WarningImportance
	e.message.Hide()
	e.add = widget.NewButtonWithIcon("添加", theme.ContentAddIcon(), e.showChooser)
	e.memberHeader = newSectionHeader("已添加成员", "", nil, e.add)
	e.members = widget.NewList(func() int { return len(e.rows) }, func() fyne.CanvasObject {
		row := newListRow(theme.ComputerIcon())
		remove := widget.NewButtonWithIcon("", theme.NewColoredResource(theme.DeleteIcon(), theme.ColorNameError), nil)
		remove.Importance = widget.LowImportance
		row.setTrailing(remove)
		return row
	}, func(id widget.ListItemID, object fyne.CanvasObject) {
		if id < 0 || id >= len(e.rows) {
			return
		}
		item := e.rows[id]
		row := object.(*infoRow)
		row.setText(item.name, item.detail)
		row.setIcon(item.icon)
		remove := row.trailingObject.(*widget.Button)
		remove.OnTapped = func() { e.removeMember(item.kind, item.key) }
		setEnabled(remove, !e.busy && !e.closed)
		row.Refresh()
	})
	e.members.HideSeparators = true
	e.empty = wrappedLabel("尚未添加成员\n点击右上角“添加”选择节点或策略。")
	e.empty.Alignment = fyne.TextAlignCenter
	e.empty.Importance = widget.LowImportance
	memberList := panel(container.NewStack(e.members, container.NewCenter(e.empty)))
	e.advanced = widget.NewAccordion(widget.NewAccordionItem("自动节点筛选（可选）", container.NewVBox(
		widget.NewForm(widget.NewFormItem("名称正则", e.filter)),
		quietLabel("仅筛选“全部节点”和“整个来源”自动加入的节点；单独添加的节点始终保留。"),
	)))
	if e.draft.Filter != "" {
		e.advanced.Open(0)
	}
	e.save = widget.NewButtonWithIcon("保存", theme.DocumentSaveIcon(), e.submit)
	e.save.Importance = widget.HighImportance
	footer := container.NewVBox(e.advanced, e.message, container.NewHBox(layout.NewSpacer(),
		widget.NewButton("取消", func() { e.dialog.Hide() }), e.save))
	basics := panel(inset(8, 8, 8, 8, widget.NewForm(widget.NewFormItem("名称", e.name), widget.NewFormItem("策略类型", widget.NewLabel("手动选择")))))
	e.root = container.NewBorder(container.NewVBox(basics, e.memberHeader), footer, nil, nil, memberList)
	title := "编辑策略组"
	if adding {
		title = "新建策略组"
	}
	e.dialog = dialog.NewCustomWithoutButtons(title, e.root, p.window)
	e.dialog.SetOnClosed(func() {
		e.closed = true
		if e.chooser != nil {
			e.chooser.dialog.Hide()
		}
	})
	e.dialog.Resize(fyne.NewSize(620, 620))
	e.filter.OnChanged = func(value string) {
		if e.closed || e.busy {
			return
		}
		e.draft.Filter = value
		e.refreshMembers()
	}
	e.refreshMembers()
	return e, nil
}

func (e *strategyGroupEditor) refreshMembers() {
	e.rows = nil
	seen := map[string]bool{}
	add := func(kind, key, name, detail string, icon fyne.Resource) {
		id := kind + ":" + key
		if seen[id] {
			return
		}
		seen[id] = true
		e.rows = append(e.rows, configuredStrategyMember{id: id, kind: kind, key: key, name: name, detail: detail, icon: icon})
	}
	if e.draft.AllNodes {
		detail := "本地与所有订阅 · 随节点更新"
		if e.draft.Filter != "" {
			detail = "名称筛选 · " + e.draft.Filter
		}
		add("all", "all", "全部节点", detail, theme.ViewRefreshIcon())
	}
	for _, key := range e.draft.Builtins {
		m := profile.BuiltinMember(key)
		add("builtin", key, m.Name, "内置 · "+m.Detail, theme.ComputerIcon())
	}
	for _, key := range e.draft.Groups {
		add("group", key, key, "子策略组", theme.ListIcon())
	}
	for _, key := range e.draft.Sources {
		name, detail := "已失效来源", key
		for _, source := range e.sources {
			if source.ID == key {
				name, detail = source.Name, "整个来源 · 随节点更新"
				if key == "manual" {
					name = "全部本地节点"
				}
				break
			}
		}
		add("source", key, name, detail, theme.FolderIcon())
	}
	nodes := map[string]configuredStrategyMember{}
	for _, source := range e.sources {
		for _, node := range source.Nodes {
			nodes[profile.NodeKey(source.ID, node.ID)] = configuredStrategyMember{name: node.Name, detail: "节点 · " + source.Name}
		}
	}
	for _, key := range e.draft.Nodes {
		m, ok := nodes[key]
		if !ok {
			m.name, m.detail = "已失效节点", key
		}
		add("node", key, m.name, m.detail, theme.ComputerIcon())
	}
	e.memberHeader.setText("已添加成员", fmt.Sprintf("%d 项", len(e.rows)))
	if len(e.rows) == 0 {
		e.empty.Show()
	} else {
		e.empty.Hide()
	}
	e.members.UnselectAll()
	e.members.Refresh()
	e.root.Refresh()
}

func (e *strategyGroupEditor) removeMember(kind, key string) {
	if e.closed || e.busy {
		return
	}
	remove := func(values []string) []string {
		return slices.DeleteFunc(values, func(value string) bool { return value == key })
	}
	switch kind {
	case "all":
		e.draft.AllNodes = false
	case "builtin":
		e.draft.Builtins = remove(e.draft.Builtins)
	case "group":
		e.draft.Groups = remove(e.draft.Groups)
	case "source":
		e.draft.Sources = remove(e.draft.Sources)
	case "node":
		e.draft.Nodes = remove(e.draft.Nodes)
	}
	e.refreshMembers()
}

func (e *strategyGroupEditor) showChooser() {
	if e.closed || e.busy || e.chooser != nil {
		return
	}
	c := &strategyMemberChooser{picker: newStrategyMemberPicker(profile.PolicyGroup{Name: e.draft.Name}, e.pane.states, e.sources), search: widget.NewEntry()}
	c.picker.existing = map[string]bool{}
	for _, row := range e.rows {
		c.picker.existing[row.id] = true
	}
	c.search.SetPlaceHolder("搜索节点、订阅或策略组")
	empty := quietLabel("没有找到匹配的成员")
	empty.Alignment = fyne.TextAlignCenter
	empty.Hide()
	c.search.OnChanged = func(query string) {
		c.picker.filter(query)
		if c.picker.visible != nil && !c.picker.visible[""] {
			empty.Show()
		} else {
			empty.Hide()
		}
	}
	c.add = widget.NewButtonWithIcon("添加所选", theme.ContentAddIcon(), func() {
		if e.closed || e.busy || e.chooser != c {
			return
		}
		var selected profile.PolicyGroup
		c.picker.apply(&selected)
		e.addMembers(selected)
		c.dialog.Hide()
	})
	c.add.Importance = widget.HighImportance
	c.picker.OnChanged = func() {
		count := len(c.picker.selected)
		c.add.SetText(fmt.Sprintf("添加所选（%d）", count))
		setEnabled(c.add, count > 0)
		if c.dialog != nil {
			c.dialog.Refresh()
		}
	}
	c.picker.OnChanged()
	footer := container.NewHBox(layout.NewSpacer(), widget.NewButton("取消", func() { c.dialog.Hide() }), c.add)
	content := container.NewBorder(container.NewVBox(c.search, quietLabel("勾选订阅父级可添加全部节点并随更新；子节点可单独添加。")), footer, nil, nil, container.NewStack(c.picker.tree, container.NewCenter(empty)))
	c.dialog = dialog.NewCustomWithoutButtons("添加成员", content, e.pane.window)
	c.dialog.SetOnClosed(func() {
		if e.chooser == c {
			e.chooser = nil
		}
	})
	c.dialog.Resize(fyne.NewSize(580, 520))
	e.chooser = c
	c.dialog.Show()
}

func (e *strategyGroupEditor) addMembers(selected profile.PolicyGroup) {
	appendNew := func(values, extra []string) []string {
		for _, key := range extra {
			if !slices.Contains(values, key) {
				values = append(values, key)
			}
		}
		return values
	}
	e.draft.AllNodes = e.draft.AllNodes || selected.AllNodes
	e.draft.Builtins = appendNew(e.draft.Builtins, selected.Builtins)
	e.draft.Groups = appendNew(e.draft.Groups, selected.Groups)
	e.draft.Sources = appendNew(e.draft.Sources, selected.Sources)
	e.draft.Nodes = appendNew(e.draft.Nodes, selected.Nodes)
	e.refreshMembers()
}

func (e *strategyGroupEditor) submit() {
	if e.closed || e.busy {
		return
	}
	next := cloneStrategyGroup(e.draft)
	next.Name, next.Filter = strings.TrimSpace(e.name.Text), e.filter.Text
	if next.Name == "" {
		e.showError(fmt.Errorf("请填写策略组名称"))
		return
	}
	if !next.AllNodes && len(next.Builtins)+len(next.Groups)+len(next.Sources)+len(next.Nodes) == 0 {
		e.showError(fmt.Errorf("请点击“添加”，至少加入一个成员"))
		return
	}
	candidate := slices.Clone(e.groups)
	if e.adding {
		candidate = append(candidate, next)
	} else {
		for i := range candidate {
			if candidate[i].Name == e.draft.Name {
				candidate[i] = next
			}
		}
	}
	e.busy = true
	e.save.Disable()
	e.add.Disable()
	e.pane.commit(candidate, e.baseline, func(err error) {
		e.busy = false
		if e.closed {
			return
		}
		e.save.Enable()
		e.add.Enable()
		if err != nil {
			e.showError(err)
			return
		}
		e.pane.selected = next.Name
		e.pane.Refresh()
		e.dialog.Hide()
	})
}

func (e *strategyGroupEditor) showError(err error) {
	e.message.SetText(err.Error())
	e.message.Show()
	e.root.Refresh()
}
