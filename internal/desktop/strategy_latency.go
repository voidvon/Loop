package desktop

import (
	"context"
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/proxy"
)

type probeDisplay struct {
	Pending bool
	Result  proxy.LatencyResult
}

var latencyIcon = iconSpeed

func (p *strategyPane) startLatency() {
	if p.probing {
		if p.cancelProbe != nil {
			p.cancelRequested = true
			p.cancelProbe()
			p.updateButtons()
		}
		return
	}
	if !p.canSelect() || p.state() == nil || p.closed.Load() {
		return
	}
	group := p.selected
	targets, err := p.controller.service.StrategyProbeTargets(group)
	if err != nil {
		dialog.ShowError(err, p.window)
		return
	}
	if err := p.controller.service.CoreError(); err != nil {
		dialog.ShowError(err, p.window)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelProbe, p.probing, p.cancelRequested = cancel, true, false
	p.probeGroup, p.probeComplete, p.probeTotal = group, 0, len(targets)
	p.probeSummary = ""
	for _, target := range targets {
		p.latencies[target.Key] = probeDisplay{Pending: true}
	}
	p.probeDone = make(chan struct{})
	done := p.probeDone
	if p.OnBusy != nil {
		p.OnBusy(true)
	}
	p.members.Refresh()
	p.updateButtons()
	go func() {
		defer close(done)
		err := p.controller.service.MeasureLatencies(ctx, targets, func(key string, result proxy.LatencyResult) {
			if p.closed.Load() {
				return
			}
			fyne.Do(func() {
				if p.closed.Load() {
					return
				}
				p.latencies[key] = probeDisplay{Result: result}
				p.probeComplete++
				p.members.Refresh()
				p.updateButtons()
			})
		})
		cancel()
		if !p.closed.Load() {
			fyne.Do(func() { p.finishLatency(err) })
		}
	}()
}
func (p *strategyPane) finishLatency(err error) {
	if p.closed.Load() {
		return
	}
	p.probing, p.cancelProbe = false, nil
	for key, value := range p.latencies {
		if value.Pending {
			delete(p.latencies, key)
		}
	}
	p.probeSummary = fmt.Sprintf("%s · 延迟测速完成 %d / %d · 单位 ms", p.probeGroup, p.probeComplete, p.probeTotal)
	if errors.Is(err, context.Canceled) {
		p.probeSummary = fmt.Sprintf("%s · 测速已取消 · 完成 %d / %d", p.probeGroup, p.probeComplete, p.probeTotal)
	} else if err != nil {
		p.probeSummary = "测速失败：" + err.Error()
	}
	if p.OnBusy != nil {
		p.OnBusy(false)
	}
	p.members.Refresh()
	p.updateButtons()
}
func (p *strategyPane) latencyKey(key string) string {
	for range len(p.states) + 1 {
		child := false
		for _, state := range p.states {
			if state.Group.Name == key {
				if state.Error != "" {
					return ""
				}
				key = state.Selected
				child = true
				break
			}
		}
		if !child {
			return key
		}
	}
	return ""
}
func (p *strategyPane) latencyLabel(key string) (string, widget.Importance) {
	value, ok := p.latencies[p.latencyKey(key)]
	if !ok {
		return "", widget.LowImportance
	}
	if value.Pending {
		return "…", widget.LowImportance
	}
	if errors.Is(value.Result.Err, context.DeadlineExceeded) {
		return "超时", widget.WarningImportance
	}
	if errors.Is(value.Result.Err, context.Canceled) {
		return "已取消", widget.LowImportance
	}
	if value.Result.Err != nil {
		return "失败", widget.WarningImportance
	}
	return fmt.Sprintf("%d ms", max(1, value.Result.Duration.Milliseconds())), widget.SuccessImportance
}

// Close runs on the UI thread; cancellation kills and reaps the isolated core
// before the application exits, including Cmd+Q which bypasses close intercepts.
func (p *strategyPane) Close() {
	p.closed.Store(true)
	if p.cancelProbe != nil {
		p.cancelProbe()
	}
	if p.probeDone != nil {
		<-p.probeDone
	}
}

// Kept local to this view so the window lifecycle does not know test details.
func (p *strategyPane) probeStatus() string {
	if p.cancelRequested {
		return "正在取消测速…"
	}
	return fmt.Sprintf("%s · 正在测速 %d / %d · 再次点击测速图标可取消", p.probeGroup, p.probeComplete, p.probeTotal)
}
