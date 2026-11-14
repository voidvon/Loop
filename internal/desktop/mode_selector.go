package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Buttons retain normal keyboard/focus behavior while the group owns the
// required selection and callback, including silent synchronization by callers.
type modeSelector struct {
	widget.DisableableWidget
	Options   []string
	Selected  string
	OnChanged func(string)
	buttons   []*widget.Button
}

func newModeSelector(options []string, changed func(string)) *modeSelector {
	m := &modeSelector{Options: options, OnChanged: changed}
	m.ExtendBaseWidget(m)
	return m
}

func (m *modeSelector) SetSelected(value string) {
	if value == m.Selected {
		return
	}
	m.Selected = value
	if m.OnChanged != nil {
		m.OnChanged(value)
	}
	m.Refresh()
}

func (m *modeSelector) CreateRenderer() fyne.WidgetRenderer {
	m.buttons = nil
	items := make([]fyne.CanvasObject, len(m.Options))
	for i, option := range m.Options {
		b := widget.NewButton(option, func() {
			if !m.Disabled() {
				m.SetSelected(option)
			}
		})
		m.buttons = append(m.buttons, b)
		items[i] = container.NewThemeOverride(b, segmentTheme{Theme: m.Theme()})
	}
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 10
	content := container.NewStack(bg, inset(4, 4, 4, 4, container.NewGridWithColumns(len(items), items...)))
	r := &modeSelectorRenderer{WidgetRenderer: widget.NewSimpleRenderer(content), selector: m, background: bg, items: items}
	r.Refresh()
	return r
}

type modeSelectorRenderer struct {
	fyne.WidgetRenderer
	selector   *modeSelector
	background *canvas.Rectangle
	items      []fyne.CanvasObject
}

func (r *modeSelectorRenderer) MinSize() fyne.Size { return fyne.NewSize(390, 36) }
func (r *modeSelectorRenderer) Refresh() {
	m := r.selector
	v := fyne.CurrentApp().Settings().ThemeVariant()
	r.background.FillColor = m.Theme().Color(colorSegment, v)
	r.background.Refresh()
	for i, b := range m.buttons {
		selected := m.Selected == m.Options[i]
		b.Importance = widget.LowImportance
		b.SetIcon(nil)
		if selected {
			b.Importance = widget.HighImportance
			b.SetIcon(iconArrow)
		}
		setEnabled(b, !m.Disabled())
		containerObject := r.items[i].(*container.ThemeOverride)
		// The override follows the current client theme after light/dark changes.
		containerObject.Theme = segmentTheme{Theme: m.Theme()}
		b.Refresh()
	}
	r.WidgetRenderer.Refresh()
}

type segmentTheme struct{ fyne.Theme }

func (t segmentTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 12
	case theme.SizeNameInnerPadding:
		return 4
	case theme.SizeNameInlineIcon:
		return 14
	case theme.SizeNameButtonRadius:
		return 7
	}
	return t.Theme.Size(name)
}
func (t segmentTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return t.Theme.Color(theme.ColorNameSelection, v)
	case theme.ColorNameForegroundOnPrimary:
		return t.Theme.Color(colorAccent, v)
	case theme.ColorNameForeground:
		return t.Theme.Color(colorSecondary, v)
	}
	return t.Theme.Color(name, v)
}
