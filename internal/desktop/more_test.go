package desktop

import (
	"fyne.io/fyne/v2/test"
	"local/loop/internal/app"
	"testing"
)

func TestMenuPreferencesPersistAndDaemonSyncDoesNotWriteBack(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	p := newMorePane(a.Preferences())
	calls := 0
	p.OnChanged = func() { calls++ }
	test.Tap(p.modeLetter)
	test.Tap(p.dimDisconnected)
	test.Tap(p.policies)
	if calls != 3 {
		t.Fatalf("changes=%d", calls)
	}
	reopened := newMorePane(a.Preferences())
	if reopened.modeLetter.Checked || reopened.dimDisconnected.Checked || reopened.policies.Checked {
		t.Fatal("preferences lost")
	}
	p.SetPreferences(app.DefaultTrayPreferences())
	if calls != 3 {
		t.Fatal("daemon synchronization triggered another write")
	}
	if !p.modeLetter.Checked || !p.dimDisconnected.Checked || !p.policies.Checked {
		t.Fatal("daemon settings not shown")
	}
	if loadMenuBarPreferences(a.Preferences()) != app.DefaultTrayPreferences() {
		t.Fatal("daemon settings not persisted locally")
	}
}
