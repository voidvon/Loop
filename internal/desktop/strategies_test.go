package desktop

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	prettyview "github.com/ideaconnect/go-fyne-pretty-view/v2"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestStrategyDeleteRemovesOptionalReferencesAndSavesTogether(t *testing.T) {
	s, core := testutil.CoreStore(t)
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	queue := make(chan func(), 8)
	fyne.SetCurrentApp(importTestApp{App: a, driver: importTestDriver{Driver: a.Driver(), queue: queue}})
	w := a.NewWindow("delete strategy")
	c := newDocumentController(w, app.NewService(app.Options{Store: s, CorePath: core}))
	p := newStrategyPane(w, c, widget.NewLabel(""))
	defer p.Close()
	defer c.Close()
	w.SetContent(p.root)
	w.Resize(fyne.NewSize(1120, 760))
	before, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	selection, _ := os.ReadFile(filepath.Join(s.Dir, "selection.json"))
	for i, state := range p.states {
		if state.Group.Name == "US" {
			p.groups.Select(i)
			break
		}
	}
	if p.selected != "US" || p.remove.Disabled() {
		t.Fatal("US unavailable for deletion")
	}
	test.Tap(p.remove)
	if c.working || w.Canvas().Overlays().Top() == nil {
		t.Fatal("deletion must ask for confirmation before saving")
	}
	confirmDialog(t, w)
	if !c.working {
		t.Fatal("deletion didn't enter the document save workflow")
	}
	select {
	case complete := <-queue:
		complete()
	case <-time.After(25 * time.Second):
		t.Fatal("deletion didn't complete")
	}
	if c.working || p.selected != profile.DefaultPolicy || len(p.states) != 8 || w.Canvas().Overlays().Top() != nil {
		t.Fatal("delete failed to refresh the strategy page")
	}
	for _, state := range p.states {
		if state.Group.Name == "US" {
			t.Fatal("deleted group remains")
		}
		for _, member := range state.Members {
			if member.Key == "US" {
				t.Fatal("deleted group still offered as a member")
			}
		}
	}
	backup, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName+".bak"))
	afterSelection, _ := os.ReadFile(filepath.Join(s.Dir, "selection.json"))
	if !bytes.Equal(backup, before) || !bytes.Equal(selection, afterSelection) {
		t.Fatal("delete lost backup or changed selected node")
	}
}

func TestStrategyPageOwnsNodeSelectionAndPreservesDraft(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	w := a.NewWindow("strategies")
	s := testutil.NewStore(t)
	service := app.NewService(app.Options{Store: s})
	running := false
	shell := newDesktopShell(w, service, nil, widget.NewLabel("未连接"), widget.NewButton("启动", nil), func() bool { return running }, func(profile.Mode) {}, func(bool) {})
	defer shell.documents.Close()
	w.SetContent(shell.root)
	w.Resize(fyne.NewSize(1120, 760))
	p := shell.strategies
	calls := 0
	p.OnSelect = func(group, key string) {
		calls++
		if err := s.SelectStrategy(group, key); err != nil {
			t.Fatal(err)
		}
		p.Refresh()
	}
	before, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	shell.selectPage(pagePolicy)
	if len(p.states) != 9 || calls != 0 {
		t.Fatal("refresh changed selection or default groups missing")
	}
	p.groups.Select(1) // YouTube
	if calls != 0 {
		t.Fatal("browsing groups changed traffic")
	}
	p.members.Select(strategyMemberIndex(t, p, profile.NodeKey("manual", "a")))
	choice, _ := s.StrategyChoice("YouTube")
	if calls != 1 || choice != profile.NodeKey("manual", "a") {
		t.Fatal("member tap did not select a strategy", choice, calls)
	}
	// Live policy saves enable editing and creation while connected.
	running = true
	shell.documents.SavePolicies = func(ctx context.Context, data, expected []byte, connected bool) (app.DocumentSaveResult, error) {
		return service.SavePolicies(ctx, nil, data, expected, false)
	}
	shell.SetModeEnabled(true)
	if p.edit.Disabled() || p.add.Disabled() || !p.canSelect() {
		t.Fatal("wrong connected controls")
	}
	p.members.Select(strategyMemberIndex(t, p, profile.DefaultPolicy))
	if calls != 2 {
		t.Fatal("online choice wasn't requested")
	}
	shell.SetModeEnabled(false)
	p.members.Select(strategyMemberIndex(t, p, profile.NodeKey("manual", "a")))
	if calls != 2 {
		t.Fatal("locked callback changed selection")
	}
	running = false
	shell.SetModeEnabled(true)
	shell.configPane.Open()
	shell.configPane.document.view.SetInputConfig(prettyview.InputConfig{DebounceFor: -1})
	a.Clipboard().SetContent(`{"route":{"final":"direct"}}`)
	shell.configPane.document.view.SelectAll()
	shell.configPane.document.view.Paste() // An unsaved editor draft prevents cross-page writes.
	if !shell.documents.dirty() {
		t.Fatal("missing editor draft")
	}
	p.members.Select(strategyMemberIndex(t, p, profile.NodeKey("manual", "a")))
	if calls != 2 {
		t.Fatal("strategy selection ignored unsaved draft")
	}
	p.Refresh()
	if !shell.documents.dirty() {
		t.Fatal("strategy refresh replaced draft")
	}
	after, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	if !bytes.Equal(before, after) {
		t.Fatal("selection changed config")
	}
	if !strings.Contains(shell.currentPolicy.Text, "Available") {
		t.Fatal("home doesn't show default strategy")
	}
}

func strategyMemberIndex(t *testing.T, p *strategyPane, key string) int {
	t.Helper()
	for i, row := range p.rows {
		if row.member.Key == key {
			return i
		}
	}
	t.Fatalf("missing member %s", key)
	return -1
}

func TestStrategyCardsUseStableKeysAndKeyboardRespectsLock(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	s := testutil.NewStore(t)
	if err := s.SaveManual(profile.Node{ID: "b", Name: "A", Outbound: map[string]any{"type": "trojan", "server": "example.org", "server_port": 443, "password": "test"}}); err != nil {
		t.Fatal(err)
	}
	w := a.NewWindow("strategy cards")
	c := newDocumentController(w, app.NewService(app.Options{Store: s}))
	defer c.Close()
	p := newStrategyPane(w, c, widget.NewLabel(""))
	defer p.Close()
	w.SetContent(p.root)
	w.Resize(fyne.NewSize(820, 600))
	calls := 0
	want := profile.NodeKey("manual", "b")
	p.OnSelect = func(group, key string) {
		calls++
		if group != profile.DefaultPolicy || key != want {
			t.Fatalf("card selected wrong stable ID: %s / %s", group, key)
		}
		if err := s.SelectStrategy(group, key); err != nil {
			t.Fatal(err)
		}
		p.Refresh()
	}
	p.members.Highlight(strategyMemberIndex(t, p, want))
	p.available = false
	p.members.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if calls != 0 {
		t.Fatal("locked card accepted keyboard selection")
	}
	p.available = true
	p.members.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if calls != 1 || p.state().Selected != want {
		t.Fatal("keyboard did not select the duplicate name by stable key")
	}
	p.Refresh()
	if calls != 1 {
		t.Fatal("refresh submitted another selection")
	}
}
