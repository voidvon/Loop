package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const (
	colorPanel           fyne.ThemeColorName = "loop.panel"
	colorMemberCard      fyne.ThemeColorName = "loop.member-card"
	colorMemberSelected  fyne.ThemeColorName = "loop.member-selected"
	colorMemberBadge     fyne.ThemeColorName = "loop.member-badge"
	colorContent         fyne.ThemeColorName = "loop.content"
	colorSegment         fyne.ThemeColorName = "loop.segment"
	colorSwitch          fyne.ThemeColorName = "loop.switch"
	colorSwitchThumb     fyne.ThemeColorName = "loop.switch-thumb"
	sizeMemberName       fyne.ThemeSizeName  = "loop.member-name"
	sizeMemberDetail     fyne.ThemeSizeName  = "loop.member-detail"
	sizeDashboardValue   fyne.ThemeSizeName  = "loop.dashboard-value"
	sizeDashboardPolicy  fyne.ThemeSizeName  = "loop.dashboard-policy"
	colorSidebar         fyne.ThemeColorName = "loop.sidebar"
	colorOrange          fyne.ThemeColorName = "loop.orange"
	colorSecondary       fyne.ThemeColorName = "loop.secondary"
	colorAccent          fyne.ThemeColorName = "loop.accent"
	colorBadgeDirect     fyne.ThemeColorName = "loop.badge-direct"
	colorBadgeDirectText fyne.ThemeColorName = "loop.badge-direct-text"
	colorBadgeProxy      fyne.ThemeColorName = "loop.badge-proxy"
	colorBadgeProxyText  fyne.ThemeColorName = "loop.badge-proxy-text"
	colorBadgeReject     fyne.ThemeColorName = "loop.badge-reject"
	colorBadgeMuted      fyne.ThemeColorName = "loop.badge-muted"
	appearanceSystem                         = "system"
	appearanceLight                          = "light"
	appearanceDark                           = "dark"
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
			background: rgb(42, 44, 45), panel: rgb(30, 32, 33), sidebar: rgb(43, 45, 47), popup: rgb(42, 44, 46),
			text: rgb(225, 226, 227), secondary: rgb(143, 145, 147), primary: rgb(24, 119, 255), accent: rgb(24, 126, 255), selection: rgb(36, 50, 71),
			orange: rgb(255, 164, 38), button: rgb(49, 51, 53),
			disabledButton: rgb(43, 45, 47), border: rgb(58, 60, 62),
		}
	}
	return clientPalette{
		background: rgb(231, 233, 231), panel: rgb(246, 247, 245), sidebar: rgb(231, 233, 232), popup: rgb(248, 249, 247),
		text: rgb(69, 71, 72), secondary: rgb(124, 127, 128), primary: rgb(0, 122, 255), accent: rgb(0, 122, 255), selection: rgb(211, 222, 235),
		orange: rgb(230, 145, 20), button: rgb(236, 238, 235),
		disabledButton: rgb(226, 228, 226), border: rgb(214, 217, 215),
	}
}

// clientTheme resolves both standard widgets and custom surfaces from one palette.
// In system mode the variant comes from Fyne's live OS appearance notification.
type clientTheme struct {
	fyne.Theme
	mode  string
	fonts *systemFonts
	glass bool // Enabled only by a platform adapter with a native backdrop.
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
	case theme.ColorNameBackground:
		return p.background
	case colorContent:
		if t.glass {
			p.background.A = 20
			if variant == theme.VariantDark {
				p.background.A = 180
			}
		}
		return p.background
	case colorPanel:
		if t.glass {
			p.panel.A = 148
			if variant == theme.VariantDark {
				p.panel.A = 180
			}
		}
		return p.panel
	case colorMemberCard:
		if variant == theme.VariantDark {
			return color.NRGBA{R: 23, G: 25, B: 26, A: 72}
		}
		return color.NRGBA{R: 255, G: 255, B: 255, A: 90}
	case colorMemberSelected:
		if variant == theme.VariantDark {
			return rgb(33, 44, 55)
		}
		return rgb(217, 229, 242)
	case colorMemberBadge:
		if variant == theme.VariantDark {
			return rgb(27, 46, 67)
		}
		return rgb(220, 235, 252)
	case colorBadgeDirect:
		if variant == theme.VariantDark {
			return rgb(32, 59, 49)
		}
		return rgb(226, 243, 233)
	case colorBadgeDirectText:
		if variant == theme.VariantDark {
			return rgb(32, 163, 105)
		}
		return rgb(17, 126, 75)
	case colorBadgeProxy:
		if variant == theme.VariantDark {
			return rgb(64, 48, 35)
		}
		return rgb(251, 237, 222)
	case colorBadgeProxyText:
		if variant == theme.VariantDark {
			return p.orange
		}
		return rgb(163, 92, 6)
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
		if t.glass {
			p.sidebar.A = 24
			if variant == theme.VariantDark {
				p.sidebar.A = 180
			}
		}
		return p.sidebar
	case colorSegment:
		if variant == theme.VariantDark {
			return color.NRGBA{R: 24, G: 26, B: 26, A: 130}
		}
		return color.NRGBA{R: 255, G: 255, B: 251, A: 125}
	case colorSwitch:
		if variant == theme.VariantDark {
			return rgb(70, 73, 74)
		}
		return rgb(196, 199, 198)
	case colorSwitchThumb:
		if variant == theme.VariantDark {
			return rgb(200, 202, 203)
		}
		return rgb(255, 255, 255)
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

func (t *clientTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 13
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 17
	case sizeMemberName:
		return 12
	case sizeMemberDetail:
		return 10
	case sizeDashboardValue:
		return 28
	case sizeDashboardPolicy:
		return 18
	case theme.SizeNameButtonRadius, theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameCardRadius:
		return 20
	case theme.SizeNameSelectionRadius:
		return 10
	}
	return t.Theme.Size(name)
}

func (t *clientTheme) Font(style fyne.TextStyle) fyne.Resource {
	if t.fonts == nil {
		return t.Theme.Font(style)
	}
	if style.Symbol {
		return t.fonts.symbols
	}
	if style.Monospace {
		if style.Bold {
			return t.fonts.monoBold
		}
		return t.fonts.monospace
	}
	if style.Bold {
		return t.fonts.semibold
	}
	return t.fonts.regular
}
