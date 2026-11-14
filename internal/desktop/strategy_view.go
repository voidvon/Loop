package desktop

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/profile"
)

// Shared by strategy members and managed nodes. GridWrap owns scrolling,
// keyboard selection and recycling; card content supplies the saved state.
type memberCard struct {
	widget.BaseWidget
	title, detail, latency *widget.Label
	icon                   *widget.Icon
	background             *surface
	content                fyne.CanvasObject
}

func newMemberCard() *memberCard {
	c := &memberCard{title: dashboardValue("成员", sizeMemberName), detail: quietLabel("节点"), latency: quietLabel("--"), icon: widget.NewIcon(iconNodes)}
	c.title.Wrapping = fyne.TextWrapBreak
	c.detail.SizeName = sizeMemberDetail
	c.latency.SizeName = sizeMemberDetail
	c.detail.Wrapping = fyne.TextWrapOff
	c.detail.Truncation = fyne.TextTruncateEllipsis
	c.latency.Wrapping = fyne.TextWrapOff
	c.latency.Truncation = fyne.TextTruncateEllipsis
	c.background = themedSurface(colorMemberCard, 16).(*surface)
	badge := collectionIcon(c.icon, 28)
	c.content = container.New(memberCardLayout{card: c}, c.background, badge, homeLabel(c.title), homeLabel(c.detail), homeLabel(c.latency))
	c.ExtendBaseWidget(c)
	return c
}
func (c *memberCard) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}

// Reserve two lines for every name so metadata aligns across the row. The
// measured text heights and bottom inset keep the final line inside the card.
type memberCardLayout struct{ card *memberCard }

func (l memberCardLayout) heights() (float32, float32) {
	name := fyne.MeasureText("Mg", l.card.title.Theme().Size(sizeMemberName), l.card.title.TextStyle).Height
	detail := fyne.MeasureText("Mg", l.card.detail.Theme().Size(sizeMemberDetail), l.card.detail.TextStyle).Height
	return name*2 + 1, detail
}
func (l memberCardLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	name, detail := l.heights()
	return fyne.NewSize(154, fyne.Max(76, 8+name+3+detail+1+detail+10))
}
func (l memberCardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	name, detail := l.heights()
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
	objects[1].Move(fyne.NewPos(8, 8))
	objects[1].Resize(fyne.NewSize(28, 28))
	width := fyne.Max(0, size.Width-44-10)
	y := float32(8)
	for i, height := range []float32{name, detail, detail} {
		top, boxHeight := y, height
		if i == 0 && !strings.Contains(l.card.title.Text, "\n") && fyne.MeasureText(l.card.title.Text, l.card.title.Theme().Size(sizeMemberName), l.card.title.TextStyle).Width <= width {
			boxHeight = (name - 1) / 2
			top += (name - boxHeight) / 2
		}
		objects[i+2].Move(fyne.NewPos(44, top))
		objects[i+2].Resize(fyne.NewSize(width, boxHeight))
		y += height
		if i == 0 {
			y += 3
		} else {
			y += 1
		}
	}
}

func (c *memberCard) update(name, detail, latency string, importance widget.Importance, icon fyne.Resource, selected bool) {
	c.title.SetText(name)
	c.detail.SetText(detail)
	c.latency.Importance = importance
	c.latency.SetText(latency)
	c.latency.Refresh()
	c.icon.SetResource(icon)
	c.background.name = colorMemberCard
	if selected {
		c.background.name = colorMemberSelected
	}
	c.background.Refresh()
	c.Refresh()
}

func strategyMemberDetail(m profile.PolicyMember) string {
	if m.IsBuiltin {
		if m.Key == "direct" {
			return "Direct"
		}
		return "Reject"
	}
	if m.IsGroup {
		return "策略组"
	}
	prefix := "订阅节点"
	if m.Source == "manual" || m.Source == "" {
		prefix = "本地节点"
	}
	if m.Detail != "" {
		return prefix + " · " + m.Detail
	}
	return prefix
}
func strategyMemberIcon(m profile.PolicyMember) fyne.Resource {
	icon := iconGlobe
	if m.IsBuiltin {
		icon = iconReject
		if m.Key == "direct" {
			icon = iconArrow
		}
	} else if m.IsGroup {
		return strategyGroupIcon(m.Key)
	}
	return theme.NewColoredResource(icon, colorAccent)
}
