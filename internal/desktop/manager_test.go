package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func newTestNodeManager(t *testing.T) (*nodeManager, profile.Store) {
	t.Helper()
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	t.Cleanup(a.Quit)
	w := a.NewWindow("nodes")
	s := testutil.NewStore(t)
	for _, id := range []string{"first", "second"} {
		if err := s.SaveSubscription(profile.Subscription{ID: id, Name: "同名订阅", URL: "https://example.com/sub", Format: "clash", Nodes: []profile.Node{{ID: id, Name: id, Outbound: map[string]any{"type": "trojan", "server": "example.com", "server_port": 443, "password": "test"}}}}); err != nil {
			t.Fatal(err)
		}
	}
	m := newNodeManager(w, app.NewService(app.Options{Store: s}))
	w.SetContent(m.root)
	w.Resize(fyne.NewSize(860, 620))
	return m, s
}

func TestNodeManagementBrowsingDoesNotChangeActiveNodeOrRules(t *testing.T) {
	m, s := newTestNodeManager(t)
	before, err := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	if err != nil {
		t.Fatal(err)
	}
	if m.sourceTitle.Text != "本地节点" || m.nodeItems[0].ID != "a" {
		t.Fatal("initial source is not local")
	}
	m.subscriptions.Select(1)
	if m.sourceIndex != 2 || m.nodeItems[0].ID != "second" {
		t.Fatal("source navigation confused subscriptions with the same name")
	}
	// Exercise the recycled row's real callbacks, not just List.Select.
	row := m.nodes.CreateItem().(*managementRow)
	m.nodes.UpdateItem(0, row)
	test.Tap(row)
	test.TapSecondary(row)
	if m.contextMenu == nil || !m.contextMenu.Visible() {
		t.Fatal("secondary tap did not open a menu")
	}
	m.selectSource(0)
	if m.contextMenu != nil {
		t.Fatal("changing source retained a stale context menu")
	}
	m.nodes.UpdateItem(0, row)
	test.TapSecondary(row)
	m.SetLocked(true)
	if m.contextMenu != nil || !m.add.Disabled() {
		t.Fatal("locked manager retained a menu or enabled writes")
	}
	m.subscriptions.Select(1)
	test.Tap(m.localSource.Button)
	if m.sourceIndex != 0 || m.nodeItems[0].ID != "a" {
		t.Fatal("locked manager must still allow browsing")
	}
	selection, _ := s.Selection()
	if selection.Source != "manual" || selection.Node != "a" {
		t.Fatal("browsing or opening a menu changed the active node")
	}
	after, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	if string(after) != string(before) {
		t.Fatal("browsing changed routing config")
	}
}

func TestManagementMenusTargetClickedItemAndGuardLockedActions(t *testing.T) {
	m, s := newTestNodeManager(t)
	// A right-clicked subscription need not be the currently browsed source.
	m.selectSource(2)
	row := m.subscriptions.CreateItem().(*managementRow)
	m.subscriptions.UpdateItem(0, row)
	test.TapSecondary(row)
	if m.sourceIndex != 2 || m.contextMenu == nil {
		t.Fatal("right click should open a menu without navigating")
	}
	menu := m.subscriptionMenu("first")
	if len(menu.Items) != 3 || menu.Items[0].Label != "更新订阅" || menu.Items[1].Label != "编辑订阅" || menu.Items[2].Label != "删除订阅" {
		t.Fatal("subscription management actions missing")
	}
	m.hideContextMenu()
	m.SetLocked(true)
	menu.Items[2].Action() // An action captured before locking must be harmless.
	if m.window.Canvas().Overlays().Top() != nil {
		t.Fatal("stale menu action opened a dialog while locked")
	}
	for _, item := range m.nodeMenu("manual", "a").Items {
		if !item.Disabled {
			t.Fatal("local node action enabled while locked")
		}
	}
	for _, item := range m.subscriptionMenu("first").Items {
		if !item.Disabled {
			t.Fatal("subscription action enabled while locked")
		}
	}
	m.SetLocked(false)
	menu.Items[2].Action()
	confirmManagementDeletion(t, m)
	sources, _ := s.Sources()
	if len(sources) != 2 || sources[1].ID != "second" || m.sources[m.sourceIndex].ID != "second" {
		t.Fatal("delete targeted the browsed subscription rather than the clicked one")
	}
	if m.subscriptionMenu("first") != nil {
		t.Fatal("deleted source still has actions")
	}
	// A local-node menu must also work when another source is being browsed.
	m.nodeMenu("manual", "a").Items[1].Action()
	confirmManagementDeletion(t, m)
	sources, _ = s.Sources()
	if len(sources[0].Nodes) != 0 || len(sources[1].Nodes) != 1 {
		t.Fatal("local delete targeted the wrong node")
	}
}

func confirmManagementDeletion(t *testing.T, m *nodeManager) {
	confirmDialog(t, m.window)
}

func confirmDialog(t *testing.T, window fyne.Window) {
	t.Helper()
	overlay := window.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("delete did not request confirmation")
	}
	var confirm *widget.Button
	seen := map[fyne.CanvasObject]bool{}
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		if seen[object] {
			return
		}
		seen[object] = true
		switch object := object.(type) {
		case *widget.Button:
			if object.Importance == widget.HighImportance {
				confirm = object
			}
		case *fyne.Container:
			for _, child := range object.Objects {
				visit(child)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(object).Objects() {
				visit(child)
			}
		}
	}
	visit(overlay)
	if confirm == nil {
		t.Fatal("delete confirmation button missing")
	}
	test.Tap(confirm)
}

func TestSubscriptionNodeMenusAndMenuInvalidation(t *testing.T) {
	m, _ := newTestNodeManager(t)
	menu := m.nodeMenu("first", "first")
	if len(menu.Items) != 1 || !menu.Items[0].Disabled || menu.Items[0].Action != nil {
		t.Fatal("subscription nodes must be managed through their subscription")
	}
	for i := range m.sources {
		if m.sources[i].ID == "first" {
			m.sources[i].URL = ""
		}
	}
	if !m.subscriptionMenu("first").Items[0].Disabled {
		t.Fatal("a local JSON subscription cannot be fetched from an absent URL")
	}
	m.showContextMenu(m.nodeMenu("manual", "a"), fyne.NewPos(20, 20))
	m.refresh()
	if m.contextMenu != nil {
		t.Fatal("refresh retained a menu referencing an old snapshot")
	}
	if m.nodeMenu("manual", "missing") != nil || m.subscriptionMenu("manual") != nil {
		t.Fatal("invalid management target has actions")
	}
}

func TestNodeSourceRefreshPreservesIDAndFallsBackAfterDeletion(t *testing.T) {
	m, s := newTestNodeManager(t)
	m.selectSource(1)
	sources, _ := s.Sources()
	sub := sources[1]
	sub.Name = "改名的订阅"
	if err := s.SaveSubscription(sub); err != nil {
		t.Fatal(err)
	}
	m.refresh()
	if m.sourceIndex != 1 || m.sourceTitle.Text != sub.Name {
		t.Fatal("refresh lost selected subscription ID")
	}
	if err := s.DeleteSubscription(sub.ID); err != nil {
		t.Fatal(err)
	}
	m.refresh()
	if m.sourceIndex != 0 || m.sourceTitle.Text != "本地节点" {
		t.Fatal("deleted source did not fall back safely")
	}
}
