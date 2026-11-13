package desktop

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func TestWindowControlKeepsKeyboardAndDisabledBehavior(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	calls := 0
	b := newWindowButton("关闭窗口", color.Black, theme.CancelIcon(), func() { calls++ })
	w := a.NewWindow("chrome")
	w.SetContent(b)
	test.Tap(b)
	b.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if calls != 2 {
		t.Fatalf("tap and keyboard should each invoke the action, got %d", calls)
	}
	b.Disable()
	test.Tap(b)
	b.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if calls != 2 {
		t.Fatal("disabled window control invoked its action")
	}
}

func TestWindowHeaderMatchesNativeDragRegion(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	header := newWindowHeader(windowActions{close: func() {}, minimize: func() {}, zoom: func() {}})
	content := widget.NewLabel("content")
	objects := []fyne.CanvasObject{content, header}
	l := windowFrameLayout{sidebarWidth: 208}
	l.Layout(objects, fyne.NewSize(1120, 760))
	if header.Size() != fyne.NewSize(208, windowHeaderHeight) || content.Position().Y != 0 || header.Position().Y != 0 {
		t.Fatal("window controls must occupy only the sidebar alongside page content")
	}
	if content.Size() != fyne.NewSize(1120, 760) {
		t.Fatal("window controls consumed a full row of content space")
	}
	if l.MinSize(objects).Height != fyne.Max(windowHeaderHeight, content.MinSize().Height) {
		t.Fatal("minimum window height includes an empty header row")
	}
}
