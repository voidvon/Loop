package desktop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	prettyview "github.com/ideaconnect/go-fyne-pretty-view/v2"
	"local/miniproxy/internal/app"
	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/testutil"
)

type rulesApplyTestBackend struct {
	starts, stops int
	failNext      bool
}

func (b *rulesApplyTestBackend) Start(context.Context) error {
	b.starts++
	if b.failNext {
		b.failNext = false
		return errors.New("new config failed")
	}
	return nil
}
func (b *rulesApplyTestBackend) Stop() error { b.stops++; return nil }

func TestRulesCanSaveOnlineAndSynchronizeConnectionAfterRestart(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "applied", true: "rolled back"}[fail], func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			a := test.NewApp()
			a.Settings().SetTheme(newClientTheme())
			defer a.Quit()
			queue := make(chan func(), 8)
			fyne.SetCurrentApp(importTestApp{App: a, driver: importTestDriver{Driver: a.Driver(), queue: queue}})
			w := a.NewWindow("online rules")
			service := app.NewService(app.Options{Store: s, CorePath: core})
			running := true
			shell := newDesktopShell(w, service, nil, widget.NewLabel("已连接"), widget.NewButton("停止", nil), func() bool { return running }, func(profile.Mode) {}, func(bool) {})
			defer shell.documents.Close()
			defer shell.strategies.Close()
			c, p := shell.documents, shell.rulesPane
			backend := &rulesApplyTestBackend{failNext: fail}
			c.Running = func() bool { return running }
			c.SaveRules = func(ctx context.Context, data, expected []byte, connected bool) (app.RuleSaveResult, error) {
				return service.SaveRules(ctx, backend, data, expected, connected)
			}
			c.OnRulesApplied = func(result app.RuleSaveResult) { running = result.Running }
			busy := false
			c.OnBusy = func(value bool) { busy = value; shell.SetModeEnabled(!value) }
			w.SetContent(shell.root)
			w.Resize(fyne.NewSize(1120, 760))
			shell.SetModeEnabled(true)
			if p.add.Disabled() || !shell.strategies.edit.Disabled() || c.available {
				t.Fatal("online rules must be editable while full config/group editing stays locked")
			}
			before := append([]byte(nil), p.baseline...)
			rule, _ := profile.BuildBasicRule("domain_suffix", "example.org", "direct")
			var operationErr error
			completed := false
			p.commit(profile.RuleChange{Operation: "add", Rule: rule}, before, func(err error) { completed = true; operationErr = err })
			if !busy || !p.add.Disabled() || shell.strategies.canSelect() {
				t.Fatal("online save didn't lock other operations")
			}
			select {
			case complete := <-queue:
				complete()
			case <-time.After(25 * time.Second):
				t.Fatal("online save didn't finish")
			}
			if !completed || (operationErr != nil) != fail || !running || busy || c.working || p.add.Disabled() {
				t.Fatal("wrong completion/connection state", operationErr, running, busy)
			}
			if backend.starts != map[bool]int{false: 1, true: 2}[fail] {
				t.Fatal("missing automatic reconnect", backend.starts)
			}
			after, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
			if !bytes.Equal(p.baseline, after) || !bytes.Equal(shell.configPane.document.baseline, after) || bytes.Equal(before, after) != fail {
				t.Fatal("rule list, editor and persisted config diverged")
			}
			if !fail && !strings.Contains(p.status.Text, "代理已连接") {
				t.Fatal("success didn't report connected state", p.status.Text)
			}
			shell.configPane.document.view.SetInputConfig(prettyview.InputConfig{DebounceFor: -1})
			a.Clipboard().SetContent(`{"route":{"final":"direct"}}`)
			shell.configPane.document.view.SelectAll()
			shell.configPane.document.view.Paste()
			starts := backend.starts
			p.commit(profile.RuleChange{Operation: "add", Rule: rule}, after, func(err error) { operationErr = err })
			if operationErr == nil || backend.starts != starts || !running {
				t.Fatal("draft protection failed during online editing")
			}
		})
	}
}

func TestRuleFormsShareCoreValidationAndProtectEditorDrafts(t *testing.T) {
	s, core := testutil.CoreStore(t)
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	w := a.NewWindow("rules")
	service := app.NewService(app.Options{Store: s, CorePath: core})
	controller := newDocumentController(w, service)
	editor := newConfigPane(w, controller)
	defer editor.Close()
	editor.document.view.SetInputConfig(prettyview.InputConfig{DebounceFor: -1})
	pane := newRulesPane(w, controller)
	controller.OnStateChanged = pane.updateButtons
	w.SetContent(pane.root)
	original, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	unchanged := map[string][]byte{}
	for _, name := range []string{"nodes.json", "subscriptions.json", "selection.json"} {
		unchanged[name], _ = os.ReadFile(filepath.Join(s.Dir, name))
	}
	commit := func(change profile.RuleChange, baseline []byte) error {
		t.Helper()
		completed := make(chan error, 1)
		pane.commit(change, baseline, func(err error) { completed <- err })
		select {
		case err := <-completed:
			return err
		case <-time.After(25 * time.Second):
			t.Fatal("rule operation did not complete")
			return nil
		}
	}
	if err := commit(profile.RuleChange{Operation: "final", Final: "direct"}, original); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	backup, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName+".bak"))
	if !bytes.Equal(backup, original) || !bytes.Equal(editor.document.baseline, saved) || editor.HasDraft() {
		t.Fatal("rule form and JSON editor diverged or backup was lost")
	}
	// A dialog opened against old contents cannot revert a newer saved edit.
	if err := commit(profile.RuleChange{Operation: "final", Final: "proxy"}, original); err == nil {
		t.Fatal("stale rule form overwrote newer configuration")
	}
	for _, tc := range []struct{ kind, value, target string }{
		{"domain", "example.com", "proxy"},
		{"domain_suffix", "example.org", "direct"},
		{"domain_keyword", "ads", "reject"},
		{"ip_cidr", "192.0.2.1\n2001:db8::/32", "proxy"},
		{"ip_is_private", "", "direct"},
	} {
		rule, err := profile.BuildBasicRule(tc.kind, tc.value, tc.target)
		if err != nil {
			t.Fatal(err)
		}
		draft, err := profile.ChangeRouting(saved, profile.RuleChange{Operation: "add", Rule: rule})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.ValidateDocument(context.Background(), profile.MainConfigName, draft); err != nil {
			t.Fatalf("core rejected %s form: %v", tc.kind, err)
		}
	}
	pane.SetAvailable(false)
	if err := commit(profile.RuleChange{Operation: "final", Final: "proxy"}, saved); err == nil || !pane.add.Disabled() {
		t.Fatal("connected/locked rule form allowed a write")
	}
	pane.SetAvailable(true)
	controller.SetAvailable(false)
	if err := commit(profile.RuleChange{Operation: "final", Final: "proxy"}, saved); err == nil || !pane.add.Disabled() {
		t.Fatal("shared controller lock allowed a rule write")
	}
	controller.SetAvailable(true)
	a.Clipboard().SetContent(`{"route":{"final":"proxy"}}`)
	editor.document.view.SelectAll()
	editor.document.view.Paste()
	pane.Refresh()
	if err := commit(profile.RuleChange{Operation: "final", Final: "proxy"}, saved); err == nil || !pane.add.Disabled() {
		t.Fatal("rule form discarded an unsaved JSON draft")
	}
	got, _ := os.ReadFile(filepath.Join(s.Dir, profile.MainConfigName))
	if !bytes.Equal(got, saved) {
		t.Fatal("rejected rule operation changed disk contents")
	}
	for name, want := range unchanged {
		got, _ := os.ReadFile(filepath.Join(s.Dir, name))
		if !bytes.Equal(got, want) {
			t.Fatal("rule form changed unrelated profile", name)
		}
	}
}
