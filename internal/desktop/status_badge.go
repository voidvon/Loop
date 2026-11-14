package desktop

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Compact, content-sized tags shared by collection rows and rule cards.
type statusBadge struct {
	widget.BaseWidget
	label      *widget.Label
	background *surface
	content    fyne.CanvasObject
}

func newStatusBadge(label *widget.Label) *statusBadge {
	label.SizeName = sizeMemberDetail
	label.Wrapping = fyne.TextWrapOff
	label.Truncation = fyne.TextTruncateEllipsis
	b := &statusBadge{label: label, background: themedSurface(colorBadgeMuted, 4).(*surface)}
	b.content = container.NewThemeOverride(container.NewStack(b.background, inset(2, 2, 8, 8, label)), statusBadgeTheme{fyne.CurrentApp().Settings().Theme()})
	b.ExtendBaseWidget(b)
	return b
}

func (b *statusBadge) CreateRenderer() fyne.WidgetRenderer {
	return &statusBadgeRenderer{WidgetRenderer: widget.NewSimpleRenderer(b.content), badge: b}
}

type statusBadgeRenderer struct {
	fyne.WidgetRenderer
	badge *statusBadge
}

func (r *statusBadgeRenderer) MinSize() fyne.Size {
	label := r.badge.label
	size := fyne.MeasureText(label.Text, label.Theme().Size(sizeMemberDetail), label.TextStyle)
	return fyne.NewSize(fyne.Min(180, float32(math.Ceil(float64(size.Width)))+17), float32(math.Ceil(float64(size.Height)))+4)
}

func (b *statusBadge) setTarget(text, target string) {
	b.background.name = colorBadgeProxy
	b.label.Importance = widget.WarningImportance
	switch target {
	case "direct":
		b.background.name = colorBadgeDirect
		b.label.Importance = widget.SuccessImportance
	case "reject":
		b.background.name = colorBadgeReject
		b.label.Importance = widget.DangerImportance
	case "", "DNS":
		b.background.name = colorBadgeMuted
		b.label.Importance = widget.LowImportance
	}
	b.label.SetText(text)
	b.content.Refresh()
	b.Refresh()
}

type statusBadgeTheme struct{ fyne.Theme }

func (t statusBadgeTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding {
		return 0
	}
	return t.Theme.Size(name)
}

func (t statusBadgeTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameWarning {
		return t.Theme.Color(colorBadgeProxyText, variant)
	}
	if name == theme.ColorNameSuccess {
		return t.Theme.Color(colorBadgeDirectText, variant)
	}
	return t.Theme.Color(name, variant)
}
