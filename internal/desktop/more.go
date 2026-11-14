package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
)

const (
	prefTrayModeLetter = "menu-bar.mode-letter"
	prefTrayDim        = "menu-bar.dim-disconnected"
	prefTrayPolicies   = "menu-bar.policies"
)

const (
	moreStartup = iota
	moreMenuBar
	moreRouting
	moreBenchmark
	morePorts
)

// Menu-bar preferences affect presentation only and stay outside proxy profiles.
type menuBarPreferences = app.TrayPreferences

func defaultMenuBarPreferences() menuBarPreferences {
	return menuBarPreferences{ShowModeLetter: true, DimDisconnected: true, ShowPolicies: true}
}

func loadMenuBarPreferences(prefs fyne.Preferences) menuBarPreferences {
	return menuBarPreferences{
		ShowModeLetter:  prefs.BoolWithFallback(prefTrayModeLetter, true),
		DimDisconnected: prefs.BoolWithFallback(prefTrayDim, true),
		ShowPolicies:    prefs.BoolWithFallback(prefTrayPolicies, true),
	}
}

func storeMenuBarPreferences(prefs fyne.Preferences, value menuBarPreferences) {
	prefs.SetBool(prefTrayModeLetter, value.ShowModeLetter)
	prefs.SetBool(prefTrayDim, value.DimDisconnected)
	prefs.SetBool(prefTrayPolicies, value.ShowPolicies)
}

func (p *morePane) SetPreferences(value menuBarPreferences) {
	storeMenuBarPreferences(p.preferences, value)
	for _, setting := range []struct {
		check *widget.Check
		value bool
	}{
		{p.modeLetter, value.ShowModeLetter}, {p.dimDisconnected, value.DimDisconnected}, {p.policies, value.ShowPolicies},
	} {
		callback := setting.check.OnChanged
		setting.check.OnChanged = nil
		setting.check.SetChecked(setting.value)
		setting.check.OnChanged = callback
	}
}

type morePane struct {
	root            fyne.CanvasObject
	preferences     fyne.Preferences
	categories      *widget.List
	body            *fyne.Container
	menuBarPage     fyne.CanvasObject
	startup         *startupPane
	routing         *routingPane
	benchmark       *benchmarkPane
	ports           *portsPane
	selected        int
	modeLetter      *widget.Check
	dimDisconnected *widget.Check
	policies        *widget.Check
	OnChanged       func()
}

func newMorePane(prefs fyne.Preferences) *morePane {
	p := &morePane{preferences: prefs}
	settings := loadMenuBarPreferences(prefs)
	p.modeLetter = p.preferenceCheck("显示模式字母", prefTrayModeLetter, settings.ShowModeLetter)
	p.dimDisconnected = p.preferenceCheck("未连接时使用半透明图标", prefTrayDim, settings.DimDisconnected)
	p.policies = p.preferenceCheck("显示策略列表", prefTrayPolicies, settings.ShowPolicies)

	p.startup = newStartupPane()
	p.routing = newRoutingPane()
	p.benchmark = newBenchmarkPane()
	p.categories = widget.NewList(func() int { return 5 }, func() fyne.CanvasObject {
		return newListRow(theme.ComputerIcon())
	}, func(id widget.ListItemID, object fyne.CanvasObject) {
		row := object.(*infoRow)
		name, detail := "菜单栏", "图标与快捷操作"
		icon := theme.NewColoredResource(theme.ComputerIcon(), colorOrange)
		if id == moreStartup {
			name, detail = "开机自启", "系统设置"
			icon = theme.NewColoredResource(theme.LoginIcon(), theme.ColorNameSuccess)
		} else if id == moreRouting {
			name, detail = "分流测试", "网址、节点与延迟"
			icon = theme.NewColoredResource(latencyIcon, theme.ColorNamePrimary)
		} else if id == moreBenchmark {
			name, detail = "批量测速", "并发请求与节点对比"
			icon = theme.NewColoredResource(iconSpeed, theme.ColorNamePrimary)
		}
		if id == morePorts {
			name, detail = "代理端口", "默认入口与全局代理"
			icon = theme.NewColoredResource(iconLink, theme.ColorNamePrimary)
		}
		row.setText(name, detail)
		row.setIcon(icon)
	})
	p.categories.HideSeparators = true
	p.categories.OnSelected = p.selectCategory
	left := panel(p.categories)
	iconSettings := panel(inset(8, 8, 8, 8, container.NewVBox(
		sectionTitle("图标"),
		menuBarSetting(p.modeLetter, "G 全局代理 · R 自动分流 · D 全局直连"),
		widget.NewSeparator(),
		menuBarSetting(p.dimDisconnected, "未连接时透明度为 50%，连接后恢复为 100%。"),
	)))
	menuSettings := panel(inset(8, 8, 8, 8, container.NewVBox(
		sectionTitle("快捷菜单"),
		menuBarSetting(p.policies, "展开策略组，快速切换节点。"),
	)))
	p.menuBarPage = container.NewVScroll(container.NewVBox(
		sectionTitle("菜单栏"), iconSettings, menuSettings,
	))
	p.body = container.NewStack(p.menuBarPage)
	columns := container.New(nodeColumnsLayout{leftWidth: 204}, left, p.body)
	p.root = container.NewBorder(inset(0, 16, 0, 0, pageHeading("更多")), nil, nil, nil, columns)
	p.categories.Select(moreMenuBar)
	return p
}

func (p *morePane) selectCategory(index int) {
	if index < moreStartup || index > morePorts {
		return
	}
	p.selected = index
	page := p.menuBarPage
	if index == moreStartup {
		page = p.startup.root
	} else if index == moreRouting {
		page = p.routing.root
	} else if index == moreBenchmark {
		page = p.benchmark.root
	}
	if index == morePorts && p.ports != nil {
		page = p.ports.root
	}
	p.body.Objects = []fyne.CanvasObject{page}
	p.body.Refresh()
	p.Refresh()
}

func (p *morePane) Refresh() {
	if p.selected == moreStartup {
		p.startup.Refresh()
	} else if p.selected == moreBenchmark {
		p.benchmark.Refresh()
	} else if p.selected == morePorts && p.ports != nil {
		p.ports.Refresh()
	}
}

func menuBarSetting(check *widget.Check, description string) fyne.CanvasObject {
	title := check.Text
	check.SetText("启用")
	return newSectionHeader(title, description, nil, check)
}

func (p *morePane) preferenceCheck(label, key string, initial bool) *widget.Check {
	check := widget.NewCheck(label, nil)
	check.SetChecked(initial)
	check.OnChanged = func(value bool) {
		p.preferences.SetBool(key, value)
		if p.OnChanged != nil {
			p.OnChanged()
		}
	}
	return check
}
