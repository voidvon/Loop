package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/proxy"
	"local/loop/internal/testutil"
)

func TestStrategyLatencyLabelsAndCancellationControls(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("latency")
	c := newDocumentController(w, app.NewService(app.Options{Store: testutil.NewStore(t)}))
	p := newStrategyPane(w, c, widget.NewLabel(""))
	defer p.Close()
	w.SetContent(p.root)
	for _, button := range []*widget.Button{p.speed, p.edit, p.remove} {
		if button.Text != "" || button.Icon == nil {
			t.Fatal("strategy actions must use icons")
		}
	}
	key := profile.NodeKey("manual", "a")
	for _, tc := range []struct {
		value      probeDisplay
		text       string
		importance widget.Importance
	}{
		{probeDisplay{Pending: true}, "…", widget.LowImportance},
		{probeDisplay{Result: proxy.LatencyResult{Duration: 68 * time.Millisecond}}, "68 ms", widget.SuccessImportance},
		{probeDisplay{Result: proxy.LatencyResult{Duration: time.Microsecond}}, "1 ms", widget.SuccessImportance},
		{probeDisplay{Result: proxy.LatencyResult{Err: context.DeadlineExceeded}}, "超时", widget.WarningImportance},
		{probeDisplay{Result: proxy.LatencyResult{Err: context.Canceled}}, "已取消", widget.LowImportance},
		{probeDisplay{Result: proxy.LatencyResult{Err: errors.New("failure")}}, "失败", widget.WarningImportance},
	} {
		p.latencies[key] = tc.value
		// YouTube -> Available -> the same selected leaf node.
		for _, ref := range []string{key, "Available", "YouTube"} {
			text, importance := p.latencyLabel(ref)
			if text != tc.text || importance != tc.importance {
				t.Fatalf("%s: got %s / %v", ref, text, importance)
			}
		}
	}
	p.latencies[key] = probeDisplay{Pending: true}
	p.probing, p.available = true, false // Global busy lock must still allow cancel.
	p.probeGroup, p.probeTotal = "Available", 1
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelProbe = cancel
	p.updateButtons()
	if p.speed.Disabled() || !p.edit.Disabled() || !p.add.Disabled() || p.canSelect() {
		t.Fatal("measurement lock disabled cancellation or allowed mutations")
	}
	test.Tap(p.speed)
	if ctx.Err() == nil || !p.cancelRequested || !p.speed.Disabled() {
		t.Fatal("second tap did not cancel")
	}
	busyReleased := false
	p.OnBusy = func(value bool) { busyReleased = !value; p.available = !value }
	p.finishLatency(context.Canceled)
	if !busyReleased || p.probing || p.speed.Disabled() || !p.canSelect() || !strings.Contains(p.status.Text, "测速已取消") {
		t.Fatal("measurement didn't release the UI lock")
	}
	if _, ok := p.latencies[key]; ok {
		t.Fatal("unfinished result left pending")
	}
	p.Close()
	p.finishLatency(nil)
	if p.canSelect() {
		t.Fatal("closed pane allowed actions")
	}
}
