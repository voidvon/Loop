package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	prettyview "github.com/ideaconnect/go-fyne-pretty-view/v2"
	"local/miniproxy/internal/app"
	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/testutil"
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
