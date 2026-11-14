package desktop

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/proxy"
	"local/loop/internal/testutil"
)

func TestSegmentedModeControlsSyncWithoutChangingConnection(t *testing.T) {
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
	test.WidgetRenderer(shell.modeSelect)
	items := shell.modeSelect.buttons
	shell.SetModeEnabled(false)
	test.Tap(items[2])
	items[2].TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	test.Tap(shell.tunSwitch)
	if shell.modeSelect.Selected != selected || modeChanges != 0 || tunChanges != 0 {
		t.Fatal("locked controls changed the connection")
	}
	shell.SetModeEnabled(true)
	items[2].TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if shell.modeSelect.Selected != profile.ModeGlobal.Label() || modeChanges != 1 {
		t.Fatal("segmented selection did not request a mode change")
	}
	shell.SyncMode()
	if shell.modeSelect.Selected != selected || modeChanges != 1 || tunChanges != 0 {
		t.Fatal("refresh caused a second mode change")
	}
}

func TestTUNControlCanSwitchWhileConnectedAndSyncAfterRecovery(t *testing.T) {
	if !proxy.TUNAvailable() {
		t.Skip("TUN is not supported on this platform")
	}
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	w := a.NewWindow("live TUN")
	s := testutil.NewStore(t)
	var requests []bool
	shell := newDesktopShell(w, app.NewService(app.Options{Store: s}), nil,
		widget.NewLabel("已连接"), widget.NewButton("停止", nil), func() bool { return true },
		func(profile.Mode) {}, func(enabled bool) { requests = append(requests, enabled) })
	defer shell.documents.Close()
	w.SetContent(shell.root)
	shell.SyncMode()
	shell.SetModeEnabled(true)
	if shell.tunSwitch.Disabled() {
		t.Fatal("TUN control is disabled while connected")
	}
	test.Tap(shell.tunSwitch)
	if len(requests) != 1 || !requests[0] {
		t.Fatal("connected TUN enable was not requested", requests)
	}
	shell.SetModeEnabled(false)
	test.Tap(shell.tunSwitch)
	if len(requests) != 1 {
		t.Fatal("busy TUN control accepted a second request")
	}
	shell.SyncMode() // A failed switch has restored the saved false setting.
	if shell.tunSwitch.Checked || len(requests) != 1 {
		t.Fatal("rollback refresh changed capture again")
	}
	if err := s.SaveTUN(true); err != nil {
		t.Fatal(err)
	}
	shell.SyncMode()
	shell.SetModeEnabled(true)
	test.Tap(shell.tunSwitch)
	if len(requests) != 2 || requests[1] {
		t.Fatal("connected TUN disable was not requested", requests)
	}
	shell.rawConfig = true
	shell.SetModeEnabled(true)
	if !shell.tunSwitch.Disabled() {
		t.Fatal("complete config mode exposed the TUN override")
	}
}
