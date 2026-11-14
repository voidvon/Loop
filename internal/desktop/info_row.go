package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// infoRow composes standard controls for lists and section headers. Lists keep
// ownership of selection, hover, keyboard navigation and row recycling.
type infoRow struct {
	widget.BaseWidget
	title, detail  *widget.Label
	icon           *widget.Icon
	iconHolder     *fyne.Container
	leading        *fyne.Container
	trailing       *fyne.Container
	leadingObject  fyne.CanvasObject
	trailingObject fyne.CanvasObject
	content        fyne.CanvasObject
	detailArea     *fyne.Container
	detailBadge    *statusBadge
}

func newListRow(icon fyne.Resource) *infoRow {
	return newInfoRow(theme.SizeNameText, 10, 12, 18, icon, nil)
}

func newSectionHeader(title, detail string, icon fyne.Resource, actions fyne.CanvasObject) *infoRow {
	r := newInfoRow(theme.SizeNameSubHeadingText, 8, 8, 20, icon, actions)
	r.detail.Wrapping = fyne.TextWrapWord
	r.detail.Truncation = fyne.TextTruncateOff
	r.setText(title, detail)
	return r
}

func newInfoRow(titleSize fyne.ThemeSizeName, verticalPadding, horizontalPadding, iconSize float32, icon fyne.Resource, actions fyne.CanvasObject) *infoRow {
	r := &infoRow{
		title:    widget.NewLabelWithStyle("名称", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		detail:   quietLabel("简介"),
		icon:     widget.NewIcon(nil),
		leading:  container.NewCenter(),
		trailing: container.NewCenter(),
	}
	r.title.SizeName = titleSize
	r.iconHolder = container.NewGridWrap(fyne.NewSize(iconSize, iconSize), r.icon)
	for _, label := range []*widget.Label{r.title, r.detail} {
		label.Wrapping = fyne.TextWrapOff
		label.Truncation = fyne.TextTruncateEllipsis
	}
	r.detailArea = container.NewStack(r.detail)
	text := container.NewThemeOverride(container.New(layout.NewCustomPaddedVBoxLayout(2), r.title, r.detailArea), titleDetailTheme{fyne.CurrentApp().Settings().Theme()})
	body := container.NewBorder(nil, nil, r.leading, r.trailing, text)
	r.content = inset(verticalPadding, verticalPadding, horizontalPadding, horizontalPadding, body)
	r.ExtendBaseWidget(r)
	r.setIcon(icon)
	r.setTrailing(actions)
	return r
}

func (r *infoRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.content)
}

func (r *infoRow) setText(title, detail string) {
	r.title.SetText(title)
	r.detail.SetText(detail)
	if detail == "" {
		r.detailArea.Hide()
	} else {
		r.detailArea.Show()
	}
	r.Refresh()
}

func (r *infoRow) setTargetBadge(text, target string) {
	if r.detailBadge == nil {
		r.detailBadge = newStatusBadge(r.detail)
		r.detailArea.Objects = []fyne.CanvasObject{container.NewHBox(r.detailBadge)}
	}
	r.detailBadge.setTarget(text, target)
	r.detailArea.Refresh()
}

func (r *infoRow) setIcon(icon fyne.Resource) {
	r.icon.SetResource(icon)
	if icon == nil {
		r.setLeading(nil)
		return
	}
	if r.leadingObject != r.iconHolder {
		r.setLeading(r.iconHolder)
	}
}

func (r *infoRow) setLeading(object fyne.CanvasObject) {
	r.leadingObject = object
	if object == nil {
		r.leading.Objects = nil
		r.leading.Hide()
	} else {
		r.leading.Objects = []fyne.CanvasObject{inset(0, 0, 0, 8, container.NewCenter(object))}
		r.leading.Show()
	}
	r.Refresh()
}

func (r *infoRow) setTrailing(object fyne.CanvasObject) {
	r.trailingObject = object
	if object == nil {
		r.trailing.Objects = nil
		r.trailing.Hide()
	} else {
		r.trailing.Objects = []fyne.CanvasObject{inset(0, 0, 8, 0, container.NewCenter(object))}
		r.trailing.Show()
	}
	r.Refresh()
}

func (r *infoRow) setTrailingVisible(visible bool) {
	if visible && r.trailingObject != nil {
		r.trailing.Show()
	} else {
		r.trailing.Hide()
	}
	r.Refresh()
}

// The brand block uses the same label spacing with its own font sizes.
func newTitleDetail(title, detail *widget.Label) fyne.CanvasObject {
	return container.NewThemeOverride(
		container.New(layout.NewCustomPaddedVBoxLayout(2), title, detail),
		titleDetailTheme{fyne.CurrentApp().Settings().Theme()},
	)
}

// Each label otherwise adds its own top/bottom padding, separating the two
// lines. The row provides the outer padding and one explicit gap instead.
type titleDetailTheme struct{ fyne.Theme }

func (t titleDetailTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding {
		return 0
	}
	return t.Theme.Size(name)
}
