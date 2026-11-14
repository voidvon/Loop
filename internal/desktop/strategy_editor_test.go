package desktop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func newTestStrategyEditor(t *testing.T) (*strategyGroupEditor, profile.Store) {
	t.Helper()
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	t.Cleanup(a.Quit)
	w := a.NewWindow("strategy editor")
	s := testutil.NewStore(t)
	for _, id := range []string{"one", "two"} {
		if err := s.SaveSubscription(profile.Subscription{ID: id, Name: id, Format: "clash", Nodes: []profile.Node{{ID: "a", Name: "重复节点", Outbound: map[string]any{"type": "trojan", "server": "example.com", "server_port": 443, "password": "test"}}}}); err != nil {
			t.Fatal(err)
		}
	}
	config := map[string]any{
		"inbounds": []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": 2080}},
		"dns":      map[string]any{"servers": []any{map[string]any{"type": "local", "tag": "dns-direct"}}},
		"route":    map[string]any{"final": "direct", "default_domain_resolver": "dns-direct"},
		"policy_groups": []profile.PolicyGroup{
			{Name: profile.DefaultPolicy, AllNodes: true},
			{Name: "Custom", AllNodes: true, Filter: "(?i)HK", Builtins: []string{"reject", "direct"}, Groups: []string{profile.DefaultPolicy}, Sources: []string{"one"}, Nodes: []string{profile.NodeKey("manual", "a"), profile.NodeKey("one", "a")}},
		},
	}
	if err := profile.WriteJSON(filepath.Join(s.Dir, profile.MainConfigName), config); err != nil {
		t.Fatal(err)
	}
	c := newDocumentController(w, app.NewService(app.Options{Store: s}))
	t.Cleanup(c.Close)
	p := newStrategyPane(w, c, widget.NewLabel(""))
	t.Cleanup(p.Close)
	w.SetContent(p.root)
	w.Resize(fyne.NewSize(1120, 800))
	p.groups.Select(1)
	e, err := newStrategyGroupEditor(p, false)
	if err != nil {
		t.Fatal(err)
	}
	e.dialog.Show()
	return e, s
}

func chooseEditorMember(t *testing.T, e *strategyGroupEditor, id string) {
	t.Helper()
	row := e.chooser.picker.tree.CreateNode(false)
	e.chooser.picker.tree.UpdateNode(id, false, row)
	test.Tap(row.(*fyne.Container).Objects[1].(*widget.Check))
}

func TestStrategyEditorStagesAdditionRemovalAndCancellation(t *testing.T) {
	e, s := newTestStrategyEditor(t)
	before, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	original := cloneStrategyGroup(e.pane.state().Group)
	if len(e.rows) != 7 || e.chooser != nil || !e.name.Disabled() {
		t.Fatal("editor must show configured members before opening a chooser", len(e.rows))
	}
	test.Tap(e.add)
	if e.chooser == nil || !e.chooser.add.Disabled() {
		t.Fatal("add did not open an empty staged chooser")
	}
	chooseEditorMember(t, e, "builtin:reject")
	if len(e.chooser.picker.selected) != 0 {
		t.Fatal("already-added member was selectable")
	}
	newKey := profile.NodeKey("two", "a")
	chooseEditorMember(t, e, "node:"+newKey)
	e.chooser.search.SetText("重复节点")
	if !e.chooser.picker.visible["node:"+newKey] || e.chooser.add.Disabled() {
		t.Fatal("search lost selected member")
	}
	cancelled := e.chooser
	cancelled.dialog.Hide()
	test.Tap(cancelled.add) // A callback captured before cancellation is harmless.
	if !reflect.DeepEqual(e.draft, original) {
		t.Fatal("cancelled chooser changed the editor draft")
	}
	test.Tap(e.add)
	if len(e.chooser.picker.selected) != 0 {
		t.Fatal("cancelled selection leaked into a new chooser")
	}
	chooseEditorMember(t, e, "node:"+newKey)
	test.Tap(e.chooser.add)
	if e.chooser != nil || len(e.draft.Nodes) != 3 || !slices.Contains(e.draft.Nodes, newKey) {
		t.Fatal("confirmed member was not added")
	}
	// Use a recycled row's real removal button; it must target its current item.
	row := e.members.CreateItem().(*infoRow)
	e.members.UpdateItem(0, row)
	e.members.UpdateItem(1, row)
	test.Tap(row.trailingObject.(*widget.Button))
	if slices.Contains(e.draft.Builtins, "reject") || !reflect.DeepEqual(e.pane.state().Group, original) {
		t.Fatal("removal changed the live policy or targeted a stale row")
	}
	after, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	if !bytes.Equal(before, after) {
		t.Fatal("draft changes were persisted before Save")
	}
	e.dialog.Hide()
	if !reflect.DeepEqual(e.pane.state().Group, original) {
		t.Fatal("cancelled editor mutated policy arrays")
	}
}

func TestStrategyEditorSavePersistsDraftAndKeepsItOnFailure(t *testing.T) {
	if _, err := exec.LookPath("sing-box"); err != nil {
		t.Skip("sing-box is not installed")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "failure"}[fail], func(t *testing.T) {
			e, s := newTestStrategyEditor(t)
			a := fyne.CurrentApp()
			queue := make(chan func(), 8)
			fyne.SetCurrentApp(importTestApp{App: a, driver: importTestDriver{Driver: a.Driver(), queue: queue}})
			c := e.pane.controller
			before, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
			e.removeMember("builtin", "reject")
			c.SavePolicies = func(ctx context.Context, data, expected []byte, _ bool) (app.DocumentSaveResult, error) {
				if fail {
					return app.DocumentSaveResult{}, errors.New("synthetic save failure")
				}
				return c.service.SavePolicies(ctx, nil, data, expected, false)
			}
			test.Tap(e.save)
			if !e.busy || !e.save.Disabled() {
				t.Fatal("save was not locked while pending")
			}
			select {
			case done := <-queue:
				done()
			case <-time.After(25 * time.Second):
				t.Fatal("save did not complete")
			}
			after, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
			if bytes.Equal(before, after) != fail || e.closed == fail || e.busy {
				t.Fatal("wrong save outcome", e.message.Text)
			}
			if fail && (e.save.Disabled() || !e.message.Visible() || slices.Contains(e.draft.Builtins, "reject")) {
				t.Fatal("failed save lost draft or prevented retry")
			}
			if !fail {
				config, err := s.Config()
				if err != nil {
					t.Fatal(err)
				}
				groups, err := profile.PolicyGroups(config)
				if err != nil || len(groups[1].Builtins) != 1 || groups[1].Builtins[0] != "direct" || !groups[1].AllNodes || groups[1].Filter != "(?i)HK" {
					t.Fatal("saved member edit lost other settings", err)
				}
			}
		})
	}
}
