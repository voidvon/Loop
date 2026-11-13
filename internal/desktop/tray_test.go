package desktop

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/miniproxy/internal/app"
	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/testutil"
)

type trayRecorder struct {
	menu         *fyne.Menu
	icon         fyne.Resource
	menus, icons int
}

func (r *trayRecorder) SetSystemTrayMenu(m *fyne.Menu)       { r.menu = m; r.menus++ }
func (r *trayRecorder) SetSystemTrayIcon(icon fyne.Resource) { r.icon = icon; r.icons++ }

func trayFixture() trayState {
	states := []profile.PolicyState{
		{Group: profile.PolicyGroup{Name: "Available"}, Selected: "node-1"},
		{Group: profile.PolicyGroup{Name: "YouTube"}, Selected: "Available", Members: []profile.PolicyMember{{Key: "Available", Name: "Available", IsGroup: true}, {Key: "US", Name: "US", IsGroup: true}}},
		{Group: profile.PolicyGroup{Name: "US"}, Error: "暂无可用子策略"},
	}
	for i := range 220 {
		states[0].Members = append(states[0].Members, profile.PolicyMember{Key: fmt.Sprintf("node-%d", i), Name: fmt.Sprintf("节点 %d", i), Detail: "测试订阅"})
	}
	return trayState{Mode: profile.ModeRule, CanToggle: true, CanSelect: true, Policies: states, Preferences: defaultMenuBarPreferences()}
}

func TestMenuBarListsEveryStrategyAndKeepsCheckedChoices(t *testing.T) {
	state := trayFixture()
	target := &trayRecorder{}
	var selectedGroup, selectedKey string
	toggled, shown, quit := 0, 0, 0
	bar := newMenuBar(target, func() trayState { return state }, func(fn func()) { fn() }, trayActions{
		Toggle: func() { toggled++ }, Select: func(g, k string) { selectedGroup, selectedKey = g, k }, Show: func() { shown++ }, Quit: func() { quit++ },
	})
	defer bar.Close()
	menu := target.menu
	if len(menu.Items) != 8 || len(menu.Items[2].ChildMenu.Items) != 220 || len(menu.Items[3].ChildMenu.Items) != 2 {
		t.Fatal("menu omitted strategies or nodes")
	}
	if !menu.Items[2].ChildMenu.Items[1].Checked || !menu.Items[3].ChildMenu.Items[0].Checked || !menu.Items[3].ChildMenu.Items[1].Disabled {
		t.Fatal("wrong current selection or invalid group is selectable")
	}
	menu.Items[2].ChildMenu.Items[219].Action()
	if selectedGroup != "Available" || selectedKey != "node-219" {
		t.Fatal("menu selected wrong stable node key", selectedGroup, selectedKey)
	}
	menu.Items[0].Action()
	menu.Items[6].Action()
	menu.Items[7].Action()
	if toggled != 1 || shown != 1 || quit != 1 || !menu.Items[7].IsQuit {
		t.Fatal("tray lifecycle actions not wired")
	}
	bar.Refresh()
	if target.menus != 1 || target.icons != 1 {
		t.Fatal("unchanged snapshot rebuilt native menus")
	}
	state.Running = true
	state.Mode = profile.ModeGlobal
	state.Policies[0].Selected = "node-219"
	bar.Refresh()
	if !target.menu.Items[0].Checked || !target.menu.Items[2].ChildMenu.Items[219].Checked || target.icons != 2 {
		t.Fatal("tray diverged after state/strategy change")
	}
}

func TestMenuBarRechecksQueuedActionsAndShutsDown(t *testing.T) {
	state := trayFixture()
	target := &trayRecorder{}
	var queue []func()
	calls := 0
	bar := newMenuBar(target, func() trayState { return state }, func(fn func()) { queue = append(queue, fn) }, trayActions{Toggle: func() { calls++ }, Select: func(string, string) { calls++ }, Quit: func() { calls++ }})
	stale := target.menu
	stale.Items[2].ChildMenu.Items[2].Action()
	state.Policies[0].Members = state.Policies[0].Members[:1] // subscription refreshed before delivery
	queue[0]()
	queue = nil
	if calls != 0 {
		t.Fatal("stale node key changed selection")
	}
	stale.Items[0].Action()
	state.CanToggle = false
	queue[0]()
	queue = nil
	if calls != 0 {
		t.Fatal("queued toggle ignored the shared lock")
	}
	state.Busy = true
	state.CanSelect = false
	bar.Refresh()
	if !target.menu.Items[0].Disabled || !target.menu.Items[2].Disabled || !target.menu.Items[2].ChildMenu.Items[0].Disabled {
		t.Fatal("busy menu still allows conflicting actions")
	}
	stale.Items[7].Action()
	bar.Close()
	queue[0]()
	count := target.menus
	bar.Refresh()
	if calls != 0 || target.menus != count {
		t.Fatal("closed tray executed an action or posted a refresh")
	}
}

func TestMenuBarTracksShellDraftsAndModeWithoutConnecting(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	store := testutil.NewStore(t)
	w := a.NewWindow("tray state")
	running := false
	shell := newDesktopShell(w, app.NewService(app.Options{Store: store}), nil, widget.NewLabel("未连接"), widget.NewButton("启动", nil), func() bool { return running }, func(profile.Mode) {}, func(bool) {})
	defer shell.documents.Close()
	defer shell.strategies.Close()
	target := &trayRecorder{}
	bar := newMenuBar(target, func() trayState { return shell.menuBarState(false, false, true) }, func(fn func()) { fn() }, trayActions{})
	defer bar.Close()
	shell.OnStateChanged = bar.Refresh
	if target.menu.Items[2].Disabled {
		t.Fatal("stopped tray cannot select a node")
	}
	shell.documents.HasDraft = func() bool { return true }
	shell.documents.changed()
	if !target.menu.Items[2].Disabled || target.menu.Items[0].Disabled {
		t.Fatal("draft lock failed or incorrectly blocked the connection switch")
	}
	shell.documents.HasDraft = func() bool { return false }
	shell.documents.changed()
	if target.menu.Items[2].Disabled {
		t.Fatal("discarding draft didn't unlock tray")
	}
	if err := store.SaveMode(profile.ModeDirect); err != nil {
		t.Fatal(err)
	}
	shell.SyncMode()
	if !strings.Contains(target.icon.Name(), "-D.") {
		t.Fatal("mode icon didn't follow homepage mode")
	}
	running = true
	shell.SyncMode()
	if !target.menu.Items[0].Checked {
		t.Fatal("connection status didn't reach tray")
	}
	shell.SetModeEnabled(false)
	if !target.menu.Items[2].Disabled {
		t.Fatal("shared UI lock didn't reach tray")
	}
}

func TestTrayIconUsesTemplateAlphaAndDistinctModeGlyphs(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	seen := map[string]bool{}
	for _, mode := range []profile.Mode{profile.ModeGlobal, profile.ModeRule, profile.ModeDirect} {
		active, inactive := trayIcon(mode, true), trayIcon(mode, false)
		if _, ok := active.(*theme.ThemedResource); !ok {
			t.Fatal("macOS menu-bar icon won't adapt to OS appearance")
		}
		svg := string(active.Content())
		if seen[svg] {
			t.Fatal("mode glyph or connection alpha is incorrect")
		}
		for _, tc := range []struct {
			resource fyne.Resource
			want     uint32
		}{{active, 65535}, {inactive, 32896}} {
			maxAlpha := trayResourceMaxAlpha(tc.resource)
			if maxAlpha != tc.want {
				t.Fatalf("rendered tray alpha=%d, want %d", maxAlpha, tc.want)
			}
		}
		seen[svg] = true
	}
}

func trayResourceMaxAlpha(resource fyne.Resource) uint32 {
	c := software.NewTransparentCanvas()
	c.SetPadded(false)
	c.SetContent(canvas.NewImageFromResource(resource))
	c.Resize(fyne.NewSize(48, 48))
	img := c.Capture()
	maxAlpha := uint32(0)
	for y := range img.Bounds().Dy() {
		for x := range img.Bounds().Dx() {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha > maxAlpha {
				maxAlpha = alpha
			}
		}
	}
	return maxAlpha
}
