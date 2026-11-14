package desktop

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func (m *nodeManager) buildView() {
	m.summary = quietLabel("")
	m.summary.Wrapping = fyne.TextWrapOff
	m.sourceHeader = newSectionHeader("", "", theme.NewColoredResource(theme.ComputerIcon(), colorAccent), nil)
	m.localSource = newChoiceButton("本地节点", theme.ComputerIcon(), func() { m.selectSource(0) })
	m.localSource.Alignment = widget.ButtonAlignLeading
	m.emptySources = quietLabel("尚未添加订阅\n点击右上角 + 添加")
	m.emptySources.Wrapping = fyne.TextWrapOff
	m.emptySources.Alignment = fyne.TextAlignCenter
	sources := container.NewStack(m.subscriptions, container.NewCenter(m.emptySources))
	left := panel(container.NewBorder(
		container.NewVBox(m.localSource.Button, inset(22, 8, 6, 0, quietLabel("订阅节点"))),
		nil, nil, nil, sources))
	m.emptyNodes = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	list := container.NewStack(m.nodes, container.NewCenter(m.emptyNodes))
	right := panel(container.NewBorder(inset(0, 12, 0, 0, m.sourceHeader), inset(12, 0, 0, 0, m.message), nil, nil, list))
	m.add = widget.NewButtonWithIcon("", theme.ContentAddIcon(), m.addSource)
	title := widget.NewLabelWithStyle("节点", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameHeadingText
	heading := inset(0, 16, 0, 0, container.NewBorder(nil, nil, nil, container.NewCenter(m.add), container.NewHBox(title, container.NewCenter(m.summary))))
	columns := container.New(nodeColumnsLayout{leftWidth: 238}, left, right)
	m.root = container.NewBorder(heading, nil, nil, nil, columns)
}

func (m *nodeManager) selectSource(index int) {
	if index < 0 || index >= len(m.sources) {
		return
	}
	m.hideContextMenu()
	m.sourceIndex = index
	m.syncSources()
	m.refreshNodes()
}

func (m *nodeManager) syncSources() {
	m.loading = true
	defer func() { m.loading = false }()
	m.localSource.Select(m.sourceIndex == 0)
	if len(m.sources) > 0 {
		m.localSource.SetText(fmt.Sprintf("本地节点\n%d 个节点", len(m.sources[0].Nodes)))
	}
	m.subscriptions.Refresh()
	if m.sourceIndex > 0 {
		m.subscriptions.Select(m.sourceIndex - 1)
	} else {
		m.subscriptions.UnselectAll()
	}
	if len(m.sources) <= 1 {
		m.emptySources.Show()
	} else {
		m.emptySources.Hide()
	}
	total := 0
	for _, source := range m.sources {
		total += len(source.Nodes)
	}
	m.summary.SetText(fmt.Sprintf("%d 个订阅  ·  %d 个节点", max(0, len(m.sources)-1), total))
}

func (m *nodeManager) refreshSourceDetails() {
	if m.sourceIndex < 0 || m.sourceIndex >= len(m.sources) {
		return
	}
	source := m.sources[m.sourceIndex]
	name, detail := source.Name, fmt.Sprintf("%d 个节点", len(source.Nodes))
	if source.ID == "manual" {
		name = "本地节点"
	} else {
		if source.Format == "clash" {
			detail += "  ·  Clash YAML"
		} else {
			detail += "  ·  本地 JSON"
		}
		if stamp, err := time.Parse(time.RFC3339, source.UpdatedAt); err == nil {
			detail += "  ·  更新于 " + stamp.Local().Format("01-02 15:04")
		}
	}
	m.sourceHeader.setText(name, detail)
	if len(m.nodeItems) == 0 {
		text := "暂无节点"
		if source.ID == "manual" {
			text = "暂无本地节点\n点击右上角 + 添加节点。"
		} else {
			text = "该订阅暂无节点，请更新订阅。"
		}
		m.emptyNodes.SetText(text)
		m.emptyNodes.Show()
		m.nodes.Hide()
	} else {
		m.emptyNodes.Hide()
		m.nodes.Show()
	}
}

// Keep the source list steady while letting the node list take extra space.
type nodeColumnsLayout struct{ leftWidth float32 }

var _ fyne.Layout = nodeColumnsLayout{}

func (l nodeColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(l.leftWidth+16+objects[1].MinSize().Width, fyne.Max(objects[0].MinSize().Height, objects[1].MinSize().Height))
}
func (l nodeColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(l.leftWidth, size.Height))
	objects[1].Move(fyne.NewPos(l.leftWidth+16, 0))
	objects[1].Resize(fyne.NewSize(fyne.Max(0, size.Width-l.leftWidth-16), size.Height))
}
