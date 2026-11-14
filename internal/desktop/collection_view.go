package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Shared presentation for strategy, node and rule collections. Each page keeps
// its data, selection and mutation callbacks; Fyne owns list recycling/focus.
func collectionPageHeader(title string, summary *widget.Label, actions ...fyne.CanvasObject) fyne.CanvasObject {
	heading := container.NewHBox(container.NewCenter(container.NewThemeOverride(pageHeading(title), titleDetailTheme{fyne.CurrentApp().Settings().Theme()})), container.NewCenter(homeLabel(summary)))
	return inset(6, 10, 0, 0, container.NewBorder(nil, nil, nil, container.NewHBox(actions...), heading))
}

func collectionSidebar(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(themedSurface(colorPanel, 20), inset(8, 8, 6, 6,
		container.NewThemeOverride(content, collectionListTheme{fyne.CurrentApp().Settings().Theme()})))
}

func collectionDetail(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(themedSurface(colorPanel, 20), inset(12, 14, 14, 14, content))
}

func collectionColumns(left, right fyne.CanvasObject) fyne.CanvasObject {
	return container.New(collectionColumnsLayout{}, left, right)
}

func newCollectionRow(icon fyne.Resource, iconSize, verticalPadding float32) *infoRow {
	return newInfoRow(theme.SizeNameText, verticalPadding, 6, iconSize, icon, nil)
}

func collectionIcon(icon *widget.Icon, size float32) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(size, size), container.NewStack(themedSurface(colorMemberBadge, size/2), inset(size/4, size/4, size/4, size/4, icon)))
}

func collectionAction(button *widget.Button, tint fyne.ThemeColorName) fyne.CanvasObject {
	button.Importance = widget.MediumImportance
	return container.NewGridWrap(fyne.NewSize(34, 34), container.NewThemeOverride(button, collectionActionTheme{Theme: fyne.CurrentApp().Settings().Theme(), tint: tint}))
}

type collectionActionTheme struct {
	fyne.Theme
	tint fyne.ThemeColorName
}

func (t collectionActionTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameForeground:
		return t.Theme.Color(t.tint, v)
	case theme.ColorNameButton:
		return t.Theme.Color(colorMemberCard, v)
	}
	return t.Theme.Color(n, v)
}
func (t collectionActionTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameButtonRadius:
		return 17
	case theme.SizeNameInlineIcon:
		return 16
	case theme.SizeNameInnerPadding:
		return 6
	}
	return t.Theme.Size(n)
}

type collectionListTheme struct{ fyne.Theme }

func (t collectionListTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameSeparatorThickness:
		return 0
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameSelectionRadius:
		return 10
	}
	return t.Theme.Size(n)
}
func (t collectionListTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameSelection {
		return t.Theme.Color(colorMemberSelected, v)
	}
	return t.Theme.Color(n, v)
}

// GridWrap keeps the recycling and keyboard behavior for every card family.
func collectionGrid(content fyne.CanvasObject, gap, radius float32, selection fyne.ThemeColorName) fyne.CanvasObject {
	return container.NewThemeOverride(content, collectionGridTheme{Theme: fyne.CurrentApp().Settings().Theme(), gap: gap, radius: radius, selection: selection})
}

type collectionGridTheme struct {
	fyne.Theme
	gap, radius float32
	selection   fyne.ThemeColorName
}

func (t collectionGridTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNamePadding {
		return t.gap
	}
	if n == theme.SizeNameSelectionRadius {
		return t.radius
	}
	return t.Theme.Size(n)
}
func (t collectionGridTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameSelection {
		if t.selection == "" {
			return color.Transparent
		}
		return t.Theme.Color(t.selection, v)
	}
	return t.Theme.Color(n, v)
}

// A stable group column matches the reference and leaves member cards room to
// wrap as the user resizes, without a dark draggable split bar between panels.
type collectionColumnsLayout struct{}

func (collectionColumnsLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(670, 420) }
func (collectionColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	left := fyne.Min(270, size.Width*.35)
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(left, size.Height))
	objects[1].Move(fyne.NewPos(left+14, 0))
	objects[1].Resize(fyne.NewSize(size.Width-left-14, size.Height))
}
