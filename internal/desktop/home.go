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
	connection := panel(container.NewVBox(
		container.NewBorder(nil, nil, quietLabel("代理连接"),
			container.NewGridWrap(fyne.NewSize(42, 42), iconTile(theme.MediaPlayIcon(), colorAccent)), nil),
		status,
		quietLabel("启动后按当前分流模式处理流量。"),
		inset(12, 0, 0, 0, container.NewHBox(container.NewGridWrap(fyne.NewSize(148, 42), toggle))),
	))
	manage := widget.NewButtonWithIcon("选择策略", theme.NavigateNextIcon(), func() { s.selectPage(pagePolicy) })
	manage.IconPlacement = widget.ButtonIconTrailingText
	manage.Importance = widget.LowImportance
	node := panel(container.NewVBox(
		container.NewBorder(nil, nil, quietLabel("默认策略"),
			container.NewGridWrap(fyne.NewSize(42, 42), iconTile(theme.ComputerIcon(), colorAccent)), nil),
		current,
		quietLabel("全局代理与默认分流使用 Available，应用策略可单独选择。"),
		inset(12, 0, 0, 0, container.NewHBox(manage)),
	))
	capture := sectionCard("流量接管", "选择流量进入代理的方式", container.NewVBox(
		s.tunSwitch,
		quietLabel("关闭时使用系统代理"),
		quietLabel("TUN 接管 TCP / UDP，启动时需要管理员授权。请停止连接后切换。"),
	))
	viewPolicy := widget.NewButtonWithIcon("查看策略", theme.NavigateNextIcon(), func() { s.selectPage(pagePolicy) })
	viewPolicy.IconPlacement = widget.ButtonIconTrailingText
	viewPolicy.Importance = widget.LowImportance
	policy := sectionCard("分流策略", "规则独立于节点和订阅", container.NewVBox(
		s.modeHelp,
		container.NewHBox(viewPolicy, layout.NewSpacer()),
		quietLabel("连接中切换分流模式会自动重连。"),
	))
	grid := container.NewVBox(
		container.NewGridWithColumns(2, connection, node),
		container.NewGridWithColumns(2, capture, policy),
		inset(8, 0, 4, 4, quietLabel("关闭窗口后继续连接；停止代理或退出 Loop 时恢复网络设置。")),
	)
	return container.NewBorder(inset(0, 12, 0, 0, heading), nil, nil, nil, container.NewVScroll(grid))
}
