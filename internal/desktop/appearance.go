package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

func setAppearanceMode(a fyne.App, th *clientTheme, mode string) {
	if th.mode == mode {
		return
	}
	th.mode = mode
	a.Preferences().SetString("appearance", mode)
	a.Settings().SetTheme(th)
}

func newAppearanceSwitches(a fyne.App) (*toggleSwitch, *toggleSwitch) {
	th, ok := a.Settings().Theme().(*clientTheme)
	updating := false
	dark := newToggleSwitch("深色模式", nil)
	system := newToggleSwitch("跟随系统", nil)
	if !ok {
		dark.Disable()
		system.Disable()
		return dark, system
	}
	sync := func() {
		updating = true
		dark.SetChecked(th.variant(a.Settings().ThemeVariant()) == theme.VariantDark)
		system.SetChecked(th.mode == appearanceSystem)
		updating = false
	}
	dark.OnChanged = func(value bool) {
		if updating {
			return
		}
		mode := appearanceLight
		if value {
			mode = appearanceDark
		}
		setAppearanceMode(a, th, mode)
	}
	system.OnChanged = func(value bool) {
		if updating {
			return
		}
		mode := appearanceSystem
		if !value {
			mode = appearanceLight
			if th.variant(a.Settings().ThemeVariant()) == theme.VariantDark {
				mode = appearanceDark
			}
		}
		setAppearanceMode(a, th, mode)
	}
	a.Settings().AddListener(func(fyne.Settings) { sync() })
	sync()
	return dark, system
}
