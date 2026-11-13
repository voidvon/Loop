package desktop

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/miniproxy/internal/app"
	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/testutil"
)

func TestStandardModeControlsSyncWithoutChangingConnection(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	w := a.NewWindow("mode controls")
	modeChanges, tunChanges := 0, 0
	shell := newDesktopShell(w, app.NewService(app.Options{Store: testutil.NewStore(t)}), nil,
		widget.NewLabel("未连接"), widget.NewButton("启动", nil), func() bool { return false },
		func(profile.Mode) { modeChanges++ }, func(bool) { tunChanges++ })
	defer shell.documents.Close()
	w.SetContent(shell.root)
	shell.SyncMode()
	shell.SyncMode()
	if modeChanges != 0 || tunChanges != 0 {
		t.Fatal("synchronizing controls changed the connection")
	}
	selected := shell.modeSelect.Selected
	items := test.WidgetRenderer(shell.modeSelect).Objects()
	shell.SetModeEnabled(false)
	test.Tap(items[2].(fyne.Tappable))
	test.Tap(shell.tunSwitch)
	if shell.modeSelect.Selected != selected || modeChanges != 0 || tunChanges != 0 {
		t.Fatal("locked controls changed the connection")
	}
	shell.SetModeEnabled(true)
	test.Tap(items[2].(fyne.Tappable))
	if shell.modeSelect.Selected != profile.ModeGlobal.Label() || modeChanges != 1 {
		t.Fatal("standard radio selection did not request a mode change")
	}
	shell.SyncMode()
	if shell.modeSelect.Selected != selected || modeChanges != 1 || tunChanges != 0 {
		t.Fatal("refresh caused a second mode change")
	}
}
