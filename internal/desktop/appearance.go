package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func newAppearanceSelect(a fyne.App, th *clientTheme) *widget.Select {
	labels := []string{"跟随系统", "浅色模式", "深色模式"}
	modes := []string{appearanceSystem, appearanceLight, appearanceDark}
	selectMode := widget.NewSelect(labels, nil)
	selected := labels[0]
	for i, mode := range modes {
		if th.mode == mode {
			selected = labels[i]
		}
	}
	selectMode.SetSelected(selected)
	selectMode.OnChanged = func(value string) {
		for i, label := range labels {
			if value == label && th.mode != modes[i] {
				th.mode = modes[i]
				a.Preferences().SetString("appearance", th.mode)
				a.Settings().SetTheme(th)
				return
			}
		}
	}
	return selectMode
}
