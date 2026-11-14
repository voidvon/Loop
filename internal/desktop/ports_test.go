package desktop

import (
	"bytes"
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"local/loop/internal/app"
	"local/loop/internal/inbound"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestPortInputsAutosaveAndRestoreFailedApplications(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "rollback"}[fail], func(t *testing.T) {
			store, core := testutil.CoreStore(t)
			a := test.NewApp()
			defer a.Quit()
			a.Settings().SetTheme(newClientTheme())
			queue := make(chan func(), 8)
			fyne.SetCurrentApp(importTestApp{App: a, driver: importTestDriver{Driver: a.Driver(), queue: queue}})
			w := a.NewWindow("proxy ports")
			service := app.NewService(app.Options{Store: store, CorePath: core})
			c := newDocumentController(w, service)
			defer c.Close()
			editor := newConfigPane(w, c)
			editor.Open()
			p := newPortsPane(c)
			c.OnStateChanged = p.updateButtons
			calls := 0
			running := true
			backend := &rulesApplyTestBackend{failNext: fail}
			c.Running = func() bool { return running }
			c.OnApplied = func(result app.DocumentSaveResult) { running = result.Running }
			c.SavePorts = func(ctx context.Context, data, expected []byte, connected bool) (app.DocumentSaveResult, error) {
				calls++
				return service.SaveProxyPorts(ctx, backend, data, expected, connected)
			}
			w.SetContent(p.root)
			p.Refresh()
			before := append([]byte(nil), p.baseline...)
			p.Refresh()
			p.defaultPort.FocusLost()
			if calls != 0 {
				t.Fatal("refresh triggered a save")
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			p.defaultPort.SetText(strconv.Itoa(port))
			p.Refresh()
			if c.working || calls != 0 || p.defaultPort.Text != strconv.Itoa(port) {
				t.Fatal("typing/refresh saved or discarded the draft")
			}
			p.defaultPort.FocusLost()
			if !c.working || !p.defaultPort.Disabled() || !p.globalEnabled.Disabled() {
				t.Fatal("form was not locked during save")
			}
			p.commit()
			select {
			case done := <-queue:
				done()
			case <-time.After(25 * time.Second):
				t.Fatal("save timed out")
			}
			actual, err := store.ProxyPorts()
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || c.working || !running || !p.globalEnabled.Checked || !actual.GlobalEnabled || p.defaultPort.Disabled() {
				t.Fatal(calls, actual, running)
			}
			want := port
			if fail {
				want = 2080
			}
			if actual.Default != want || p.defaultPort.Text != strconv.Itoa(want) {
				t.Fatal("input did not reflect saved port", actual.Default, p.defaultPort.Text)
			}
			after, _ := os.ReadFile(c.path)
			if !bytes.Equal(after, p.baseline) || !bytes.Equal(editor.document.baseline, after) || bytes.Equal(before, after) != fail {
				t.Fatal("UI/editor did not track saved or rolled back document")
			}
			if fail && !strings.Contains(p.status.Text, "保存失败") {
				t.Fatal(p.status.Text)
			}
		})
	}
}

func TestPortEntryCommitValidationAndCopy(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	w := a.NewWindow("ports validation")
	c := newDocumentController(w, app.NewService(app.Options{Store: testutil.NewStore(t)}))
	defer c.Close()
	p := newPortsPane(c)
	p.Refresh()
	before := append([]byte(nil), p.baseline...)
	for _, input := range []string{"", "abc", "80", "65536", "2081"} {
		p.defaultPort.SetText(input)
		p.defaultPort.FocusLost()
		if c.working || !bytes.Equal(before, p.baseline) {
			t.Fatal("invalid input was saved", input)
		}
	}
	p.defaultPort.SetText("2080")
	test.Tap(p.copyButtons[0])
	if a.Clipboard().Content() != "http://127.0.0.1:2080" {
		t.Fatal(a.Clipboard().Content())
	}
	test.Tap(p.copyButtons[1])
	if a.Clipboard().Content() != "socks5://127.0.0.1:2080" {
		t.Fatal(a.Clipboard().Content())
	}
	ports := inbound.DefaultPorts()
	draft, err := profile.ChangeProxyPorts(before, ports)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.path, draft, 0600); err != nil {
		t.Fatal(err)
	}
	p.Refresh()
	test.Tap(p.copyButtons[2])
	if a.Clipboard().Content() != "http://127.0.0.1:2081" {
		t.Fatal(a.Clipboard().Content())
	}
	c.HasDraft = func() bool { return true }
	p.updateButtons()
	if !p.defaultPort.Disabled() || !p.globalEnabled.Disabled() {
		t.Fatal("unsaved JSON draft did not lock port edits")
	}
}
