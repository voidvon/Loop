package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	prettyview "github.com/ideaconnect/go-fyne-pretty-view/v2"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestConfigPanePreservesDraftAcrossNavigationAndRefresh(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	w := a.NewWindow("configuration")
	s := testutil.NewStore(t)
	shell := newDesktopShell(w, app.NewService(app.Options{Store: s}), nil, widget.NewLabel("未连接"), widget.NewButton("启动", nil), func() bool { return false }, func(profile.Mode) {}, func(bool) {})
	w.SetContent(shell.root)
	shell.selectPage(pageConfig)
	p := shell.configPane
	defer p.Close()
	doc := p.document
	// Fyne's test driver executes Do inline; settle synchronously so a timer
	// cannot run UI callbacks concurrently with the test goroutine.
	doc.view.SetInputConfig(prettyview.InputConfig{DebounceFor: -1})
	draft := `{"route":{"final":"direct"}}`
	a.Clipboard().SetContent(draft)
	doc.view.SelectAll()
	doc.view.Paste()
	if !doc.dirty() {
		t.Fatal("edit was not tracked")
	}
	shell.selectPage(pageHome)
	shell.selectPage(pageConfig)
	p.Refresh()
	if !bytes.Equal(doc.view.Source(), []byte(draft)) {
		t.Fatal("page navigation discarded draft")
	}
	p.SetAvailable(false)
	if !p.save.Disabled() || !p.check.Disabled() {
		t.Fatal("running/locked state allows disk changes")
	}
	source, _ := os.ReadFile(filepath.Join(s.Dir, "config.json"))
	if bytes.Equal(source, []byte(draft)) {
		t.Fatal("draft was saved without explicit save")
	}
	if _, err := profile.ParseDocument(doc.view.Source()); err != nil {
		t.Fatal(err)
	}
}

func TestEditorLoadsOnFirstVisitAndReadsLatestRules(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("lazy editor")
	s := testutil.NewStore(t)
	c := newDocumentController(w, app.NewService(app.Options{Store: s}))
	p := newConfigPane(w, c)
	defer p.Close()
	p.Refresh()
	p.updateEditorPalette()
	p.SetAvailable(false)
	if p.document.view != nil || c.dirty() {
		t.Fatal("background refresh constructed an unused editor")
	}
	// The rule page can change the document without opening the editor.
	latest := []byte(`{"route":{"final":"direct"}}`)
	if err := os.WriteFile(p.document.path, latest, 0600); err != nil {
		t.Fatal(err)
	}
	c.OnSaved(latest)
	if p.document.view != nil {
		t.Fatal("saving rules constructed an unused editor")
	}
	p.Open()
	if !bytes.Equal(p.document.baseline, latest) || p.HasDraft() || !p.save.Disabled() {
		t.Fatal("first visit did not read the latest document/lock state")
	}
}

func TestUnchangedDocumentRefreshPreservesCaretAndRedo(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("editor refresh")
	c := newDocumentController(w, app.NewService(app.Options{Store: testutil.NewStore(t)}))
	p := newConfigPane(w, c)
	defer p.Close()
	p.Open()
	editor := p.document.view
	editor.SetInputConfig(prettyview.InputConfig{DebounceFor: -1})
	original := append([]byte(nil), editor.Source()...)
	draft := `{"route":{"final":"direct"}}`
	a.Clipboard().SetContent(draft)
	editor.SelectAll()
	editor.Paste()
	editor.Undo()
	if p.HasDraft() || !bytes.Equal(editor.Source(), original) {
		t.Fatal("undo did not restore the saved document")
	}
	editor.SetCaret(1, 2)
	line, column := editor.Caret()
	p.Refresh()
	if gotLine, gotColumn := editor.Caret(); gotLine != line || gotColumn != column {
		t.Fatal("refresh reset the caret in an unchanged document")
	}
	editor.Redo()
	if !bytes.Equal(editor.Source(), []byte(draft)) || !p.HasDraft() {
		t.Fatal("refresh discarded the redo history")
	}
}
