package desktop

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/profile"
)

type strategyOption struct {
	label, key, kind string
}

// The standard virtualized Tree keeps large subscriptions cheap to display.
// Choices use stable IDs, so duplicate node names across providers are safe.
type strategyMemberPicker struct {
	tree      *widget.Tree
	options   map[string]strategyOption
	children  map[string][]string
	selected  map[string]bool
	existing  map[string]bool
	visible   map[string]bool
	OnChanged func()
}

func newStrategyMemberPicker(group profile.PolicyGroup, states []profile.PolicyState, sources []profile.Subscription) *strategyMemberPicker {
	p := &strategyMemberPicker{options: map[string]strategyOption{}, children: map[string][]string{}, selected: map[string]bool{}}
	branch := func(parent, id, label string) {
		p.children[parent] = append(p.children[parent], id)
		p.children[id] = []string{}
		p.options[id] = strategyOption{label: label}
	}
	option := func(parent, kind, key, label string, checked bool) {
		id := kind + ":" + key
		p.children[parent] = append(p.children[parent], id)
		p.options[id] = strategyOption{label: label, key: key, kind: kind}
		if checked {
			p.selected[id] = true
		}
	}
	branch("", "builtin", "内置")
	branch("", "automatic", "自动包含")
	branch("", "source:manual", "本地节点")
	p.options["source:manual"] = strategyOption{label: "本地节点", key: "manual", kind: "source"}
	if slices.Contains(group.Sources, "manual") {
		p.selected["source:manual"] = true
	}
	branch("", "subscriptions", "订阅节点")
	branch("", "groups", "策略组")
	option("automatic", "all", "all", "全部本地与订阅节点（自动更新）", group.AllNodes)
	for _, key := range []string{"direct", "reject"} {
		m := profile.BuiltinMember(key)
		option("builtin", "builtin", key, m.Name+" · "+m.Detail, slices.Contains(group.Builtins, key))
	}
	for _, source := range sources {
		parent := "source:manual"
		if source.ID != "manual" {
			parent = "source:" + source.ID
			branch("subscriptions", parent, source.Name)
			p.options[parent] = strategyOption{label: source.Name, key: source.ID, kind: "source"}
			if slices.Contains(group.Sources, source.ID) {
				p.selected[parent] = true
			}
		}
		for _, node := range source.Nodes {
			key := profile.NodeKey(source.ID, node.ID)
			option(parent, "node", key, node.Name, slices.Contains(group.Nodes, key))
		}
	}
	for _, state := range states {
		if state.Group.Name != group.Name {
			option("groups", "group", state.Group.Name, state.Group.Name, slices.Contains(group.Groups, state.Group.Name))
		}
	}
	// Keep stale explicit IDs visible and removable instead of losing them on edit.
	for _, field := range []struct {
		kind string
		keys []string
	}{{"node", group.Nodes}, {"source", group.Sources}} {
		for _, key := range field.keys {
			if _, ok := p.options[field.kind+":"+key]; ok {
				continue
			}
			if _, ok := p.children["unavailable"]; !ok {
				branch("", "unavailable", "不可用成员")
			}
			option("unavailable", field.kind, key, "已失效 · "+key, true)
		}
	}
	p.tree = widget.NewTree(func(id string) []string {
		if p.visible == nil {
			return p.children[id]
		}
		var children []string
		for _, child := range p.children[id] {
			if p.visible[child] {
				children = append(children, child)
			}
		}
		return children
	}, func(id string) bool { _, ok := p.children[id]; return ok }, func(bool) fyne.CanvasObject {
		label := widget.NewLabel("")
		label.Truncation = fyne.TextTruncateEllipsis
		return container.NewBorder(nil, nil, widget.NewCheck("", nil), nil, label)
	}, func(id string, _ bool, object fyne.CanvasObject) {
		row := object.(*fyne.Container)
		label := row.Objects[0].(*widget.Label)
		check := row.Objects[1].(*widget.Check)
		item := p.options[id]
		check.OnChanged = nil
		check.SetChecked(p.selected[id] || p.existing[id])
		label.SetText(item.label)
		_, label.TextStyle.Bold = p.children[id]
		label.Importance = widget.MediumImportance
		label.Show()
		if item.kind == "" {
			label.Refresh()
			check.Hide()
			return
		}
		check.Show()
		check.Enable()
		if p.existing[id] {
			check.Disable()
			label.SetText(item.label + " · 已添加")
			label.Importance = widget.LowImportance
		}
		label.Refresh()
		check.OnChanged = func(value bool) {
			if p.existing[id] {
				return
			}
			if value {
				p.selected[id] = true
			} else {
				delete(p.selected, id)
			}
			if p.OnChanged != nil {
				p.OnChanged()
			}
		}
	})
	p.tree.OnSelected = func(id string) {
		if p.options[id].kind != "" && !p.existing[id] {
			if p.selected[id] {
				delete(p.selected, id)
			} else {
				p.selected[id] = true
			}
			p.tree.Refresh()
			if p.OnChanged != nil {
				p.OnChanged()
			}
		}
		p.tree.UnselectAll()
	}
	p.tree.OpenBranch("builtin")
	p.tree.OpenBranch("subscriptions")
	return p
}

func (p *strategyMemberPicker) filter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	p.visible = nil
	if query != "" {
		p.visible = map[string]bool{}
		var visit func(string, bool) bool
		visit = func(id string, parentMatch bool) bool {
			match := parentMatch || (id != "" && strings.Contains(strings.ToLower(p.options[id].label), query))
			found := match
			for _, child := range p.children[id] {
				if visit(child, match) {
					found = true
				}
			}
			p.visible[id] = found
			return found
		}
		visit("", false)
	}
	p.tree.Refresh()
	if query != "" {
		p.tree.OpenAllBranches()
	}
}

func (p *strategyMemberPicker) apply(group *profile.PolicyGroup) {
	group.AllNodes = p.selected["all:all"]
	group.Builtins, group.Nodes, group.Sources, group.Groups = nil, nil, nil, nil
	// Follow the displayed order, rather than Go map iteration, for stable defaults.
	var visit func(string)
	visit = func(parent string) {
		for _, id := range p.children[parent] {
			item := p.options[id]
			if p.selected[id] {
				switch item.kind {
				case "builtin":
					group.Builtins = append(group.Builtins, item.key)
				case "node":
					group.Nodes = append(group.Nodes, item.key)
				case "source":
					group.Sources = append(group.Sources, item.key)
				case "group":
					group.Groups = append(group.Groups, item.key)
				}
			}
			if _, branch := p.children[id]; branch {
				visit(id)
			}
		}
	}
	visit("")
}

type strategyMemberRow struct {
	heading string
	member  profile.PolicyMember
}

func strategyMemberRows(state *profile.PolicyState) []strategyMemberRow {
	if state == nil {
		return nil
	}
	categories := []string{"内置", "本地节点", "订阅节点", "策略组"}
	buckets := map[string][]profile.PolicyMember{}
	for _, m := range state.Members {
		category := "订阅节点"
		if m.IsBuiltin {
			category = "内置"
		} else if m.IsGroup {
			category = "策略组"
		} else if m.Source == "manual" || m.Source == "" {
			category = "本地节点"
		}
		buckets[category] = append(buckets[category], m)
	}
	rows := []strategyMemberRow{}
	for _, category := range categories {
		members := buckets[category]
		if len(members) == 0 {
			continue
		}
		rows = append(rows, strategyMemberRow{heading: fmt.Sprintf("%s · %d", category, len(members))})
		for _, m := range members {
			rows = append(rows, strategyMemberRow{member: m})
		}
	}
	return rows
}
