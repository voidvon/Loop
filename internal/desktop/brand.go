package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
)

func sidebarBrand(status *widget.Label) fyne.CanvasObject {
	icon := canvas.NewImageFromResource(appIcon)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(34, 34))
	name := dashboardValue(app.DisplayName, theme.SizeNameText)
	name.Wrapping = fyne.TextWrapOff
	status.SizeName = theme.SizeNameCaptionText
	status.Wrapping = fyne.TextWrapOff
	text := newTitleDetail(name, status)
	return container.New(layout.NewCustomPaddedHBoxLayout(10), container.NewCenter(icon), container.NewCenter(text))
}
