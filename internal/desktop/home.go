package desktop

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (s *desktopShell) homePage(status *widget.Label, toggle *widget.Button, current *widget.Label) fyne.CanvasObject {
	s.homeState = dashboardValue("未连接", sizeDashboardValue)
	s.homeCapture = dashboardValue("系统代理", theme.SizeNameText)
	s.homeMode = dashboardValue("自动分流", theme.SizeNameText)
	s.homeInventory = dashboardValue("—", sizeDashboardValue)
	s.homeSources = quietLabel("正在读取配置")
	s.homePorts = quietLabel("")
	s.homePorts.Wrapping = fyne.TextWrapWord
	current.SizeName = sizeDashboardPolicy
	current.TextStyle.Bold = true
	current.Wrapping = fyne.TextWrapOff
	current.Truncation = fyne.TextTruncateEllipsis
	status.SizeName = theme.SizeNameSubHeadingText
	status.TextStyle.Bold = true
	status.Wrapping = fyne.TextWrapWord
	toggle.Importance = widget.HighImportance
	toggle.SetIcon(iconPower)

	globe := dashboardIconButton(iconGlobe, func() { s.openHomeTool(moreRouting) })
	link := dashboardIconButton(iconLink, func() { s.selectPage(pageNodes) })
	headingLabel := widget.NewLabelWithStyle("首页", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	headingLabel.SizeName = theme.SizeNameHeadingText
	title := container.New(layout.NewCustomPaddedHBoxLayout(12), container.NewCenter(homeLabel(headingLabel)), container.NewCenter(homeLabel(quietLabel("Loop"))))
	toolbar := container.New(layout.NewCustomPaddedHBoxLayout(10), s.modeSelect, globe, link)
	heading := inset(6, 8, 0, 0, container.NewBorder(nil, nil, title, toolbar))

	connection := dashboardCard("代理连接", container.NewBorder(nil, nil, nil,
		container.NewCenter(widget.NewIcon(theme.NewColoredResource(iconPower, colorSecondary))), homeLabel(s.homeState)),
		homeLabel(s.homePorts))
	policy := dashboardCard("默认策略", homeLabel(current), dashboardTextButton("管理策略", func() { s.selectPage(pagePolicy) }))
	inventory := dashboardCard("配置概况", homeLabel(s.homeInventory), homeLabel(s.homeSources))

	connectionBody := container.New(layout.NewCustomPaddedVBoxLayout(14),
		homeLabel(status),
		homeLabel(quietLabel("默认入口与 TUN 跟随首页模式；全局入口独立代理。")),
		container.NewGridWithColumns(2,
			dashboardKeyValue("分流模式", s.homeMode), dashboardKeyValue("接管方式", s.homeCapture)),
	)
	actions := container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(112, 34), toggle),
		dashboardTextButton("分流测试", func() { s.openHomeTool(moreRouting) }))
	session := dashboardCard("连接状态", connectionBody, actions)

	dark, system := newAppearanceSwitches(fyne.CurrentApp())
	shortcuts := dashboardCard("快捷开关", container.New(layout.NewCustomPaddedVBoxLayout(8),
		dashboardSwitchRow(iconShield, colorAccent, "TUN 模式", "接管 TCP / UDP", s.tunSwitch),
		dashboardSwitchRow(iconMoon, colorOrange, "深色模式", "切换界面外观", dark),
		dashboardSwitchRow(iconAppearance, theme.ColorNameSuccess, "跟随系统", "自动匹配系统外观", system),
	), nil)

	s.modeHelp.Wrapping = fyne.TextWrapWord
	overview := dashboardCard("分流策略", container.New(layout.NewCustomPaddedVBoxLayout(18),
		dashboardInfoRow(iconRules, colorAccent, "规则独立管理", "分流规则独立于节点和订阅"),
		homeLabel(s.modeHelp),
	), dashboardTextButton("查看策略", func() { s.selectPage(pagePolicy) }))
	tools := dashboardCard("常用工具", container.New(layout.NewCustomPaddedVBoxLayout(4),
		dashboardToolRow("分流测试", iconGlobe, func() { s.openHomeTool(moreRouting) }),
		dashboardToolRow("批量测速", iconSpeed, func() { s.openHomeTool(moreBenchmark) }),
		dashboardToolRow("节点管理", iconNodes, func() { s.selectPage(pageNodes) }),
		dashboardToolRow("代理端口", iconLink, func() { s.openHomeTool(morePorts) }),
	), nil)
	grid := container.New(homeDashboardLayout{}, connection, policy, inventory, session, shortcuts, overview, tools)
	return container.NewBorder(heading, nil, nil, nil, container.NewVScroll(grid))
}

func (s *desktopShell) openHomeTool(category int) {
	s.selectPage(pageMore)
	s.more.categories.Select(category)
}

func (s *desktopShell) refreshHomeSummary() {
	if s.homeState == nil {
		return
	}
	state := "未连接"
	if s.running() {
		state = "已连接"
	}
	s.homeState.SetText(state)
	s.homeCapture.SetText(s.captureLabel.Text)
	mode := s.modeSelect.Selected
	if mode == "" {
		mode = "完整配置"
	}
	s.homeMode.SetText(mode)
	if s.rawConfig {
		s.homePorts.SetText("端口由完整配置管理")
		s.homeInventory.SetText("完整配置")
		s.homeSources.SetText("节点与规则由配置文件管理")
		return
	}
	if ports, err := s.store.ProxyPorts(); err == nil {
		global := "已关闭"
		if ports.GlobalEnabled {
			global = fmt.Sprintf("127.0.0.1:%d", ports.Global)
		}
		s.homePorts.SetText(fmt.Sprintf("默认 127.0.0.1:%d\n全局 %s", ports.Default, global))
	} else {
		s.homePorts.SetText("代理端口配置无效")
	}
	sources, err := s.store.Sources()
	if err != nil {
		s.homeInventory.SetText("—")
		s.homeSources.SetText("无法读取节点配置")
		return
	}
	total := 0
	for _, source := range sources {
		total += len(source.Nodes)
	}
	s.homeInventory.SetText(fmt.Sprintf("%d 个节点", total))
	s.homeSources.SetText(fmt.Sprintf("%d 个订阅 · 本地配置", max(0, len(sources)-1)))
}

func dashboardValue(text string, size fyne.ThemeSizeName) *widget.Label {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.SizeName = size
	l.Truncation = fyne.TextTruncateEllipsis
	return l
}

func homeLabel(label *widget.Label) fyne.CanvasObject {
	return container.NewThemeOverride(label, titleDetailTheme{fyne.CurrentApp().Settings().Theme()})
}

func dashboardCard(title string, body, footer fyne.CanvasObject) fyne.CanvasObject {
	heading := widget.NewLabel(title)
	heading.Importance = widget.LowImportance
	var bottom fyne.CanvasObject
	if footer != nil {
		bottom = inset(10, 0, 0, 0, footer)
	}
	return container.NewStack(themedSurface(colorPanel, 20), inset(16, 16, 18, 18,
		container.NewBorder(inset(0, 14, 0, 0, homeLabel(heading)), bottom, nil, nil, body)))
}

func dashboardKeyValue(key string, value *widget.Label) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedVBoxLayout(5), homeLabel(quietLabel(key)), homeLabel(value))
}

func dashboardTextButton(text string, tapped func()) fyne.CanvasObject {
	b := widget.NewButtonWithIcon(text, iconChevron, tapped)
	b.IconPlacement = widget.ButtonIconTrailingText
	b.Importance = widget.LowImportance
	return container.NewThemeOverride(b, compactActionTheme{fyne.CurrentApp().Settings().Theme()})
}

func dashboardIconButton(icon fyne.Resource, tapped func()) fyne.CanvasObject {
	b := widget.NewButtonWithIcon("", theme.NewColoredResource(icon, colorSecondary), tapped)
	return container.NewGridWrap(fyne.NewSize(36, 36), container.NewThemeOverride(b, circleActionTheme{fyne.CurrentApp().Settings().Theme()}))
}

func dashboardInfoRow(icon fyne.Resource, tint fyne.ThemeColorName, title, detail string) fyne.CanvasObject {
	text := newTitleDetail(dashboardValue(title, theme.SizeNameText), quietLabel(detail))
	return container.NewBorder(nil, nil, inset(0, 0, 0, 12, container.NewCenter(widget.NewIcon(theme.NewColoredResource(icon, tint)))), nil, text)
}

func dashboardSwitchRow(icon fyne.Resource, tint fyne.ThemeColorName, title, detail string, control fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, nil, container.NewCenter(control), dashboardInfoRow(icon, tint, title, detail))
}

func dashboardToolRow(title string, icon fyne.Resource, tapped func()) fyne.CanvasObject {
	b := widget.NewButtonWithIcon(title, theme.NewColoredResource(icon, colorAccent), tapped)
	b.Alignment = widget.ButtonAlignLeading
	b.Importance = widget.LowImportance
	return container.NewBorder(nil, nil, nil, nil, container.NewThemeOverride(b, compactActionTheme{fyne.CurrentApp().Settings().Theme()}))
}

// Match the reference dashboard's three summaries, wide primary panel and two
// lower cards. A minimum height allows shorter windows to scroll naturally.
type homeDashboardLayout struct{}

func (homeDashboardLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(640, 610) }
func (homeDashboardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	gap := float32(12)
	col := (size.Width - 2*gap) / 3
	top, middle := float32(150), float32(218)
	bottom := fyne.Max(218, size.Height-top-middle-2*gap)
	place := func(i int, x, y, w, h float32) {
		objects[i].Move(fyne.NewPos(x, y))
		objects[i].Resize(fyne.NewSize(w, h))
	}
	for i := 0; i < 3; i++ {
		place(i, float32(i)*(col+gap), 0, col, top)
	}
	place(3, 0, top+gap, 2*col+gap, middle)
	place(4, 2*(col+gap), top+gap, col, middle)
	half := (size.Width - gap) / 2
	place(5, 0, top+middle+2*gap, half, bottom)
	place(6, half+gap, top+middle+2*gap, half, bottom)
}
