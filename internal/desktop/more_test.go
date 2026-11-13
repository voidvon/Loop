package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/miniproxy/internal/app"
	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/testutil"
)

func TestMorePreferencesUpdateTrayAndPersistWithoutChangingProxy(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	store := testutil.NewStore(t)
	w := a.NewWindow("more settings")
	modeChanges, tunChanges := 0, 0
	running := true
	shell := newDesktopShell(w, app.NewService(app.Options{Store: store}), nil,
		widget.NewLabel("已连接"), widget.NewButton("停止", nil), func() bool { return running },
		func(profile.Mode) { modeChanges++ }, func(bool) { tunChanges++ })
	defer shell.documents.Close()
	defer shell.strategies.Close()
	files := func() map[string]string {
		t.Helper()
		values := map[string]string{}
		err := filepath.WalkDir(store.Dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			values[path] = string(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return values
	}
	before := files()
	target := &trayRecorder{}
	bar := newMenuBar(target, func() trayState { return shell.menuBarState(false, false, true) }, func(fn func()) { fn() }, trayActions{})
	defer bar.Close()
	shell.OnStateChanged = bar.Refresh

	test.Tap(shell.navigation[pageMore].Button)
	if shell.current != pageMore || shell.content.Objects[0] != shell.more.root {
		t.Fatal("More navigation did not open menu-bar settings")
	}
	test.Tap(shell.more.modeLetter)
	if !strings.Contains(target.icon.Name(), "plain") {
		t.Fatal("changing letter display did not update native icon")
	}
	// Dim changes while connected must also apply to the next disconnect.
	test.Tap(shell.more.dimDisconnected)
	running = false
	shell.SyncMode()
	if trayResourceMaxAlpha(target.icon) != 65535 {
		t.Fatal("disconnected icon ignored opacity preference")
	}
	test.Tap(shell.more.dimDisconnected)
	if trayResourceMaxAlpha(target.icon) != 32896 {
		t.Fatal("restoring dimming did not update the disconnected icon")
	}
	test.Tap(shell.more.dimDisconnected)
	test.Tap(shell.more.policies)
	if len(target.menu.Items) != 4 || target.menu.Items[0].Label != "启用代理" || target.menu.Items[2].Label != "显示主窗口" || !target.menu.Items[3].IsQuit {
		t.Fatal("hiding strategies changed connection/window actions or left extra separators")
	}
	reopened := newMorePane(a.Preferences())
	if reopened.modeLetter.Checked || reopened.dimDisconnected.Checked || reopened.policies.Checked {
		t.Fatal("reopening settings lost saved preferences")
	}
	if modeChanges != 0 || tunChanges != 0 || !reflect.DeepEqual(before, files()) {
		t.Fatal("UI preferences modified proxy settings or profile documents")
	}
	test.Tap(shell.more.policies)
	if len(target.menu.Items) <= 4 || target.menu.Items[2].ChildMenu == nil {
		t.Fatal("restoring strategies did not rebuild node choices")
	}
}

func TestMenuBarRejectsStrategyActionAfterListIsHidden(t *testing.T) {
	state := trayFixture()
	var queued func()
	calls := 0
	target := &trayRecorder{}
	bar := newMenuBar(target, func() trayState { return state }, func(fn func()) { queued = fn }, trayActions{
		Select: func(string, string) { calls++ },
	})
	defer bar.Close()
	target.menu.Items[2].ChildMenu.Items[0].Action()
	state.Preferences.ShowPolicies = false
	bar.Refresh()
	queued()
	if calls != 0 {
		t.Fatal("hidden native strategy menu delivered a stale selection")
	}
}
