package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (s *desktopShell) homePage(status *widget.Label, toggle *widget.Button, current *widget.Label) fyne.CanvasObject {
	toggle.SetIcon(theme.MediaPlayIcon())
	toggle.Importance = widget.HighImportance
	status.TextStyle.Bold = true
	status.SizeName = theme.SizeNameSubHeadingText
	status.Wrapping = fyne.TextWrapWord
	current.TextStyle.Bold = true
	current.SizeName = theme.SizeNameSubHeadingText

	heading := container.NewBorder(nil, nil, nil,
		container.NewCenter(s.modeSelect), pageHeading("首页"))
	connection := homeCard("代理连接", theme.MediaPlayIcon(), container.New(layout.NewCustomPaddedVBoxLayout(8),
		homeLabel(status),
		homeLabel(quietLabel("启动后按当前分流模式处理流量。")),
	), container.NewHBox(container.NewGridWrap(fyne.NewSize(148, 42), toggle)))
	manage := widget.NewButtonWithIcon("选择策略", theme.NavigateNextIcon(), func() { s.selectPage(pagePolicy) })
	manage.IconPlacement = widget.ButtonIconTrailingText
	manage.Importance = widget.LowImportance
	node := homeCard("默认策略", theme.ComputerIcon(), container.New(layout.NewCustomPaddedVBoxLayout(8),
		homeLabel(current),
		homeLabel(quietLabel("全局代理使用此默认策略，分流规则可分别指定策略。")),
	), container.NewHBox(container.NewGridWrap(fyne.NewSize(148, 42), manage)))
	capture := homeCard("流量接管", theme.SettingsIcon(), container.New(layout.NewCustomPaddedVBoxLayout(8),
		homeLabel(quietLabel("选择流量进入代理的方式")),
		s.tunSwitch,
		homeLabel(quietLabel("开启后接管 TCP / UDP；关闭时使用系统代理。")),
	), homeLabel(quietLabel("首次安装服务需授权，之后重开应用和修改配置无需重复授权。")))
	viewPolicy := widget.NewButtonWithIcon("查看策略", theme.NavigateNextIcon(), func() { s.selectPage(pagePolicy) })
	viewPolicy.IconPlacement = widget.ButtonIconTrailingText
	viewPolicy.Importance = widget.LowImportance
	policy := homeCard("分流策略", theme.ListIcon(), container.New(layout.NewCustomPaddedVBoxLayout(8),
		homeLabel(quietLabel("规则独立于节点和订阅")),
		homeLabel(s.modeHelp),
		container.NewHBox(viewPolicy, layout.NewSpacer()),
	), homeLabel(quietLabel("连接中切换分流模式会自动重连。")))
	grid := container.New(layout.NewCustomPaddedVBoxLayout(16),
		homeCardRow(connection, node),
		homeCardRow(capture, policy),
		inset(0, 0, 4, 4, quietLabel("关闭窗口后继续连接；停止代理或退出 Loop 时恢复网络设置。")),
	)
	return container.NewBorder(inset(0, 12, 0, 0, heading), nil, nil, nil, container.NewVScroll(grid))
}

// Keep headings, content and actions inside the same padded area. The footer
// stays at the bottom when the other card in the row needs more height.
func homeCard(title string, icon fyne.Resource, body, footer fyne.CanvasObject) fyne.CanvasObject {
	heading := container.NewBorder(nil, nil, nil,
		container.NewGridWrap(fyne.NewSize(24, 24), iconTile(icon, colorAccent)), homeLabel(sectionTitle(title)))
	content := container.NewBorder(inset(0, 12, 0, 0, heading), inset(16, 0, 0, 0, footer), nil, nil, body)
	return panel(inset(16, 16, 16, 16, content))
}

func homeLabel(label *widget.Label) fyne.CanvasObject {
	return container.NewThemeOverride(label, titleDetailTheme{fyne.CurrentApp().Settings().Theme()})
}

func homeCardRow(left, right fyne.CanvasObject) fyne.CanvasObject {
	// The standard grid supplies the remaining gap between the two columns.
	padding := fyne.Max(0, (16-theme.Padding())/2)
	return container.NewGridWithColumns(2, inset(0, 0, 0, padding, left), inset(0, 0, padding, 0, right))
}
