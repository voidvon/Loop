package desktop

import (
	"context"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"local/loop/internal/app"
	"local/loop/internal/proxy"
)

func TestBenchmarkPaneParametersMultiSelectionProgressAndCancellation(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	more := newMorePane(a.Preferences())
	more.categories.Select(moreBenchmark)
	p := more.benchmark
	defer p.Close()
	if more.body.Objects[0] != p.root {
		t.Fatal("missing benchmark category")
	}
	options, err := p.options()
	if err != nil || options.Requests != 200 || options.Concurrency != 10 || options.Timeout != 20*time.Second {
		t.Fatal(options, err)
	}
	p.requests.SetText("invalid")
	if _, err := p.options(); err == nil {
		t.Fatal("invalid count accepted")
	}
	p.duration.SetText("1s")
	if options, err := p.options(); err != nil || options.Duration != time.Second {
		t.Fatal("duration did not override count", options, err)
	}
	p.requests.SetText("20")
	p.concurrency.SetText("2")
	p.duration.SetText("")
	p.loadNodes = func() ([]app.BenchmarkNode, error) {
		return []app.BenchmarkNode{{Key: "a", Name: "A", Source: "Local"}, {Key: "b", Name: "B", Source: "Subscription"}}, nil
	}
	p.Refresh()
	p.selected["b"] = true
	p.mode.SetSelected(benchmarkSelected)
	posted := make(chan func(), 8)
	p.post = func(callback func()) { posted <- callback }
	entered := make(chan app.BenchmarkRequest, 1)
	p.run = func(ctx context.Context, request app.BenchmarkRequest, _ func(proxy.BenchmarkProgress)) (proxy.BenchmarkReport, error) {
		entered <- request
		<-ctx.Done()
		return proxy.BenchmarkReport{}, ctx.Err()
	}
	p.OnBusy = func(value bool) { p.SetAvailable(!value) }
	p.start()
	request := <-entered
	if len(request.NodeKeys) != 1 || request.NodeKeys[0] != "b" || request.Options.Requests != 20 || p.startButton.Disabled() || !p.url.Disabled() || !p.picker.Disabled() {
		t.Fatal("selection or cancellation controls incorrect", request)
	}
	p.ApplyProgress(&proxy.BenchmarkProgress{ID: "old-job", Result: proxy.BenchmarkResult{Key: "b", Completed: 19}})
	if p.results["b"].Completed != 0 {
		t.Fatal("stale progress accepted")
	}
	p.ApplyProgress(&proxy.BenchmarkProgress{ID: request.ID, Index: 0, Total: 1, Result: proxy.BenchmarkResult{Key: "b", Name: "B", Completed: 10, Planned: 20, Success: 9, Average: 50 * time.Millisecond, P95: 80 * time.Millisecond, RequestsPerSecond: 5}})
	if p.cell(0, 2) != "90.0%" || p.cell(0, 3) != "50.0" || p.progress.Value != .5 {
		t.Fatal("incorrect progress or statistics", p.results["b"])
	}
	p.start()
	callback := <-posted
	<-p.done
	callback()
	if p.probing || !p.available || !strings.Contains(p.status.Text, "已取消") || p.results["b"].Completed != 10 {
		t.Fatal("cancellation lost progress or lock")
	}
	p.ApplyProgress(&proxy.BenchmarkProgress{ID: request.ID, Result: proxy.BenchmarkResult{Key: "b", Name: "B", Completed: 11, Success: 9, CancelledRequests: 1, Finished: true}})
	if p.results["b"].Completed != 11 || !strings.Contains(p.status.Text, "已取消") {
		t.Fatal("terminal daemon progress not accepted")
	}
	p.ApplyProgress(&proxy.BenchmarkProgress{ID: request.ID, Result: proxy.BenchmarkResult{Key: "b", Completed: 10}})
	if p.results["b"].Completed != 11 || !p.results["b"].Finished {
		t.Fatal("late partial snapshot overwrote final result")
	}
	p.Close()
	p.ApplyProgress(&proxy.BenchmarkProgress{ID: request.ID, Result: proxy.BenchmarkResult{Key: "b", Completed: 99}})
	if p.results["b"].Completed == 99 {
		t.Fatal("closed pane updated")
	}
}

func TestBenchmarkPaneCloseCancelsWorker(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	p := newBenchmarkPane()
	entered := make(chan context.Context, 1)
	p.run = func(ctx context.Context, _ app.BenchmarkRequest, _ func(proxy.BenchmarkProgress)) (proxy.BenchmarkReport, error) {
		entered <- ctx
		<-ctx.Done()
		return proxy.BenchmarkReport{}, ctx.Err()
	}
	p.start()
	ctx := <-entered
	p.Close()
	if ctx.Err() != context.Canceled {
		t.Fatal("close did not cancel benchmark")
	}
}
