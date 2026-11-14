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
	return container.NewStack(themedSurface(colorPanel, 20), inset(14, 14, 14, 14, content))
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
	return panel(container.NewBorder(container.NewVBox(sectionTitle(title), quietLabel(subtitle)), nil, nil, nil, inset(12, 0, 0, 0, body)))
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

// Fyne owns card layout and interaction; the theme supplies a soft surface.
type cardTheme struct{ fyne.Theme }

func (t cardTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameBackground {
		return t.Theme.Color(colorPanel, variant)
	}
	if name == theme.ColorNameShadow {
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}

// Sidebar selection is a quiet material highlight; primary actions elsewhere
// keep their accent color and their usual foreground contrast.
type navigationTheme struct{ fyne.Theme }

func (t navigationTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return t.Theme.Color(theme.ColorNameSelection, variant)
	case theme.ColorNameForegroundOnPrimary:
		return t.Theme.Color(colorAccent, variant)
	case theme.ColorNameForeground:
		return t.Theme.Color(colorSecondary, variant)
	}
	return t.Theme.Color(name, variant)
}

func (t navigationTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameInlineIcon:
		return 20
	case theme.SizeNameButtonRadius:
		return 10
	}
	return t.Theme.Size(name)
}

type compactActionTheme struct{ fyne.Theme }

func (t compactActionTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInnerPadding:
		return 5
	case theme.SizeNameInlineIcon:
		return 17
	case theme.SizeNameText:
		return 12
	}
	return t.Theme.Size(name)
}

type circleActionTheme struct{ fyne.Theme }

func (t circleActionTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameButtonRadius {
		return 18
	}
	if name == theme.SizeNameInlineIcon {
		return 16
	}
	if name == theme.SizeNameInnerPadding {
		return 6
	}
	return t.Theme.Size(name)
}
func (t circleActionTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameButton {
		return t.Theme.Color(colorSegment, variant)
	}
	return t.Theme.Color(name, variant)
}
