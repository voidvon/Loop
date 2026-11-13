package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func sidebarBrand(status *widget.Label) fyne.CanvasObject {
	icon := canvas.NewImageFromResource(theme.NewColoredResource(theme.ComputerIcon(), colorAccent))
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(36, 36))
	name := sectionTitle("MiniProxy")
	name.Wrapping = fyne.TextWrapOff
	status.SizeName = theme.SizeNameText
	status.Wrapping = fyne.TextWrapOff
	text := container.NewThemeOverride(
		container.New(layout.NewCustomPaddedVBoxLayout(2), name, status),
		brandTheme{fyne.CurrentApp().Settings().Theme()},
	)
	return container.New(layout.NewCustomPaddedHBoxLayout(12), container.NewCenter(icon), container.NewCenter(text))
}

// Remove the labels' internal padding only in the brand block, so the two
// lines share a left edge and remain close without changing other widgets.
type brandTheme struct{ fyne.Theme }

func (t brandTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding {
		return 0
	}
	return t.Theme.Size(name)
}
