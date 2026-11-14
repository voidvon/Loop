package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Keep Check's state, callback, keyboard and disabled semantics while drawing
// a compact switch. Labels belong to the surrounding settings row.
type toggleSwitch struct {
	widget.Check
	focused bool
}

func newToggleSwitch(label string, changed func(bool)) *toggleSwitch {
	s := &toggleSwitch{}
	s.Text, s.OnChanged = label, changed
	s.ExtendBaseWidget(s)
	return s
}
func (s *toggleSwitch) FocusGained() { s.focused = true; s.Check.FocusGained() }
func (s *toggleSwitch) FocusLost()   { s.focused = false; s.Check.FocusLost() }

func (s *toggleSwitch) CreateRenderer() fyne.WidgetRenderer {
	r := &toggleSwitchRenderer{control: s, track: rounded(color.Transparent, 9), thumb: canvas.NewCircle(color.White)}
	r.Refresh()
	return r
}

type toggleSwitchRenderer struct {
	control *toggleSwitch
	track   *canvas.Rectangle
	thumb   *canvas.Circle
}

func (r *toggleSwitchRenderer) MinSize() fyne.Size { return fyne.NewSize(38, 30) }
func (r *toggleSwitchRenderer) Layout(size fyne.Size) {
	x, y := (size.Width-32)/2, (size.Height-18)/2
	r.track.Move(fyne.NewPos(x, y))
	r.track.Resize(fyne.NewSize(32, 18))
	if r.control.Checked {
		x += 14
	}
	r.thumb.Move(fyne.NewPos(x+2, y+2))
	r.thumb.Resize(fyne.NewSize(14, 14))
}
func (r *toggleSwitchRenderer) Refresh() {
	s := r.control
	v := fyne.CurrentApp().Settings().ThemeVariant()
	th := s.Theme()
	r.track.FillColor = th.Color(colorSwitch, v)
	if s.Checked {
		r.track.FillColor = th.Color(colorAccent, v)
	}
	r.thumb.FillColor = th.Color(colorSwitchThumb, v)
	if s.Disabled() {
		r.thumb.FillColor = th.Color(theme.ColorNameDisabled, v)
	}
	r.track.StrokeWidth = 0
	if s.focused && !s.Disabled() {
		r.track.StrokeWidth = 2
		r.track.StrokeColor = th.Color(colorAccent, v)
	}
	r.Layout(s.Size())
	r.track.Refresh()
	r.thumb.Refresh()
}
func (r *toggleSwitchRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.track, r.thumb}
}
func (r *toggleSwitchRenderer) Destroy() {}
