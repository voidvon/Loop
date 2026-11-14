package desktop

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

const benchmarkCurrent = "当前分流"
const benchmarkSelected = "指定节点"

type benchmarkPane struct {
	root                                                fyne.CanvasObject
	window                                              fyne.Window
	url, requests, concurrency, timeout, duration, rate *widget.Entry
	method                                              *widget.Select
	mode                                                *widget.RadioGroup
	keepAlive, redirects, compression, http2            *widget.Check
	picker, startButton                                 *widget.Button
	status                                              *widget.Label
	progress                                            *widget.ProgressBar
	table                                               *widget.Table
	loadNodes                                           func() ([]app.BenchmarkNode, error)
	run                                                 func(context.Context, app.BenchmarkRequest, func(proxy.BenchmarkProgress)) (proxy.BenchmarkReport, error)
	post                                                func(func())
	OnBusy                                              func(bool)
	nodes                                               []app.BenchmarkNode
	selected                                            map[string]bool
	order                                               []string
	results                                             map[string]proxy.BenchmarkResult
	runID                                               string
	optionsUsed                                         proxy.BenchmarkOptions
	available, probing, cancelling                      bool
	cancel                                              context.CancelFunc
	done                                                chan struct{}
	closed                                              atomic.Bool
}

func newBenchmarkPane() *benchmarkPane {
	p := &benchmarkPane{post: fyne.Do, available: true, selected: map[string]bool{}, results: map[string]proxy.BenchmarkResult{}}
	entry := func(value string) *widget.Entry { e := widget.NewEntry(); e.SetText(value); return e }
	p.url = entry("https://ai.nodeisle.com/")
	p.requests, p.concurrency, p.timeout = entry("200"), entry("10"), entry("20")
	p.duration, p.rate = entry(""), entry("0")
	p.duration.SetPlaceHolder("如 30s；留空按请求数")
	p.method = widget.NewSelect([]string{"GET", "HEAD"}, nil)
	p.method.SetSelected("GET")
	p.keepAlive = widget.NewCheck("复用连接", nil)
	p.keepAlive.SetChecked(true)
	p.redirects = widget.NewCheck("跟随重定向", nil)
	p.compression = widget.NewCheck("禁用压缩", nil)
	p.compression.SetChecked(true)
	p.http2 = widget.NewCheck("HTTP/2", nil)
	p.picker = widget.NewButtonWithIcon("选择节点", theme.ListIcon(), p.chooseNodes)
	p.picker.Hide()
	p.mode = widget.NewRadioGroup([]string{benchmarkCurrent, benchmarkSelected}, func(string) { p.updateControls() })
	p.mode.Horizontal, p.mode.Required = true, true
	p.mode.SetSelected(benchmarkCurrent)
	p.startButton = widget.NewButtonWithIcon("开始测试", iconSpeed, p.start)
	p.startButton.Importance = widget.HighImportance
	p.status = quietLabel("每个节点单独执行一组请求；点击结果行查看详细统计。")
	p.status.Wrapping = fyne.TextWrapWord
	p.progress = widget.NewProgressBar()
	p.progress.Hide()
	field := func(label string, object fyne.CanvasObject) fyne.CanvasObject {
		return container.NewVBox(quietLabel(label), object)
	}
	advanced := widget.NewAccordion(widget.NewAccordionItem("高级参数", container.NewVBox(
		container.NewGridWithColumns(3, field("持续时间 -z", p.duration), field("每任务 QPS -q", p.rate), field("方法 -m", p.method)),
		container.NewGridWithColumns(2, p.keepAlive, p.redirects, p.compression, p.http2),
		quietLabel("设置 -z 后忽略 -n；-q 为每个并发任务的请求上限。"),
	)))
	settings := container.NewVBox(
		sectionTitle("批量测速"),
		container.NewBorder(nil, nil, nil, p.startButton, p.url),
		container.NewGridWithColumns(3, field("请求数 -n", p.requests), field("并发数 -c", p.concurrency), field("超时秒 -t", p.timeout)),
		container.NewBorder(nil, nil, nil, p.picker, p.mode), advanced, p.status, p.progress,
	)
	headers := []string{"节点 / 分流", "完成", "2xx 成功率", "平均 ms", "P95≈ ms", "请求/秒"}
	p.table = widget.NewTable(func() (int, int) { return len(p.order), len(headers) }, func() fyne.CanvasObject {
		label := widget.NewLabel("")
		label.Truncation = fyne.TextTruncateEllipsis
		return label
	}, func(id widget.TableCellID, object fyne.CanvasObject) {
		object.(*widget.Label).SetText(p.cell(id.Row, id.Col))
	})
	p.table.ShowHeaderRow = true
	p.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	p.table.UpdateHeader = func(id widget.TableCellID, object fyne.CanvasObject) {
		if id.Col >= 0 && id.Col < len(headers) {
			object.(*widget.Label).SetText(headers[id.Col])
		}
	}
	for i, width := range []float32{170, 58, 85, 80, 80, 80} {
		p.table.SetColumnWidth(i, width)
	}
	p.table.OnSelected = func(id widget.TableCellID) { p.table.UnselectAll(); p.showResult(id.Row) }
	p.root = container.NewBorder(settings, nil, nil, nil, panel(p.table))
	return p
}

func (p *benchmarkPane) Refresh() {
	if p.closed.Load() || p.probing || p.loadNodes == nil {
		return
	}
	nodes, err := p.loadNodes()
	if err != nil {
		p.status.SetText("无法读取节点：" + err.Error())
		return
	}
	p.nodes = nodes
	exists := map[string]bool{}
	for _, node := range nodes {
		exists[node.Key] = true
	}
	for key := range p.selected {
		if !exists[key] {
			delete(p.selected, key)
		}
	}
	p.updateControls()
}

func (p *benchmarkPane) SetAvailable(value bool) { p.available = value; p.updateControls() }

func (p *benchmarkPane) updateControls() {
	// RadioGroup invokes its callback while the pane is still being built.
	if p.startButton == nil {
		return
	}
	for _, entry := range []*widget.Entry{p.url, p.requests, p.concurrency, p.timeout, p.duration, p.rate} {
		setEnabled(entry, !p.probing)
	}
	for _, check := range []*widget.Check{p.keepAlive, p.redirects, p.compression, p.http2} {
		setEnabled(check, !p.probing)
	}
	setEnabled(p.mode, !p.probing)
	setEnabled(p.method, !p.probing)
	setEnabled(p.picker, !p.probing && p.available && len(p.nodes) > 0)
	p.picker.SetText(fmt.Sprintf("选择节点 (%d)", len(p.selected)))
	if p.mode.Selected == benchmarkSelected {
		p.picker.Show()
	} else {
		p.picker.Hide()
	}
	if p.probing {
		p.startButton.SetText("取消测试")
		p.startButton.SetIcon(theme.CancelIcon())
		setEnabled(p.startButton, !p.cancelling)
	} else {
		p.startButton.SetText("开始测试")
		p.startButton.SetIcon(iconSpeed)
		setEnabled(p.startButton, p.available && p.run != nil)
	}
}

func (p *benchmarkPane) options() (proxy.BenchmarkOptions, error) {
	integer := func(e *widget.Entry) (int, error) { return strconv.Atoi(strings.TrimSpace(e.Text)) }
	n, err := integer(p.requests)
	if err != nil && strings.TrimSpace(p.duration.Text) == "" {
		return proxy.BenchmarkOptions{}, errors.New("请求数须为整数")
	}
	c, err := integer(p.concurrency)
	if err != nil {
		return proxy.BenchmarkOptions{}, errors.New("并发数须为整数")
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(p.timeout.Text), 64)
	if err != nil || seconds <= 0 || seconds > 120 {
		return proxy.BenchmarkOptions{}, errors.New("超时须大于 0 且不超过 120 秒")
	}
	rate, err := strconv.ParseFloat(strings.TrimSpace(p.rate.Text), 64)
	if err != nil {
		return proxy.BenchmarkOptions{}, errors.New("QPS 须为数字，0 表示不限速")
	}
	var duration time.Duration
	if value := strings.TrimSpace(p.duration.Text); value != "" {
		duration, err = time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return proxy.BenchmarkOptions{}, errors.New("持续时间格式无效，例如 30s 或 2m")
		}
	}
	return (proxy.BenchmarkOptions{URL: p.url.Text, Requests: n, Concurrency: c, Timeout: time.Duration(seconds * float64(time.Second)), Duration: duration, Rate: rate, Method: p.method.Selected,
		DisableKeepAlive: !p.keepAlive.Checked, FollowRedirects: p.redirects.Checked, DisableCompression: p.compression.Checked, HTTP2: p.http2.Checked}).Validated()
}

func (p *benchmarkPane) start() {
	if p.closed.Load() {
		return
	}
	if p.probing {
		if p.cancel != nil {
			p.cancelling = true
			p.status.SetText("正在取消…")
			p.cancel()
			p.updateControls()
		}
		return
	}
	if !p.available || p.run == nil {
		return
	}
	options, err := p.options()
	if err != nil {
		p.status.SetText(err.Error())
		return
	}
	request := app.BenchmarkRequest{ID: profile.NewID(), Options: options}
	order := []string{"routing"}
	results := map[string]proxy.BenchmarkResult{"routing": {Key: "routing", Name: benchmarkCurrent, Planned: options.Requests}}
	if p.mode.Selected == benchmarkSelected {
		order = nil
		results = map[string]proxy.BenchmarkResult{}
		for _, node := range p.nodes {
			if p.selected[node.Key] {
				request.NodeKeys = append(request.NodeKeys, node.Key)
				order = append(order, node.Key)
				results[node.Key] = proxy.BenchmarkResult{Key: node.Key, Name: node.Name, Source: node.Source, Planned: options.Requests}
			}
		}
		if len(order) == 0 {
			p.status.SetText("请先选择要测试的节点")
			return
		}
	}
	p.url.SetText(options.URL)
	p.runID, p.order, p.results = request.ID, order, results
	p.optionsUsed = options
	p.probing, p.cancelling = true, false
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan struct{})
	done := p.done
	p.status.SetText("正在准备测试…")
	p.progress.SetValue(0)
	p.progress.Show()
	p.table.Refresh()
	if p.OnBusy != nil {
		p.OnBusy(true)
	}
	p.updateControls()
	go func() {
		defer close(done)
		report, err := p.run(ctx, request, func(progress proxy.BenchmarkProgress) {
			if !p.closed.Load() {
				p.post(func() { p.ApplyProgress(&progress) })
			}
		})
		cancel()
		if !p.closed.Load() {
			p.post(func() { p.finish(report, err) })
		}
	}()
}

func (p *benchmarkPane) ApplyProgress(value *proxy.BenchmarkProgress) {
	if p.closed.Load() || value == nil || value.ID != p.runID {
		return
	}
	for _, result := range value.Results {
		p.mergeResult(result)
	}
	p.mergeResult(value.Result)
	p.table.Refresh()
	if !p.probing || p.cancelling {
		return
	}
	result := value.Result
	count := strconv.Itoa(result.Completed)
	fraction := 0.0
	if result.Planned > 0 {
		count += fmt.Sprintf(" / %d", result.Planned)
		fraction = float64(result.Completed) / float64(result.Planned)
	} else if options, err := p.options(); err == nil && options.Duration > 0 {
		fraction = float64(result.Elapsed) / float64(options.Duration)
	}
	if value.Total > 0 {
		p.progress.SetValue((float64(value.Index) + min(1.0, fraction)) / float64(value.Total))
	}
	p.status.SetText(fmt.Sprintf("%d / %d · %s · 已完成 %s", value.Index+1, value.Total, result.Name, count))
}

func (p *benchmarkPane) finish(report proxy.BenchmarkReport, err error) {
	if p.closed.Load() {
		return
	}
	for _, result := range report.Results {
		p.mergeResult(result)
	}
	p.probing, p.cancelling, p.cancel = false, false, nil
	if p.OnBusy != nil {
		p.OnBusy(false)
	}
	p.updateControls()
	p.table.Refresh()
	if errors.Is(err, context.Canceled) || report.Cancelled {
		p.status.SetText("测试已取消，保留已完成的结果")
		return
	}
	if err != nil {
		p.status.SetText("测试失败：" + err.Error())
		return
	}
	p.progress.SetValue(1)
	p.status.SetText(fmt.Sprintf("测试完成 · %d 个目标 · 点击结果行查看分位延迟、吞吐与状态码", len(report.Results)))
}

func (p *benchmarkPane) cell(row, col int) string {
	if row < 0 || row >= len(p.order) {
		return ""
	}
	r := p.results[p.order[row]]
	if col == 0 {
		return r.Name
	}
	if r.Completed == 0 {
		if r.Finished {
			return "—"
		}
		if !p.probing {
			return "未测试"
		}
		return "待测"
	}
	switch col {
	case 1:
		return strconv.Itoa(r.Completed)
	case 2:
		if r.Completed == r.CancelledRequests {
			return "—"
		}
		return fmt.Sprintf("%.1f%%", float64(r.Success)*100/float64(max(1, r.Completed-r.CancelledRequests)))
	case 3:
		return benchmarkMilliseconds(r.Average)
	case 4:
		return benchmarkMilliseconds(r.P95)
	case 5:
		return fmt.Sprintf("%.1f", r.RequestsPerSecond)
	}
	return ""
}

func benchmarkMilliseconds(value time.Duration) string {
	if value == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f", float64(value)/float64(time.Millisecond))
}

func (p *benchmarkPane) showResult(row int) {
	if p.window == nil || row < 0 || row >= len(p.order) {
		return
	}
	r := p.results[p.order[row]]
	if r.Completed == 0 {
		return
	}
	lines := []string{p.optionsUsed.Method + " " + p.optionsUsed.URL,
		fmt.Sprintf("完成 %d · 2xx %d · HTTP 非 2xx %d · 网络失败 %d · 取消 %d", r.Completed, r.Success, r.HTTPFailures, r.NetworkFailures, r.CancelledRequests),
		fmt.Sprintf("总耗时 %.2f s · %.2f 请求/秒", r.Elapsed.Seconds(), r.RequestsPerSecond),
		fmt.Sprintf("响应体 %.2f MiB · 平均 %.2f MiB/s", float64(r.Bytes)/(1<<20), r.BytesPerSecond/(1<<20)),
		fmt.Sprintf("平均 %s ms · 最快 %s ms · 最慢 %s ms", benchmarkMilliseconds(r.Average), benchmarkMilliseconds(r.Fastest), benchmarkMilliseconds(r.Slowest)),
		fmt.Sprintf("P50≈ %s ms · P90≈ %s ms · P95≈ %s ms · P99≈ %s ms", benchmarkMilliseconds(r.P50), benchmarkMilliseconds(r.P90), benchmarkMilliseconds(r.P95), benchmarkMilliseconds(r.P99))}
	if r.Source != "" {
		lines = append([]string{"来源：" + r.Source}, lines...)
	}
	codes := make([]int, 0, len(r.StatusCodes))
	for code := range r.StatusCodes {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	for _, code := range codes {
		lines = append(lines, fmt.Sprintf("HTTP %d：%d", code, r.StatusCodes[code]))
	}
	errors := make([]string, 0, len(r.Errors))
	for message := range r.Errors {
		errors = append(errors, message)
	}
	sort.Strings(errors)
	for _, message := range errors {
		lines = append(lines, fmt.Sprintf("%s：%d", message, r.Errors[message]))
	}
	lines = append(lines, "延迟计至完整响应体读取完毕；分位值误差约 1%，最小分辨率 1 µs。")
	d := dialog.NewCustom(r.Name, "关闭", container.NewVScroll(wrappedLabel(strings.Join(lines, "\n\n"))), p.window)
	d.Resize(fyne.NewSize(620, 540))
	d.Show()
}

func (p *benchmarkPane) mergeResult(result proxy.BenchmarkResult) {
	previous, exists := p.results[result.Key]
	if !exists || previous.Completed > result.Completed || (previous.Finished && !result.Finished) || (previous.Completed == result.Completed && previous.Elapsed > result.Elapsed) {
		return
	}
	p.results[result.Key] = result
}

func (p *benchmarkPane) Close() {
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
