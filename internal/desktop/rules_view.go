package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/profile"
)

func (p *rulesPane) buildView() {
	p.summary, p.status = quietLabel(""), quietLabel("")
	p.summary.Wrapping = fyne.TextWrapOff
	p.status.Hide()
	p.body = container.NewStack()
	p.final = widget.NewSelect(nil, p.changeFinal)
	finalHeader := newInfoRow(theme.SizeNameSubHeadingText, 8, 0, 20, nil, nil)
	finalHeader.setText("兜底", "未命中其他规则时使用")
	finalHeader.setLeading(collectionIcon(widget.NewIcon(theme.NewColoredResource(iconFlag, colorAccent)), 36))
	p.finalPage = container.NewVScroll(container.NewVBox(
		inset(0, 20, 0, 0, finalHeader),
		widget.NewForm(widget.NewFormItem("处理策略", p.final)),
	))
	p.sectionHeader = newInfoRow(theme.SizeNameSubHeadingText, 8, 0, 20, nil, nil)
	p.sectionHeader.detail.Wrapping = fyne.TextWrapWord
	p.sectionHeader.detail.Truncation = fyne.TextTruncateOff
	p.sectionHeader.setLeading(collectionIcon(widget.NewIcon(theme.NewColoredResource(iconConfig, colorAccent)), 36))
	p.empty = widget.NewLabelWithStyle("暂无规则\n点击右上角 + 添加", fyne.TextAlignCenter, fyne.TextStyle{})
	p.empty.Importance = widget.LowImportance
	p.cardWidth = 224
	p.list = widget.NewGridWrap(func() int { return len(p.filtered) }, func() fyne.CanvasObject { card := newRuleCard(); card.minWidth = p.cardWidth; return card }, func(id widget.GridWrapItemID, obj fyne.CanvasObject) { p.updateRuleCard(id, obj.(*ruleCard)) })
	p.list.OnSelected = func(id widget.GridWrapItemID) {
		if id >= 0 && id < len(p.filtered) {
			p.selected = p.filtered[id]
			// Space activates the same menu as a pointer click or right click.
			section, index := p.section, p.selected
			baseline := append([]byte(nil), p.baseline...)
			p.list.UnselectAll()
			p.showRuleMenu(index, section, baseline, fyne.CurrentApp().Driver().AbsolutePositionForObject(p.list))
		}
	}
	grid := collectionGrid(p.list, 24, 14, "")
	p.localPage = container.NewBorder(
		inset(0, 14, 0, 0, p.sectionHeader),
		nil, nil, nil, container.NewStack(container.New(ruleGridLayout{p}, grid), container.NewCenter(p.empty)))
	p.groups = widget.NewList(func() int { return 2 + len(p.remoteSets) }, func() fyne.CanvasObject {
		row := newCollectionRow(theme.NewColoredResource(iconConfig, colorAccent), 20, 10)
		row.setTargetBadge("按规则设置", "")
		return row
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		row := obj.(*infoRow)
		name, detail, target, icon := "本地规则", "按规则设置", "", iconConfig
		if id == 0 {
			target = p.resolvedTarget(p.routing.Final)
			name, detail, icon = "兜底", compactRuleTarget(target), iconFlag
		} else if id >= 2 {
			if id-2 >= len(p.remoteSets) {
				return
			}
			index := p.remoteSets[id-2]
			name = strings.Join(profile.RuleSetTags(p.routing.RuleSets[index]["tag"]), " · ")
			target = p.resolvedTarget(p.subscriptionTarget(index))
			detail, icon = compactRuleTarget(target), iconCloud
			if _, simple := p.subscriptionBinding(index); !simple {
				detail, target = "按规则设置", ""
			}
		}
		row.setText(name, detail)
		row.setTargetBadge(detail, target)
		row.setIcon(theme.NewColoredResource(icon, colorAccent))
	})
	p.groups.HideSeparators = true
	p.groups.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= p.groups.Length() {
			return
		}
		p.hideRuleMenu()
		p.section = id
		p.showSection()
		p.filter()
		p.list.ScrollToTop()
	}
	p.add = widget.NewButtonWithIcon("", iconAdd, p.addRuleDialog)
	p.reload = widget.NewButtonWithIcon("", iconRefresh, p.Refresh)
	header := collectionPageHeader("所有规则", p.summary, collectionAction(p.reload, colorSecondary), collectionAction(p.add, colorAccent))
	columns := collectionColumns(collectionSidebar(p.groups), collectionDetail(p.body))
	p.root = container.NewBorder(header, p.status, nil, nil, columns)
}

func (p *rulesPane) showSection() {
	page := p.localPage
	if p.section == 0 {
		page = p.finalPage
	}
	p.body.Objects = []fyne.CanvasObject{page}
	p.body.Refresh()
}

func (p *rulesPane) refreshSection() {
	title, detail, icon := "本地规则", fmt.Sprintf("%d 条本地规则", len(p.localIndexes())), iconConfig
	if p.section >= 2 && p.section-2 < len(p.remoteSets) {
		index := p.remoteSets[p.section-2]
		set := p.routing.RuleSets[index]
		title = strings.Join(profile.RuleSetTags(set["tag"]), " · ")
		interval := fieldText(set["update_interval"])
		if interval == "" {
			interval = "1d"
		}
		detail = subscriptionFormatLabel(set) + " · " + interval + " 更新 · " + fieldText(set["url"])
		icon = iconCloud
	}
	p.sectionHeader.setText(title, detail)
	p.sectionHeader.setLeading(collectionIcon(widget.NewIcon(theme.NewColoredResource(icon, colorAccent)), 36))
	if len(p.filtered) == 0 {
		p.empty.Show()
	} else {
		p.empty.Hide()
	}
}

// Compact rule cards share collection surfaces, icon discs and status tags.
// Fyne GridWrap retains scrolling, focus and recycling; actions bind to the
// current source indexes and baseline rather than a card's pooled position.
type ruleCard struct {
	widget.BaseWidget
	kind, title, badge *widget.Label
	icon               *widget.Icon
	tag                *statusBadge
	content            *fyne.Container
	onMenu             func(fyne.Position)
	minWidth           float32
}

func newRuleCard() *ruleCard {
	c := &ruleCard{minWidth: 224, kind: quietLabel(""), title: dashboardValue("", theme.SizeNameText), badge: widget.NewLabel(""), icon: widget.NewIcon(theme.NewColoredResource(iconFilter, colorAccent))}
	c.kind.SizeName = sizeMemberDetail
	for _, label := range []*widget.Label{c.kind, c.title} {
		label.Wrapping = fyne.TextWrapOff
		label.Truncation = fyne.TextTruncateEllipsis
	}
	c.tag = newStatusBadge(c.badge)
	c.content = container.New(ruleCardLayout{c}, themedSurface(colorMemberCard, 14), collectionIcon(c.icon, 28), homeLabel(c.kind), homeLabel(c.title), c.tag)
	c.ExtendBaseWidget(c)
	return c
}
func (c *ruleCard) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(c.content) }
func (c *ruleCard) Tapped(e *fyne.PointEvent)           { c.TappedSecondary(e) }
func (c *ruleCard) TappedSecondary(e *fyne.PointEvent) {
	if c.onMenu != nil {
		c.onMenu(e.AbsolutePosition)
	}
}

type ruleCardLayout struct{ card *ruleCard }

func (l ruleCardLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	text := l.card.kind.MinSize().Height + l.card.title.MinSize().Height + 4
	return fyne.NewSize(l.card.minWidth, fyne.Max(60, text+20))
}
func (l ruleCardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Resize(size)
	objects[1].Move(fyne.NewPos(12, (size.Height-28)/2))
	objects[1].Resize(fyne.NewSize(28, 28))
	width := fyne.Max(0, size.Width-50-12)
	tagSize := l.card.tag.MinSize()
	tagSize.Width = fyne.Min(tagSize.Width, width*.48)
	tagSize.Height = fyne.Min(tagSize.Height, size.Height/2-4)
	objects[4].Move(fyne.NewPos(size.Width-12-tagSize.Width, 10))
	objects[4].Resize(tagSize)
	kindH, titleH := objects[2].MinSize().Height, objects[3].MinSize().Height
	y := (size.Height - kindH - titleH - 4) / 2
	objects[2].Move(fyne.NewPos(50, y))
	objects[2].Resize(fyne.NewSize(fyne.Max(0, width-tagSize.Width-6), kindH))
	objects[3].Move(fyne.NewPos(50, y+kindH+4))
	objects[3].Resize(fyne.NewSize(width, titleH))
}

// Exactly two columns at desktop widths and one in a narrow window. Measure
// the grid's own spacing so it agrees with GridWrap's column calculation.
type ruleGridLayout struct{ pane *rulesPane }

func (ruleGridLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(224, 60) }
func (l ruleGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	gap := l.pane.list.Theme().Size(theme.SizeNamePadding)
	width := size.Width
	if width >= 448+gap {
		width = (width - gap) / 2
	}
	width = fyne.Max(224, width)
	if l.pane.cardWidth != width {
		l.pane.cardWidth = width
		l.pane.list.Refresh()
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

func compactRuleTarget(target string) string {
	switch target {
	case "direct":
		return "DIRECT"
	case "reject":
		return "REJECT"
	case "":
		return "—"
	}
	return target
}
func (p *rulesPane) updateRuleCard(id int, c *ruleCard) {
	if id < 0 || id >= len(p.filtered) {
		return
	}
	index := p.filtered[id]
	target := ""
	if p.section >= 2 {
		if index < 0 || index >= len(p.routing.RuleSets) {
			return
		}
		set := p.routing.RuleSets[index]
		tags := profile.RuleSetTags(set["tag"])
		c.kind.SetText("SING-BOX · " + subscriptionFormatLabel(set))
		c.title.SetText(strings.Join(tags, " · "))
		target = p.subscriptionTarget(index)
		c.icon.SetResource(theme.NewColoredResource(iconCloud, colorAccent))
	} else {
		if index < 0 || index >= len(p.routing.Rules) {
			return
		}
		rule := p.routing.Rules[index]
		target = fieldText(rule["outbound"])
		if rule["action"] == "reject" {
			target = "reject"
		}
		if basic, ok := profile.ReadBasicRule(rule); ok {
			c.kind.SetText(strings.ToUpper(strings.ReplaceAll(basic.Kind, "_", "-")))
			title := "私有 IP"
			if len(basic.Values) > 0 {
				title = basic.Values[0]
				if len(basic.Values) > 1 {
					title += fmt.Sprintf(" · +%d", len(basic.Values)-1)
				}
			}
			c.title.SetText(title)
		} else {
			name, _ := p.ruleDescription(rule)
			c.kind.SetText("ADVANCED")
			c.title.SetText(name)
			if rule["action"] == "resolve" {
				target = "DNS"
				c.kind.SetText("RESOLVE")
			}
		}
		c.icon.SetResource(theme.NewColoredResource(iconFilter, colorAccent))
	}
	c.tag.setTarget(compactRuleTarget(p.resolvedTarget(target)), p.resolvedTarget(target))
	baseline := append([]byte(nil), p.baseline...)
	section := p.section
	c.onMenu = func(pos fyne.Position) {
		if section != p.section || !bytes.Equal(baseline, p.baseline) {
			return
		}
		p.selected = index
		p.showRuleMenu(index, section, baseline, pos)
	}
	c.icon.Refresh()
	// Pooled cards can reappear after an appearance change while offscreen.
	c.content.Refresh()
	c.Refresh()
}
func (p *rulesPane) hideRuleMenu() {
	if p.contextMenu != nil {
		p.contextMenu.Hide()
		p.contextMenu = nil
	}
}
func (p *rulesPane) showRuleMenu(index, section int, baseline []byte, position fyne.Position) {
	p.hideRuleMenu()
	action := func(label string, icon fyne.Resource, enabled bool, run func()) *fyne.MenuItem {
		item := fyne.NewMenuItem(label, func() {
			if p.writable() && p.section == section && bytes.Equal(p.baseline, baseline) {
				p.selected = index
				run()
			}
		})
		item.Icon = icon
		item.Disabled = !p.writable() || !enabled
		return item
	}
	var items []*fyne.MenuItem
	if section >= 2 {
		single := len(profile.RuleSetTags(p.routing.RuleSets[index]["tag"])) == 1
		routeIndex := p.subscriptionRouteIndex(index)
		items = []*fyne.MenuItem{action("编辑订阅", theme.DocumentCreateIcon(), single, func() { p.editRuleSubscription(index) }), action("上移匹配顺序", theme.MoveUpIcon(), routeIndex > 0, func() { p.selected = routeIndex; p.move(-1) }), action("下移匹配顺序", theme.MoveDownIcon(), routeIndex >= 0 && routeIndex+1 < len(p.routing.Rules), func() { p.selected = routeIndex; p.move(1) }), fyne.NewMenuItemSeparator(), action("删除订阅", theme.DeleteIcon(), single, func() { p.deleteRuleSubscription(index) })}
	} else {
		_, basic := profile.ReadBasicRule(p.routing.Rules[index])
		items = []*fyne.MenuItem{action("编辑规则", theme.DocumentCreateIcon(), basic, func() { p.editRule(index) }), action("上移", theme.MoveUpIcon(), index > 0, func() { p.move(-1) }), action("下移", theme.MoveDownIcon(), index+1 < len(p.routing.Rules), func() { p.move(1) }), fyne.NewMenuItemSeparator(), action("删除规则", theme.DeleteIcon(), true, p.deleteRule)}
	}
	details := fyne.NewMenuItem("查看详情", func() {
		if p.section != section || !bytes.Equal(p.baseline, baseline) {
			return
		}
		if section >= 2 {
			set := p.routing.RuleSets[index]
			data, _ := json.MarshalIndent(set, "", "  ")
			dialog.ShowInformation("规则订阅详情", string(data), p.window)
		} else {
			name, value := p.ruleDescription(p.routing.Rules[index])
			dialog.ShowInformation("规则详情", fmt.Sprintf("第 %d 条匹配\n%s\n%s", index+1, name, value), p.window)
		}
	})
	details.Icon = iconConfig
	items = append([]*fyne.MenuItem{details, fyne.NewMenuItemSeparator()}, items...)
	p.contextMenu = widget.NewPopUpMenu(fyne.NewMenu("规则", items...), p.window.Canvas())
	p.contextMenu.ShowAtPosition(position)
}
