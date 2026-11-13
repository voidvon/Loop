package desktop

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

const importTestLink = "vless://11111111-1111-4111-8111-111111111111@example.com:443?encryption=none&security=tls&sni=example.com&type=ws&path=%2Fproxy#Test%20node"

func TestNodeAddButtonOpensDialogAndRespectsLock(t *testing.T) {
	m, _ := newTestNodeManager(t)
	m.SetLocked(true)
	m.add.OnTapped() // Also guard a stale callback.
	if m.window.Canvas().Overlays().Top() != nil {
		t.Fatal("locked add opened an editor")
	}
	m.SetLocked(false)
	test.Tap(m.add)
	if m.contextMenu != nil || m.window.Canvas().Overlays().Top() == nil {
		t.Fatal("add must open a dialog directly")
	}
}

func TestSourceEditorValidatesKindAndPreservesInputs(t *testing.T) {
	m, _ := newTestNodeManager(t)
	e := newSourceEditor(m)
	e.nodeLink.SetText("trojan://secret@example.com:443")
	test.Tap(e.save)
	if !strings.Contains(e.message.Text, "仅支持") || m.locked {
		t.Fatal("unsupported protocol was accepted")
	}
	e.tabs.SelectIndex(1)
	e.subLink.SetText("https://example.com/sub?token=test")
	test.Tap(e.save)
	if e.message.Text != "请填写订阅名称" {
		t.Fatal("subscription name wasn't validated")
	}
	e.subName.SetText("Test")
	e.subLink.SetText("file:///tmp/sub")
	test.Tap(e.save)
	if !strings.Contains(e.message.Text, "HTTP") || m.locked {
		t.Fatal("non-HTTP subscription accepted")
	}
	e.tabs.SelectIndex(0)
	if e.nodeLink.Text != "trojan://secret@example.com:443" || e.subName.Text != "Test" || e.save.Text != "检查并添加" {
		t.Fatal("switching types lost input")
	}
	e.nodeLink.SetText(importTestLink)
	m.SetLocked(true)
	test.Tap(e.save)
	if !strings.Contains(e.message.Text, "等待") {
		t.Fatal("open editor bypassed a later lock")
	}
	for _, invalid := range []string{importTestLink + "\n" + importTestLink, "vless://missing", "vless://test@example.com:99999"} {
		if _, err := parseVLESSImport(invalid, ""); err == nil {
			t.Fatal("accepted malformed or multiple links")
		}
	}
}

// Keep actual background work, but execute queued UI completions on the test
// thread, matching a real Fyne main loop and avoiding timing-based polling.
type importTestApp struct {
	fyne.App
	driver fyne.Driver
}

func (a importTestApp) Driver() fyne.Driver { return a.driver }

type importTestDriver struct {
	fyne.Driver
	queue chan func()
}

func (d importTestDriver) DoFromGoroutine(fn func(), _ bool) { d.queue <- fn }

func TestSourceEditorAddsUsingRealCoreAndPreservesPolicy(t *testing.T) {
	for _, kind := range []string{"node", "subscription", "failed subscription"} {
		t.Run(kind, func(t *testing.T) {
			s, core := testutil.CoreStore(t)
			a := test.NewApp()
			a.Settings().SetTheme(newClientTheme())
			defer a.Quit()
			queue := make(chan func(), 8)
			fyne.SetCurrentApp(importTestApp{App: a, driver: importTestDriver{Driver: a.Driver(), queue: queue}})
			w := a.NewWindow("add")
			m := newNodeManager(w, app.NewService(app.Options{Store: s, CorePath: core}))
			w.SetContent(m.root)
			w.Resize(fyne.NewSize(860, 620))
			e := newSourceEditor(m)
			e.dialog.Show()
			unchanged := map[string][]byte{}
			for _, name := range []string{profile.MainConfigName, "selection.json"} {
				unchanged[name], _ = os.ReadFile(filepath.Join(s.Dir, name))
			}
			if kind == "node" {
				e.nodeLink.SetText("  " + importTestLink + "  ")
				e.nodeName.SetText("  Custom node  ")
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if kind == "failed subscription" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					fmt.Fprint(w, "proxies:\n  - {name: Test, type: vless, server: example.com, port: 443, uuid: 11111111-1111-4111-8111-111111111111, tls: true}\n")
				}))
				defer server.Close()
				e.tabs.SelectIndex(1)
				e.subName.SetText(" Local subscription ")
				e.subLink.SetText(server.URL + "?token=test")
			}
			busy := false
			m.OnBusy = func(value bool) { busy = value }
			test.Tap(e.save)
			if !busy || !m.locked {
				t.Fatal("import didn't lock competing operations", e.message.Text)
			}
			select {
			case complete := <-queue:
				complete()
			case <-time.After(20 * time.Second):
				t.Fatal("import didn't complete")
			}
			if busy || m.locked {
				t.Fatal("import didn't release the lock")
			}
			sources, err := s.Sources()
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "node":
				if len(sources[0].Nodes) != 2 || sources[0].Nodes[1].Name != "Custom node" || m.sourceIndex != 0 || len(m.nodeItems) != 2 {
					t.Fatal("node not saved and displayed")
				}
				out := sources[0].Nodes[1].Outbound
				if out["type"] != "vless" || out["transport"].(map[string]any)["path"] != "/proxy" || out["tls"].(map[string]any)["server_name"] != "example.com" {
					t.Fatal("node lost transport or TLS fields")
				}
			case "subscription":
				if len(sources) != 2 || sources[1].Name != "Local subscription" || sources[1].Format != "clash" || sources[1].UpdatedAt == "" || m.sourceIndex != 1 || len(m.nodeItems) != 1 {
					t.Fatal("subscription not saved and displayed")
				}
			case "failed subscription":
				if len(sources) != 1 || e.subName.Text != " Local subscription " || e.subLink.Text == "" || w.Canvas().Overlays().Top() == nil {
					t.Fatal("failed request saved data or discarded the form")
				}
			}
			for name, before := range unchanged {
				after, _ := os.ReadFile(filepath.Join(s.Dir, name))
				if !bytes.Equal(before, after) {
					t.Fatalf("adding a source changed %s", name)
				}
			}
		})
	}
}
