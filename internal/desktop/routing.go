package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/proxy"
)

type routingPane struct {
	root                                               fyne.CanvasObject
	url                                                *widget.Entry
	test                                               *widget.Button
	status                                             *widget.Label
	results                                            *fyne.Container
	mode, rule, chain, node, source, latency, response *widget.Label
	runTest                                            func(context.Context, string) (proxy.RoutingResult, error)
	post                                               func(func())
	OnBusy                                             func(bool)
	available, probing, cancelling                     bool
	cancel                                             context.CancelFunc
	done                                               chan struct{}
	closed                                             atomic.Bool
}

func newRoutingPane() *routingPane {
	p := &routingPane{post: fyne.Do, available: true}
	p.url = widget.NewEntry()
	p.url.SetPlaceHolder("输入网址，例如 https://example.com")
	p.url.OnSubmitted = func(string) {
		if !p.probing {
			p.start()
		}
	}
	p.test = widget.NewButtonWithIcon("开始测试", theme.MediaPlayIcon(), p.start)
	p.test.Importance = widget.HighImportance
	p.status = quietLabel("使用已保存的配置、当前模式和策略选择；代理未开启时也可测试。")
	p.status.Wrapping = fyne.TextWrapWord
	p.mode, p.rule, p.chain, p.node = wrappedLabel(""), wrappedLabel(""), wrappedLabel(""), wrappedLabel("")
	p.source, p.latency, p.response = wrappedLabel(""), wrappedLabel(""), wrappedLabel("")
	p.results = container.NewVBox(panel(inset(8, 8, 8, 8, widget.NewForm(
		widget.NewFormItem("模式", p.mode),
		widget.NewFormItem("命中规则", p.rule),
		widget.NewFormItem("策略路径", p.chain),
		widget.NewFormItem("使用节点", p.node),
		widget.NewFormItem("节点来源", p.source),
		widget.NewFormItem("请求延迟", p.latency),
		widget.NewFormItem("响应", p.response),
	))))
	p.results.Hide()
	help := quietLabel("延迟包含连接与 TLS 握手，计时至响应头；重定向仅测试第一跳，不模拟应用规则。")
	help.Wrapping = fyne.TextWrapWord
	p.root = container.NewVScroll(container.NewVBox(
		sectionTitle("分流测试"),
		panel(inset(8, 8, 8, 8, container.NewVBox(
			container.NewBorder(nil, nil, nil, p.test, p.url), p.status,
		))), p.results, help,
	))
	return p
}

func (p *routingPane) SetAvailable(value bool) {
	p.available = value
	p.updateButtons()
}

func (p *routingPane) updateButtons() {
	if p.probing {
		p.test.SetText("取消测试")
		p.test.SetIcon(theme.CancelIcon())
		setEnabled(p.test, !p.cancelling)
	} else {
		p.test.SetText("开始测试")
		p.test.SetIcon(theme.MediaPlayIcon())
		setEnabled(p.test, p.available && p.runTest != nil)
	}
	setEnabled(p.url, !p.probing)
}

func (p *routingPane) start() {
	if p.closed.Load() {
		return
	}
	if p.probing {
		if p.cancel != nil {
			p.cancelling = true
			p.status.SetText("正在取消…")
			p.cancel()
			p.updateButtons()
		}
		return
	}
	if !p.available || p.runTest == nil {
		return
	}
	value, err := proxy.NormalizeRoutingURL(p.url.Text)
	if err != nil {
		p.status.SetText(err.Error())
		p.results.Hide()
		return
	}
	p.url.SetText(value)
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.probing, p.cancelling = cancel, true, false
	p.done = make(chan struct{})
	done := p.done
	p.results.Hide()
	p.status.SetText("正在测试…")
	if p.OnBusy != nil {
		p.OnBusy(true)
	}
	p.updateButtons()
	go func() {
		defer close(done)
		result, err := p.runTest(ctx, value)
		cancel()
		if !p.closed.Load() {
			p.post(func() { p.finish(result, err) })
		}
	}()
}

func (p *routingPane) finish(result proxy.RoutingResult, err error) {
	if p.closed.Load() {
		return
	}
	p.probing, p.cancelling, p.cancel = false, false, nil
	if p.OnBusy != nil {
		p.OnBusy(false)
	}
	p.updateButtons()
	if errors.Is(err, context.Canceled) {
		p.status.SetText("测试已取消")
		return
	}
	if err != nil {
		p.status.SetText("测试失败：" + err.Error())
		return
	}
	p.mode.SetText(result.Mode)
	p.rule.SetText(routingValue(result.Rule))
	p.chain.SetText(routingValue(strings.Join(result.Chain, " → ")))
	p.node.SetText(routingValue(result.Node))
	p.source.SetText(routingValue(result.Source))
	latency := "—"
	if result.Duration > 0 {
		latency = fmt.Sprintf("%.0f ms", max(1.0, float64(result.Duration.Microseconds())/1000))
	}
	p.latency.SetText(latency)
	response := "—"
	if result.StatusCode != 0 {
		response = fmt.Sprintf("HTTP %d", result.StatusCode)
	}
	p.response.SetText(response)
	p.status.SetText("测试完成")
	if result.Error != "" {
		p.status.SetText(result.Error)
	}
	p.results.Show()
}

func routingValue(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func (p *routingPane) Close() {
	if p.closed.Swap(true) {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
}
