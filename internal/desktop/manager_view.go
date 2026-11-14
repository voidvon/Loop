package desktop

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/profile"
)

func (m *nodeManager) buildView() {
	m.summary = quietLabel("")
	m.summary.Wrapping = fyne.TextWrapOff
	m.sourceHeader = newSectionHeader("", "", theme.NewColoredResource(iconLocalNodes, colorAccent), nil)
	m.localSource = newChoiceButton("本地节点", theme.NewColoredResource(iconLocalNodes, colorAccent), func() { m.selectSource(0) })
	m.localSource.Alignment = widget.ButtonAlignLeading
	m.emptySources = quietLabel("尚未添加订阅\n点击右上角 + 添加")
	m.emptySources.Wrapping = fyne.TextWrapOff
	m.emptySources.Alignment = fyne.TextAlignCenter
	sources := container.NewStack(m.subscriptions, container.NewCenter(m.emptySources))
	left := collectionSidebar(inset(4, 4, 0, 0, container.NewBorder(
		container.NewVBox(container.NewThemeOverride(m.localSource.Button, nodeSourceTheme{fyne.CurrentApp().Settings().Theme()}), inset(18, 8, 6, 0, quietLabel("订阅节点"))),
		nil, nil, nil, sources)))
	m.emptyNodes = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	list := container.NewStack(collectionGrid(m.nodes, 12, 16, colorMemberSelected), container.NewCenter(m.emptyNodes))
	right := collectionDetail(container.NewBorder(inset(0, 12, 0, 0, m.sourceHeader), inset(12, 0, 0, 0, m.message), nil, nil, list))
	m.add = widget.NewButtonWithIcon("", theme.ContentAddIcon(), m.addSource)
	heading := collectionPageHeader("节点", m.summary, collectionAction(m.add, colorAccent))
	columns := collectionColumns(left, right)
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

type nodeSourceTheme struct{ fyne.Theme }

func (t nodeSourceTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInnerPadding {
		return 6
	}
	return (navigationTheme{t.Theme}).Size(n)
}
func (t nodeSourceTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNamePrimary:
		return t.Theme.Color(colorMemberSelected, v)
	case theme.ColorNameForegroundOnPrimary:
		return t.Theme.Color(theme.ColorNameForeground, v)
	}
	return t.Theme.Color(n, v)
}

var iconLocalNodes = outlineIcon("local-nodes", `<rect x="5" y="3" width="14" height="12" rx="3"/><path d="M5 9h14M8 6h8M9 12h6M12 15v5M5 21h14"/>`)

func nodeCardDetails(node profile.Node) (string, fyne.Resource) {
	kind, _ := node.Outbound["type"].(string)
	icon := iconGlobe
	switch kind {
	case "direct":
		kind, icon = "Direct", iconArrow
	case "block":
		kind, icon = "Reject", iconReject
	default:
		kind = strings.ToUpper(kind)
	}
	if kind == "" {
		kind = "节点"
	}
	return kind, theme.NewColoredResource(icon, colorAccent)
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
