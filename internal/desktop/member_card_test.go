package desktop

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestMemberCardTwoLineNameKeepsMetadataAndBottomInset(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	c := newMemberCard()
	c.update(strings.Repeat("LongNodeName", 10), "VLESS", "--", widget.LowImportance, iconGlobe, false)
	w := a.NewWindow("card")
	w.SetPadded(false)
	w.SetContent(c)
	c.Resize(c.MinSize())
	test.WidgetRenderer(c).Layout(c.Size())
	lines := memberTitleLines(c.title)
	if len(lines) != 2 || !strings.HasSuffix(lines[1], "…") {
		t.Fatalf("long name must occupy at most two lines with ellipsis: %q", lines)
	}
	objects := c.content.(*fyne.Container).Objects
	metadataY := objects[3].Position().Y
	bottom := objects[4]
	if metadataY < objects[2].Position().Y+objects[2].Size().Height+3 || c.Size().Height-bottom.Position().Y-bottom.Size().Height < 9.9 {
		t.Fatal("name overlaps metadata or final line lost its bottom inset")
	}
	c.update("Short", "Direct", "68 ms", widget.SuccessImportance, iconArrow, true)
	test.WidgetRenderer(c).Layout(c.Size())
	if lines := memberTitleLines(c.title); len(lines) != 1 || lines[0] != "Short" {
		t.Fatalf("recycled card retained a second name line: %q", lines)
	}
	if objects[3].Position().Y != metadataY || c.latency.Text != "68 ms" {
		t.Fatal("short name shifted metadata or lost the measured latency")
	}
}

func memberTitleLines(object fyne.CanvasObject) []string {
	if !object.Visible() {
		return nil
	}
	switch item := object.(type) {
	case *canvas.Text:
		if item.Text != "" {
			return []string{item.Text}
		}
	case *fyne.Container:
		var lines []string
		for _, child := range item.Objects {
			lines = append(lines, memberTitleLines(child)...)
		}
		return lines
	case fyne.Widget:
		var lines []string
		for _, child := range test.WidgetRenderer(item).Objects() {
			lines = append(lines, memberTitleLines(child)...)
		}
		return lines
	}
	return nil
}
