//go:build darwin && cgo && !ios

package desktop

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type nativeTestFont struct{ fyne.SystemFontResource }

func (nativeTestFont) Content() []byte { panic("native UI parsed a Go font") }

func TestNativeThemeRendersWrappedTruncatedTextAndEntryCaret(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	fonts, err := loadSystemFonts()
	if err != nil {
		t.Fatal(err)
	}
	for _, face := range []*fyne.Resource{&fonts.regular, &fonts.semibold, &fonts.monospace, &fonts.monoBold, &fonts.symbols} {
		native, ok := (*face).(fyne.SystemFontResource)
		if !ok || native.SystemFontName() == "" {
			t.Fatal("theme font did not opt into CoreText")
		}
		*face = nativeTestFont{native}
	}
	th := newClientTheme()
	th.fonts = fonts
	a.Settings().SetTheme(th)
	w := a.NewWindow("native font widgets")
	defer w.Close()
	label := widget.NewLabel("🇺🇸 美国节点 🚀 A very long label to truncate")
	label.Truncation = fyne.TextTruncateEllipsis
	wrapped := widget.NewLabel("中文换行与 Latin words should share native measurement and drawing.")
	wrapped.Wrapping = fyne.TextWrapWord
	entry := widget.NewEntry()
	entry.SetText("Loop 中文")
	w.SetContent(container.NewVBox(label, wrapped, entry))
	w.Resize(fyne.NewSize(240, 220))
	w.Show()
	if image := w.Canvas().Capture(); image.Bounds().Empty() {
		t.Fatal("native widgets produced no image")
	}
	text := []rune(entry.Text)
	pad := th.Size(theme.SizeNameInnerPadding)
	for _, column := range []int{0, 2, 5, 6, 7} {
		x := fyne.MeasureText(string(text[:column]), th.Size(theme.SizeNameText), entry.TextStyle).Width
		event := &desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(pad+x, pad+2)}, Button: desktop.MouseButtonPrimary}
		entry.MouseDown(event)
		entry.MouseUp(event)
		if entry.CursorColumn != column || entry.CursorRow != 0 {
			t.Fatalf("click at native advance %v: caret = %d,%d, want 0,%d", x, entry.CursorRow, entry.CursorColumn, column)
		}
	}
}
