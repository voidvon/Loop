package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const (
	colorPanel        fyne.ThemeColorName = "miniproxy.panel"
	colorSidebar      fyne.ThemeColorName = "miniproxy.sidebar"
	colorOrange       fyne.ThemeColorName = "miniproxy.orange"
	colorSecondary    fyne.ThemeColorName = "miniproxy.secondary"
	colorAccent       fyne.ThemeColorName = "miniproxy.accent"
	colorRuleCard     fyne.ThemeColorName = "miniproxy.rule-card"
	colorBadgeDirect  fyne.ThemeColorName = "miniproxy.badge-direct"
	colorBadgeProxy   fyne.ThemeColorName = "miniproxy.badge-proxy"
	colorBadgeReject  fyne.ThemeColorName = "miniproxy.badge-reject"
	colorBadgeMuted   fyne.ThemeColorName = "miniproxy.badge-muted"
	colorTrayActive   fyne.ThemeColorName = "miniproxy.tray-active"
	colorTrayInactive fyne.ThemeColorName = "miniproxy.tray-inactive"
	appearanceSystem                      = "system"
	appearanceLight                       = "light"
	appearanceDark                        = "dark"
)

type clientPalette struct {
	background, panel, sidebar, popup                   color.NRGBA
	text, secondary, primary, accent, selection, orange color.NRGBA
	button, disabledButton, border                      color.NRGBA
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 255} }

func palette(variant fyne.ThemeVariant) clientPalette {
	if variant == theme.VariantDark {
		return clientPalette{
			background: rgb(24, 27, 34), panel: rgb(34, 38, 47), sidebar: rgb(28, 32, 40), popup: rgb(43, 48, 59),
			text: rgb(232, 236, 243), secondary: rgb(155, 165, 183), primary: rgb(50, 109, 225), accent: rgb(131, 176, 255), selection: rgb(42, 62, 96),
			orange: rgb(246, 180, 111), button: rgb(47, 53, 65),
			disabledButton: rgb(41, 46, 56), border: rgb(58, 65, 79),
		}
	}
	return clientPalette{
		background: rgb(244, 245, 248), panel: rgb(255, 255, 255), sidebar: rgb(235, 238, 243), popup: rgb(255, 255, 255),
		text: rgb(42, 49, 63), secondary: rgb(111, 121, 139), primary: rgb(50, 109, 225), accent: rgb(50, 109, 225), selection: rgb(225, 234, 253),
		orange: rgb(191, 100, 29), button: rgb(240, 243, 248),
		disabledButton: rgb(241, 243, 246), border: rgb(227, 231, 238),
	}
}

// clientTheme resolves both standard widgets and custom surfaces from one palette.
// In system mode the variant comes from Fyne's live OS appearance notification.
type clientTheme struct {
	fyne.Theme
	mode  string
	fonts *systemFonts
}

func newClientTheme() *clientTheme {
	return &clientTheme{Theme: theme.DefaultTheme(), mode: appearanceSystem}
}

func (t *clientTheme) variant(system fyne.ThemeVariant) fyne.ThemeVariant {
	switch t.mode {
	case appearanceDark:
		return theme.VariantDark
	case appearanceLight:
		return theme.VariantLight
	default:
		return system
	}
}

func (t *clientTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	variant = t.variant(variant)
	p := palette(variant)
	switch name {
	case colorTrayActive:
		return color.NRGBA{A: 255}
	case colorTrayInactive:
		return color.NRGBA{A: 128}
	case theme.ColorNameBackground:
		return p.background
	case colorPanel:
		return p.panel
	case colorRuleCard:
		if variant == theme.VariantDark {
			return rgb(28, 32, 40)
		}
		return rgb(248, 249, 252)
	case colorBadgeDirect:
		if variant == theme.VariantDark {
			return rgb(32, 59, 49)
		}
		return rgb(226, 243, 233)
	case colorBadgeProxy:
		if variant == theme.VariantDark {
			return rgb(64, 48, 35)
		}
		return rgb(251, 237, 222)
	case colorBadgeReject:
		if variant == theme.VariantDark {
			return rgb(65, 35, 40)
		}
		return rgb(251, 229, 231)
	case colorBadgeMuted:
		return p.button
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return p.popup
	case colorSidebar:
		return p.sidebar
	case colorOrange:
		return p.orange
	case colorSecondary:
		return p.secondary
	case theme.ColorNameForegroundOnPrimary:
		return rgb(255, 255, 255)
	case theme.ColorNameForeground:
		return p.text
	case colorAccent, theme.ColorNameHyperlink:
		return p.accent
	case theme.ColorNamePrimary:
		return p.primary
	case theme.ColorNameSuccess:
		if variant == theme.VariantDark {
			return rgb(136, 207, 164)
		}
		return rgb(43, 124, 95)
	case theme.ColorNameButton, theme.ColorNameInputBackground, theme.ColorNameHeaderBackground:
		return p.button
	case theme.ColorNameDisabledButton:
		return p.disabledButton
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return p.secondary
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return p.border
	case theme.ColorNameSelection:
		return p.selection
	case theme.ColorNameHover, theme.ColorNameFocus:
		return color.NRGBA{R: p.primary.R, G: p.primary.G, B: p.primary.B, A: 25}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: p.secondary.R, G: p.secondary.G, B: p.secondary.B, A: 130}
	}
	return t.Theme.Color(name, variant)
}

func (t *clientTheme) Font(style fyne.TextStyle) fyne.Resource {
	if t.fonts == nil || style.Monospace || style.Symbol {
		return t.Theme.Font(style)
	}
	if style.Bold {
		return t.fonts.semibold
	}
	return t.fonts.regular
}
