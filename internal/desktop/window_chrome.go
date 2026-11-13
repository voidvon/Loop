package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	windowHeaderHeight    float32 = 48
	windowControlDiameter float32 = 12
	windowControlPitch    float32 = 20
	windowControlsLeft    float32 = 10
)

// Fixed colors keep the macOS traffic-light symbols dark in both appearances.
// The green control uses two diagonal triangles, as in the native zoom control.
var (
	windowCloseIcon    = fyne.NewStaticResource("window-close.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 12 12"><path d="m3 3 6 6m0-6-6 6" fill="none" stroke="#4c100c" stroke-width="1.25" stroke-linecap="round"/></svg>`))
	windowMinimizeIcon = fyne.NewStaticResource("window-minimize.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 12 12"><path d="M2.5 6h7" fill="none" stroke="#795000" stroke-width="1.25" stroke-linecap="round"/></svg>`))
	windowZoomIcon     = fyne.NewStaticResource("window-zoom.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 12 12"><path d="M2.5 2.5h4.25L2.5 6.75Zm7 7H5.25L9.5 5.25Z" fill="#075e13"/></svg>`))
)

type windowActions struct {
	close, minimize, zoom func()
}

func newWindowHeader(actions windowActions) fyne.CanvasObject {
	controls := []*windowButton{
		newWindowButton("关闭窗口", color.NRGBA{255, 95, 87, 255}, windowCloseIcon, actions.close),
		newWindowButton("最小化", color.NRGBA{255, 189, 46, 255}, windowMinimizeIcon, actions.minimize),
		newWindowButton("缩放窗口", color.NRGBA{40, 200, 64, 255}, windowZoomIcon, actions.zoom),
	}
	buttons := container.New(windowControlsLayout{})
	for _, control := range controls {
		control.onHover = func(hovered bool) {
			for _, other := range controls {
				other.groupHovered = hovered
				other.Refresh()
			}
		}
		buttons.Add(control)
	}
	background := themedSurface(colorSidebar, 0)
	return container.NewStack(background, inset(10, 10, windowControlsLeft, 0, container.NewBorder(nil, nil, buttons, nil)))
}

// Native traffic lights have a 12-point diameter and 20-point center spacing.
// Avoid HBox's application theme padding, which made the controls too far apart.
type windowControlsLayout struct{}

func (windowControlsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(float32(len(objects))*windowControlPitch, 28)
}
func (windowControlsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for index, object := range objects {
		object.Move(fyne.NewPos(float32(index)*windowControlPitch, 0))
		object.Resize(fyne.NewSize(windowControlPitch, size.Height))
	}
}

// Window controls occupy only the sidebar. Main content starts at the top of
// the window, alongside the controls instead of below an empty full-width row.
type windowFrameLayout struct{ sidebarWidth float32 }

func (windowFrameLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(fyne.Max(objects[0].MinSize().Width, objects[1].MinSize().Width),
		fyne.Max(windowHeaderHeight, objects[0].MinSize().Height))
}
func (l windowFrameLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
	objects[1].Move(fyne.NewPos(0, 0))
	objects[1].Resize(fyne.NewSize(l.sidebarWidth, windowHeaderHeight))
}

// Keep Button's focus, keyboard and disabled behavior, while painting compact
// macOS-style controls instead of a standard rectangular button.
type windowButton struct {
	widget.Button
	fill         color.Color
	hovered      bool
	groupHovered bool
	onHover      func(bool)
	focused      bool
}

func newWindowButton(label string, fill color.Color, icon fyne.Resource, tapped func()) *windowButton {
	b := &windowButton{fill: fill}
	b.Text, b.Icon, b.OnTapped = label, icon, tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *windowButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	if b.onHover != nil {
		b.onHover(true)
	}
	b.Refresh()
}
func (b *windowButton) MouseOut() {
	b.hovered = false
	if b.onHover != nil {
		b.onHover(false)
	}
	b.Refresh()
}
func (b *windowButton) MouseMoved(*desktop.MouseEvent) {}
func (b *windowButton) FocusGained()                   { b.focused = true; b.Refresh() }
func (b *windowButton) FocusLost()                     { b.focused = false; b.Refresh() }

func (b *windowButton) CreateRenderer() fyne.WidgetRenderer {
	r := &windowButtonRenderer{button: b, dot: canvas.NewCircle(b.fill), glyph: canvas.NewImageFromResource(b.Icon)}
	r.Refresh()
	return r
}

type windowButtonRenderer struct {
	button *windowButton
	dot    *canvas.Circle
	glyph  *canvas.Image
}

func (r *windowButtonRenderer) MinSize() fyne.Size { return fyne.NewSize(windowControlPitch, 28) }
func (r *windowButtonRenderer) Layout(size fyne.Size) {
	r.dot.Resize(fyne.NewSize(windowControlDiameter, windowControlDiameter))
	r.dot.Move(fyne.NewPos((size.Width-windowControlDiameter)/2, (size.Height-windowControlDiameter)/2))
	r.glyph.Resize(r.dot.Size())
	r.glyph.Move(r.dot.Position())
}
func (r *windowButtonRenderer) Refresh() {
	r.dot.FillColor = r.button.fill
	r.dot.StrokeWidth = 0
	if r.button.focused {
		r.dot.StrokeColor = r.button.Theme().Color(colorAccent, fyne.CurrentApp().Settings().ThemeVariant())
		r.dot.StrokeWidth = 2
	}
	r.glyph.Resource = r.button.Icon
	r.glyph.Hidden = !r.button.hovered && !r.button.groupHovered && !r.button.focused
	r.Layout(r.button.Size())
	r.dot.Refresh()
	r.glyph.Refresh()
}
func (r *windowButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.dot, r.glyph}
}
func (r *windowButtonRenderer) Destroy() {}
