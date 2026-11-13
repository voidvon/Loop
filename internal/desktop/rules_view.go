package desktop

import (
	"bytes"
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/profile"
)

func (p *rulesPane) buildView() {
	p.summary, p.status = quietLabel(""), quietLabel("")
	p.summary.Wrapping = fyne.TextWrapOff
	p.status.Hide()
	p.body = container.NewStack()
	p.final = widget.NewSelect(nil, func(string) { p.updateButtons() })
	p.saveFinal = widget.NewButtonWithIcon("保存", theme.DocumentSaveIcon(), func() {
		p.commit(profile.RuleChange{Operation: "final", Final: p.targetValue(p.final.Selected)}, append([]byte(nil), p.baseline...), nil)
	})
	p.saveFinal.Importance = widget.HighImportance
	p.finalPage = container.NewVScroll(container.NewVBox(
		inset(0, 16, 0, 0, container.NewBorder(nil, nil, iconTile(theme.LogoutIcon(), colorOrange), nil, container.NewVBox(sectionTitle("Final 兜底"), quietLabel("未命中其他规则时使用")))),
		widget.NewForm(widget.NewFormItem("处理策略", p.final)),
		container.NewHBox(layout.NewSpacer(), p.saveFinal),
	))
	p.sectionTitle, p.sectionDetail = sectionTitle("本地规则"), quietLabel("")
	p.empty = widget.NewLabelWithStyle("暂无规则\n点击右上角 + 添加", fyne.TextAlignCenter, fyne.TextStyle{})
	p.empty.Importance = widget.LowImportance
	p.cardWidth = 252
	p.list = widget.NewGridWrap(func() int { return len(p.filtered) }, func() fyne.CanvasObject { card := newRuleCard(); card.minWidth = p.cardWidth; return card }, func(id widget.GridWrapItemID, obj fyne.CanvasObject) { p.updateRuleCard(id, obj.(*ruleCard)) })
	p.list.OnSelected = func(id widget.GridWrapItemID) {
		if id >= 0 && id < len(p.filtered) {
			p.selected = p.filtered[id]
		}
	}
	p.list.OnUnselected = func(widget.GridWrapItemID) { p.selected = -1 }
	p.localPage = container.NewBorder(
		inset(0, 14, 0, 0, container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(36, 40), iconTile(theme.ListIcon(), colorAccent)), nil, container.NewVBox(p.sectionTitle, p.sectionDetail))),
		nil, nil, nil, container.NewStack(container.New(ruleGridLayout{p}, p.list), container.NewCenter(p.empty)))
	p.groups = widget.NewList(func() int { return 3 }, func() fyne.CanvasObject {
		name, detail := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), quietLabel("")
		name.Truncation, detail.Truncation = fyne.TextTruncateEllipsis, fyne.TextTruncateEllipsis
		return container.NewBorder(nil, nil, container.NewCenter(widget.NewIcon(theme.ListIcon())), nil, container.NewVBox(name, detail))
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		row := obj.(*fyne.Container)
		labels := row.Objects[0].(*fyne.Container)
		names := []string{"Final 兜底", "本地规则", "订阅规则"}
		details := []string{compactRuleTarget(p.routing.Final), fmt.Sprintf("%d 条规则", len(p.localIndexes())), fmt.Sprintf("%d 个订阅", len(p.remoteSets))}
		labels.Objects[0].(*widget.Label).SetText(names[id])
		labels.Objects[1].(*widget.Label).SetText(details[id])
		icons := []fyne.Resource{theme.LogoutIcon(), theme.DocumentIcon(), theme.FolderIcon()}
		row.Objects[1].(*fyne.Container).Objects[0].(*widget.Icon).SetResource(icons[id])
	})
	p.groups.HideSeparators = true
	p.groups.OnSelected = func(id widget.ListItemID) {
		p.hideRuleMenu()
		p.section = id
		page := p.localPage
		if id == 0 {
			page = p.finalPage
		}
		p.body.Objects = []fyne.CanvasObject{page}
		p.filter()
		p.body.Refresh()
	}
	p.add = widget.NewButtonWithIcon("", theme.ContentAddIcon(), p.addRuleDialog)
	p.reload = widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), p.Refresh)
	p.reload.Importance = widget.LowImportance
	title := widget.NewLabelWithStyle("所有规则", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	header := inset(0, 16, 0, 0, container.NewBorder(nil, nil, nil, container.NewHBox(p.reload, p.add), container.NewHBox(title, container.NewCenter(p.summary))))
	left := panel(p.groups)
	right := panel(inset(6, 4, 6, 6, p.body))
	columns := container.New(nodeColumnsLayout{leftWidth: 204}, left, right)
	p.root = container.NewBorder(header, p.status, nil, nil, columns)
}

func (p *rulesPane) refreshSection() {
	title, detail := "本地规则", fmt.Sprintf("%d 条规则", len(p.filtered))
	if p.section == 2 {
		title, detail = "订阅规则", fmt.Sprintf("%d 个 sing-box 规则集", len(p.filtered))
	}
	p.sectionTitle.SetText(title)
	p.sectionDetail.SetText(detail)
	if len(p.filtered) == 0 {
		p.empty.Show()
	} else {
		p.empty.Hide()
	}
}

// Only event binding and a stable card size are custom; Fyne Card, Button,
// Label, GridWrap and PopUpMenu own all rendering and interaction feedback.
type ruleCard struct {
	widget.BaseWidget
	kind, title, detail, badge *widget.Label
	icon                       *widget.Icon
	more                       *widget.Button
	badgeSurface               *surface
	content                    fyne.CanvasObject
	onMenu                     func(fyne.Position)
	minWidth                   float32
}

func newRuleCard() *ruleCard {
	c := &ruleCard{minWidth: 252, kind: quietLabel(""), title: widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), detail: quietLabel(""), badge: widget.NewLabel(""), icon: widget.NewIcon(theme.DocumentIcon())}
	for _, label := range []*widget.Label{c.kind, c.title, c.detail, c.badge} {
		label.Wrapping = fyne.TextWrapOff
		label.Truncation = fyne.TextTruncateEllipsis
	}
	c.kind.SizeName = theme.SizeNameCaptionText
	c.badge.SizeName = theme.SizeNameCaptionText
	c.badge.Alignment = fyne.TextAlignCenter
	c.badgeSurface = themedSurface(colorBadgeProxy, 6).(*surface)
	badge := container.NewThemeOverride(container.NewGridWrap(fyne.NewSize(88, 24), container.NewStack(c.badgeSurface, c.badge)), ruleBadgeTheme{fyne.CurrentApp().Settings().Theme()})
	c.more = widget.NewButtonWithIcon("", theme.MoreHorizontalIcon(), func() {
		if c.onMenu != nil {
			c.onMenu(fyne.CurrentApp().Driver().AbsolutePositionForObject(c.more).Add(fyne.NewPos(0, c.more.Size().Height)))
		}
	})
	c.more.Importance = widget.LowImportance
	top := container.NewBorder(nil, nil, container.NewCenter(c.icon), badge, c.kind)
	main := container.NewBorder(nil, nil, nil, c.more, c.title)
	c.content = container.NewThemeOverride(widget.NewCard("", "", container.NewVBox(top, main, c.detail)), ruleCardTheme{fyne.CurrentApp().Settings().Theme()})
	c.ExtendBaseWidget(c)
	return c
}
func (c *ruleCard) CreateRenderer() fyne.WidgetRenderer {
	return &ruleCardRenderer{WidgetRenderer: widget.NewSimpleRenderer(inset(4, 4, 4, 4, c.content)), card: c}
}
func (c *ruleCard) TappedSecondary(e *fyne.PointEvent) {
	if c.onMenu != nil {
		c.onMenu(e.AbsolutePosition)
	}
}

type ruleCardRenderer struct {
	fyne.WidgetRenderer
	card *ruleCard
}

func (r *ruleCardRenderer) MinSize() fyne.Size { return fyne.NewSize(r.card.minWidth, 134) }

// Set only the standard GridWrap's item width: two equal columns on desktop,
// one at narrow widths. The grid still owns scrolling, recycling and focus.
type ruleGridLayout struct{ pane *rulesPane }

func (l ruleGridLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(252, 134) }
func (l ruleGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	width := size.Width
	padding := theme.Padding()
	if width >= 508 {
		width = (width - padding) / 2
	}
	width = fyne.Max(252, width)
	if l.pane.cardWidth != width {
		l.pane.cardWidth = width
		l.pane.list.Refresh()
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

type ruleCardTheme struct{ fyne.Theme }

func (t ruleCardTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameBackground {
		return t.Theme.Color(colorRuleCard, variant)
	}
	return t.Theme.Color(name, variant)
}

type ruleBadgeTheme struct{ fyne.Theme }

func (t ruleBadgeTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameWarning {
		return t.Theme.Color(colorOrange, variant)
	}
	return t.Theme.Color(name, variant)
}

func compactRuleTarget(target string) string {
	switch target {
	case "direct":
		return "DIRECT"
	case "proxy":
		return profile.DefaultPolicy
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
	if p.section == 2 {
		if index < 0 || index >= len(p.routing.RuleSets) {
			return
		}
		set := p.routing.RuleSets[index]
		tags := profile.RuleSetTags(set["tag"])
		kind := "SING-BOX · " + subscriptionFormatLabel(set)
		if routeIndex := p.subscriptionRouteIndex(index); routeIndex >= 0 {
			kind = fmt.Sprintf("%02d · %s", routeIndex+1, kind)
		}
		c.kind.SetText(kind)
		c.title.SetText(strings.Join(tags, " · "))
		interval := fieldText(set["update_interval"])
		if interval == "" {
			interval = "1d"
		}
		c.detail.SetText(interval + " 更新 · " + fieldText(set["url"]))
		target = p.subscriptionTarget(index)
		c.icon.SetResource(theme.NewColoredResource(theme.FolderIcon(), colorAccent))
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
			c.kind.SetText(fmt.Sprintf("%02d · %s", index+1, strings.ToUpper(strings.ReplaceAll(basic.Kind, "_", "-"))))
			title := "私有 IP"
			detail := "局域网与非公网地址"
			if len(basic.Values) > 0 {
				title = basic.Values[0]
				detail = basicRuleLabel(basic.Kind)
				if len(basic.Values) > 1 {
					detail = fmt.Sprintf("另有 %d 个匹配值", len(basic.Values)-1)
				}
			}
			c.title.SetText(title)
			c.detail.SetText(detail)
		} else {
			name, value := ruleDescription(rule)
			c.kind.SetText(fmt.Sprintf("%02d · ADVANCED", index+1))
			c.title.SetText(name)
			c.detail.SetText(value)
			if rule["action"] == "resolve" {
				target = "DNS"
				c.detail.SetText("IP 匹配前解析域名")
				c.kind.SetText(fmt.Sprintf("%02d · RESOLVE", index+1))
			}
		}
		c.icon.SetResource(theme.NewColoredResource(theme.DocumentIcon(), colorAccent))
	}
	c.badge.SetText(compactRuleTarget(target))
	c.badgeSurface.name = colorBadgeProxy
	c.badge.Importance = widget.WarningImportance
	if target == "direct" {
		c.badgeSurface.name = colorBadgeDirect
		c.badge.Importance = widget.SuccessImportance
	} else if target == "reject" {
		c.badgeSurface.name = colorBadgeReject
		c.badge.Importance = widget.DangerImportance
	} else if target == "DNS" || target == "" {
		c.badgeSurface.name = colorBadgeMuted
		c.badge.Importance = widget.LowImportance
	}
	c.badge.Refresh()
	baseline := append([]byte(nil), p.baseline...)
	section := p.section
	c.onMenu = func(pos fyne.Position) {
		if section != p.section || !bytes.Equal(baseline, p.baseline) {
			return
		}
		p.selected = index
		p.showRuleMenu(index, section, baseline, pos)
	}
	c.more.Enable()
	c.icon.Refresh()
	// Pooled cards can reappear after an appearance change while offscreen.
	c.content.Refresh()
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
	if section == 2 {
		single := len(profile.RuleSetTags(p.routing.RuleSets[index]["tag"])) == 1
		routeIndex := p.subscriptionRouteIndex(index)
		items = []*fyne.MenuItem{action("编辑订阅", theme.DocumentCreateIcon(), single, func() { p.editRuleSubscription(index) }), action("上移匹配顺序", theme.MoveUpIcon(), routeIndex > 0, func() { p.selected = routeIndex; p.move(-1) }), action("下移匹配顺序", theme.MoveDownIcon(), routeIndex >= 0 && routeIndex+1 < len(p.routing.Rules), func() { p.selected = routeIndex; p.move(1) }), fyne.NewMenuItemSeparator(), action("删除订阅", theme.DeleteIcon(), single, func() { p.deleteRuleSubscription(index) })}
	} else {
		_, basic := profile.ReadBasicRule(p.routing.Rules[index])
		items = []*fyne.MenuItem{action("编辑规则", theme.DocumentCreateIcon(), basic, func() { p.editRule(index) }), action("上移", theme.MoveUpIcon(), index > 0, func() { p.move(-1) }), action("下移", theme.MoveDownIcon(), index+1 < len(p.routing.Rules), func() { p.move(1) }), fyne.NewMenuItemSeparator(), action("删除规则", theme.DeleteIcon(), true, p.deleteRule)}
	}
	p.contextMenu = widget.NewPopUpMenu(fyne.NewMenu("规则", items...), p.window.Canvas())
	p.contextMenu.ShowAtPosition(position)
}
