package desktop

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestStrategyMemberPickerSeparatesSourcesAndKeepsStableChoices(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	states := []profile.PolicyState{{Group: profile.PolicyGroup{Name: profile.DefaultPolicy}}, {Group: profile.PolicyGroup{Name: "Custom"}}}
	sources := []profile.Subscription{{ID: "manual", Name: "本地", Nodes: []profile.Node{{ID: "a", Name: "Duplicate"}}}, {ID: "one", Name: "订阅一", Nodes: []profile.Node{{ID: "a", Name: "Duplicate"}}}}
	group := profile.PolicyGroup{Name: "Custom", AllNodes: true, Builtins: []string{"reject"}, Nodes: []string{profile.NodeKey("one", "a")}, Sources: []string{"one"}, Groups: []string{profile.DefaultPolicy}}
	p := newStrategyMemberPicker(group, states, sources)
	w := a.NewWindow("members")
	w.SetContent(p.tree)
	w.Resize(fyne.NewSize(600, 500))
	p.tree.OpenAllBranches()
	if len(p.children[""]) != 5 || p.options["group:Custom"].key != "" {
		t.Fatal("wrong categories or self reference")
	}
	var got profile.PolicyGroup
	p.apply(&got)
	if got.AllNodes != group.AllNodes || !reflect.DeepEqual(got.Builtins, group.Builtins) || !reflect.DeepEqual(got.Nodes, group.Nodes) || !reflect.DeepEqual(got.Sources, group.Sources) || !reflect.DeepEqual(got.Groups, group.Groups) {
		t.Fatal("round trip lost or broadened selection", got)
	}
	// Exercise the actual standard checkbox callback, including recycled rows.
	row := p.tree.CreateNode(false)
	p.tree.UpdateNode("node:"+profile.NodeKey("manual", "a"), false, row)
	test.Tap(row.(*fyne.Container).Objects[1].(*widget.Check))
	p.apply(&got)
	if len(got.Nodes) != 2 {
		t.Fatal("local node checkbox did not persist")
	}
	p.tree.UpdateNode("builtin", true, row)
	if !row.(*fyne.Container).Objects[0].Visible() || row.(*fyne.Container).Objects[1].Visible() {
		t.Fatal("branch must render as a standard label")
	}
	group.Nodes = append(group.Nodes, profile.NodeKey("missing", "deleted"))
	p = newStrategyMemberPicker(group, states, sources)
	p.apply(&got)
	if len(got.Nodes) != 2 || len(p.children["unavailable"]) != 1 {
		t.Fatal("unavailable explicit member silently discarded")
	}
}

func TestStrategyPickerCanSelectSubscriptionParents(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	sources := []profile.Subscription{{ID: "manual", Name: "本地节点"}, {ID: "airport", Name: "我的订阅", Nodes: []profile.Node{{ID: "a", Name: "节点 A"}, {ID: "b", Name: "节点 B"}}}}
	p := newStrategyMemberPicker(profile.PolicyGroup{Name: "Custom"}, nil, sources)
	w := a.NewWindow("parent selection")
	w.SetContent(p.tree)
	row := p.tree.CreateNode(true).(*fyne.Container)
	p.tree.UpdateNode("source:airport", true, row)
	check := row.Objects[1].(*widget.Check)
	if !check.Visible() || check.Disabled() {
		t.Fatal("subscription parent cannot be selected")
	}
	test.Tap(check)
	var got profile.PolicyGroup
	p.apply(&got)
	if !reflect.DeepEqual(got.Sources, []string{"airport"}) || len(got.Nodes) != 0 {
		t.Fatal("parent selection must retain a dynamic source, not a node snapshot", got)
	}
	p.filter("节点 A")
	if !p.visible["source:airport"] || !p.selected["source:airport"] {
		t.Fatal("search lost selected subscription")
	}
	p.tree.UpdateNode("source:manual", true, row)
	test.Tap(row.Objects[1].(*widget.Check))
	p.apply(&got)
	if !reflect.DeepEqual(got.Sources, []string{"manual", "airport"}) {
		t.Fatal("local parent was not selectable", got)
	}
}

func TestStrategyOnlineSaveUsesTransactionAndSynchronizesEditor(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "rollback"}[fail], func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			a := test.NewApp()
			defer a.Quit()
			a.Settings().SetTheme(newClientTheme())
			queue := make(chan func(), 8)
			fyne.SetCurrentApp(importTestApp{App: a, driver: importTestDriver{Driver: a.Driver(), queue: queue}})
			w := a.NewWindow("online policies")
			service := app.NewService(app.Options{Store: s, CorePath: core})
			running := true
			shell := newDesktopShell(w, service, nil, widget.NewLabel("已连接"), widget.NewButton("停止", nil), func() bool { return running }, func(profile.Mode) {}, func(bool) {})
			c, p := shell.documents, shell.strategies
			defer c.Close()
			defer p.Close()
			backend := &rulesApplyTestBackend{failNext: fail}
			c.Running = func() bool { return running }
			c.SavePolicies = func(ctx context.Context, data, expected []byte, connected bool) (app.DocumentSaveResult, error) {
				return service.SavePolicies(ctx, backend, data, expected, connected)
			}
			c.OnApplied = func(result app.RuleSaveResult) { running = result.Running }
			c.OnBusy = func(busy bool) { shell.SetModeEnabled(!busy) }
			shell.SetModeEnabled(true)
			w.SetContent(shell.root)
			w.Resize(fyne.NewSize(1120, 760))
			p.groups.Select(1)
			if p.add.Disabled() || p.edit.Disabled() || p.remove.Disabled() {
				t.Fatal("connected actions are locked")
			}
			before := append([]byte(nil), p.baseline...)
			groups := []profile.PolicyGroup{}
			for _, state := range p.states {
				groups = append(groups, state.Group)
			}
			groups = append(groups, profile.PolicyGroup{Name: "Custom", Builtins: []string{"direct", "reject"}})
			completed := false
			var operationErr error
			p.commit(groups, before, func(err error) { completed = true; operationErr = err })
			if !c.working || !p.add.Disabled() {
				t.Fatal("transaction didn't lock controls")
			}
			select {
			case done := <-queue:
				done()
			case <-time.After(25 * time.Second):
				t.Fatal("save did not complete")
			}
			if !completed || (operationErr != nil) != fail || !running || c.working || p.add.Disabled() {
				t.Fatal("wrong completion state", operationErr)
			}
			if backend.starts != map[bool]int{false: 1, true: 2}[fail] {
				t.Fatal("didn't automatically reconnect")
			}
			after, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
			shell.configPane.Open()
			if !bytes.Equal(p.baseline, after) || !bytes.Equal(shell.configPane.document.baseline, after) || bytes.Equal(before, after) != fail {
				t.Fatal("policy, editor and document diverged")
			}
		})
	}
}

func TestAvailableUsesOrdinaryEditingDeletionAndDefaultControls(t *testing.T) {
	s, core := testutil.CoreStore(t)
	config := map[string]any{"proxy_ports": map[string]any{"default": 2080, "global": 2081, "global_enabled": false}, "route": map[string]any{"final": "direct"}, "policy_groups": []profile.PolicyGroup{{Name: profile.DefaultPolicy, AllNodes: true}, {Name: "Other", Builtins: []string{"direct"}}}}
	if err := profile.WriteJSON(filepath.Join(s.Dir, profile.MainConfigName), config); err != nil {
		t.Fatal(err)
	}
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	queue := make(chan func(), 8)
	fyne.SetCurrentApp(importTestApp{App: a, driver: importTestDriver{Driver: a.Driver(), queue: queue}})
	w := a.NewWindow("ordinary Available")
	service := app.NewService(app.Options{Store: s, CorePath: core})
	c := newDocumentController(w, service)
	defer c.Close()
	p := newStrategyPane(w, c, widget.NewLabel(""))
	defer p.Close()
	w.SetContent(p.root)
	w.Resize(fyne.NewSize(1120, 760))
	if p.edit.Disabled() || p.remove.Disabled() || !p.setDefault.Disabled() {
		t.Fatal("Available is still locked")
	}
	test.Tap(p.edit)
	popup := w.Canvas().Overlays().Top()
	if popup == nil {
		t.Fatal("Available edit dialog didn't open")
	}
	popup.Hide()
	w.Canvas().Overlays().Remove(popup)
	p.groups.Select(1)
	if p.setDefault.Disabled() {
		t.Fatal("another group cannot become default")
	}
	test.Tap(p.setDefault)
	select {
	case done := <-queue:
		done()
	case <-time.After(25 * time.Second):
		t.Fatal("default save timed out")
	}
	if p.defaultPolicy != "Other" || !p.setDefault.Disabled() || p.current.Text != "Other  →  DIRECT" {
		t.Fatal("default was not reflected in home", p.defaultPolicy, p.current.Text)
	}
	p.groups.Select(0)
	if p.remove.Disabled() || p.edit.Disabled() {
		t.Fatal("former default is still locked")
	}
	test.Tap(p.remove)
	confirmDialog(t, w)
	select {
	case done := <-queue:
		done()
	case <-time.After(25 * time.Second):
		t.Fatal("Available deletion timed out")
	}
	if len(p.states) != 1 || p.states[0].Group.Name != "Other" || p.selected != "Other" {
		t.Fatal("deleted Available still visible", p.states)
	}
	// With no dependency, deleting the last group clears the default and home.
	test.Tap(p.remove)
	confirmDialog(t, w)
	select {
	case done := <-queue:
		done()
	case <-time.After(25 * time.Second):
		t.Fatal("last group deletion timed out")
	}
	if len(p.states) != 0 || p.defaultPolicy != "" || p.current.Text == "Other  →  DIRECT" || !p.edit.Disabled() {
		t.Fatal("stale default remained after deletion", p.current.Text)
	}
}
