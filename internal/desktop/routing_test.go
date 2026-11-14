package desktop

import (
	"context"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"local/loop/internal/proxy"
)

func TestRoutingPaneSubmitCancelResultsAndClose(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	more := newMorePane(a.Preferences())
	more.categories.Select(moreRouting)
	p := more.routing
	defer p.Close()
	if more.body.Objects[0] != p.root {
		t.Fatal("routing category is not connected")
	}
	posted := make(chan func(), 1)
	p.post = func(callback func()) { posted <- callback }
	entered := make(chan string, 1)
	p.runTest = func(ctx context.Context, value string) (proxy.RoutingResult, error) {
		entered <- value
		<-ctx.Done()
		return proxy.RoutingResult{}, ctx.Err()
	}
	p.OnBusy = func(value bool) { p.SetAvailable(!value) }
	p.url.SetText("file:///etc/passwd")
	p.start()
	if p.probing || !strings.Contains(p.status.Text, "有效") {
		t.Fatal("invalid URL started a test")
	}
	p.url.SetText("example.com")
	p.url.OnSubmitted(p.url.Text)
	if value := <-entered; value != "https://example.com" {
		t.Fatal(value)
	}
	if !p.probing || !p.url.Disabled() || p.test.Disabled() {
		t.Fatal("global lock disabled cancel or allowed edits")
	}
	test.Tap(p.test)
	if !p.cancelling || !p.test.Disabled() {
		t.Fatal("cancellation not reflected")
	}
	callback := <-posted
	<-p.done
	callback()
	if p.probing || !p.available || p.results.Visible() || p.status.Text != "测试已取消" {
		t.Fatal("cancellation did not release busy state")
	}
	p.runTest = func(context.Context, string) (proxy.RoutingResult, error) {
		return proxy.RoutingResult{
			Mode: "自动分流", Rule: "FINAL", Node: "US Test", Source: "测试订阅", Chain: []string{"Available", "US Test"}, Duration: 83 * time.Millisecond, StatusCode: 403,
		}, nil
	}
	p.start()
	callback = <-posted
	<-p.done
	callback()
	if !p.results.Visible() || p.node.Text != "US Test" || p.latency.Text != "83 ms" || p.response.Text != "HTTP 403" || p.chain.Text != "Available → US Test" {
		t.Fatal("routing result not shown")
	}
	// A failed request must show its known route without reusing old latency.
	p.finish(proxy.RoutingResult{Mode: "自动分流", Node: "REJECT · 拒绝", Rule: "domain=test", Error: "已被分流规则拒绝"}, nil)
	if p.latency.Text != "—" || p.response.Text != "—" || p.node.Text != "REJECT · 拒绝" {
		t.Fatal("stale success fields after rejection")
	}
	p.Close()
	p.finish(proxy.RoutingResult{Node: "late result"}, nil)
	if p.node.Text == "late result" {
		t.Fatal("late callback updated closed UI")
	}
}

func TestClosingRoutingPaneCancelsWorkerAndIgnoresQueuedCallback(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	p := newRoutingPane()
	posted := make(chan func(), 1)
	p.post = func(callback func()) { posted <- callback }
	entered, release := make(chan struct{}), make(chan struct{})
	p.runTest = func(ctx context.Context, _ string) (proxy.RoutingResult, error) {
		close(entered)
		<-release
		return proxy.RoutingResult{Node: "late"}, ctx.Err()
	}
	p.url.SetText("example.com")
	p.start()
	<-entered
	close(release)
	callback := <-posted
	<-p.done
	p.Close()
	callback()
	if p.results.Visible() {
		t.Fatal("queued result applied after closing")
	}
	// A blocked network request must finish before Close returns.
	p = newRoutingPane()
	var workerContext context.Context
	entered = make(chan struct{})
	p.runTest = func(ctx context.Context, _ string) (proxy.RoutingResult, error) {
		workerContext = ctx
		close(entered)
		<-ctx.Done()
		return proxy.RoutingResult{}, ctx.Err()
	}
	p.url.SetText("example.com")
	p.start()
	<-entered
	p.Close()
	if workerContext.Err() != context.Canceled {
		t.Fatal("closing did not cancel worker")
	}
}
