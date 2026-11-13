package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func inset(top, bottom, left, right float32, object fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedLayout(top, bottom, left, right), object)
}

func rounded(fill color.Color, radius float32) *canvas.Rectangle {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius
	return bg
}

// A widget receives Fyne theme refreshes; a plain canvas rectangle would retain
// its original color after a system or application appearance change.
type surface struct {
	widget.BaseWidget
	name   fyne.ThemeColorName
	radius float32
}

func themedSurface(name fyne.ThemeColorName, radius float32) fyne.CanvasObject {
	s := &surface{name: name, radius: radius}
	s.ExtendBaseWidget(s)
	return s
}

func (s *surface) CreateRenderer() fyne.WidgetRenderer {
	r := &surfaceRenderer{surface: s, background: rounded(color.Transparent, s.radius)}
	r.Refresh()
	return r
}

type surfaceRenderer struct {
	surface    *surface
	background *canvas.Rectangle
}

func (r *surfaceRenderer) MinSize() fyne.Size    { return fyne.NewSize(0, 0) }
func (r *surfaceRenderer) Layout(size fyne.Size) { r.background.Resize(size) }
func (r *surfaceRenderer) Refresh() {
	r.background.FillColor = r.surface.Theme().Color(r.surface.name, fyne.CurrentApp().Settings().ThemeVariant())
	r.background.Refresh()
}
func (r *surfaceRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.background} }
func (r *surfaceRenderer) Destroy()                     {}

func panel(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(widget.NewCard("", "", content), cardTheme{fyne.CurrentApp().Settings().Theme()})
}

func quietLabel(text string) *widget.Label {
	l := wrappedLabel(text)
	l.Importance = widget.LowImportance
	l.SizeName = theme.SizeNameCaptionText
	return l
}

func sectionTitle(text string) *widget.Label {
	l := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.SizeName = theme.SizeNameSubHeadingText
	return l
}

func sectionCard(title, subtitle string, body fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(widget.NewCard(title, subtitle, body), cardTheme{fyne.CurrentApp().Settings().Theme()})
}

func iconTile(resource fyne.Resource, tint fyne.ThemeColorName) fyne.CanvasObject {
	return container.NewCenter(widget.NewIcon(theme.NewColoredResource(resource, tint)))
}

// choiceButton only selects the standard Button importance; rendering and
// keyboard, hover, focus and disabled behavior stay with Fyne.
type choiceButton struct{ *widget.Button }

func newChoiceButton(text string, icon fyne.Resource, tapped func()) *choiceButton {
	b := &choiceButton{widget.NewButtonWithIcon(text, icon, tapped)}
	b.Select(false)
	return b
}

func (b *choiceButton) Select(selected bool) {
	b.Importance = widget.LowImportance
	if selected {
		b.Importance = widget.HighImportance
	}
	b.Refresh()
}

// Card styling changes only its surface color. Fyne owns the card's layout,
// padding, corner radius and elevation, as well as the controls inside it.
type cardTheme struct{ fyne.Theme }

func (t cardTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameBackground {
		return t.Theme.Color(colorPanel, variant)
	}
	return t.Theme.Color(name, variant)
}
