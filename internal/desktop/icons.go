package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"strings"
)

// A single 24-point outline family keeps navigation and toolbar actions at the
// same weight. Resources stay vector-based and follow the active theme color.
func outlineIcon(name, shapes string) fyne.Resource {
	for _, tag := range []string{"path", "rect", "circle", "ellipse"} {
		shapes = strings.ReplaceAll(shapes, "<"+tag+" ", "<"+tag+` fill="none" `)
	}
	return theme.NewThemedResource(fyne.NewStaticResource("loop-"+name+".svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="none" stroke="#000" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round">`+shapes+`</g></svg>`)))
}

var (
	iconCloud      = theme.NewThemedResource(fyne.NewStaticResource("loop-cloud.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path fill="#000" d="M6 20a6 6 0 0 1-1-11.9 7 7 0 0 1 13.3-2.5A7 7 0 0 1 18 20z"/></svg>`)))
	iconFlag       = outlineIcon("flag", `<path d="M4 22V4m0 0c5-5 11 5 16 0v12c-5 5-11-5-16 0M9 3v12M15 6v12M4 9c5-5 11 5 16 0"/>`)
	iconFilter     = outlineIcon("filter", `<path d="M4 7h16M7 12h10M10 17h4"/>`)
	iconHome       = outlineIcon("home", `<rect x="3.5" y="3.5" width="6" height="6" rx="1"/><rect x="14.5" y="3.5" width="6" height="6" rx="1"/><rect x="3.5" y="14.5" width="6" height="6" rx="1"/><rect x="14.5" y="14.5" width="6" height="6" rx="1"/>`)
	iconPolicy     = outlineIcon("policy", `<path d="M3 6h8m4 0h6M3 12h3m4 0h11M3 18h11m4 0h3"/><circle cx="13" cy="6" r="2"/><circle cx="8" cy="12" r="2"/><circle cx="16" cy="18" r="2"/>`)
	iconNodes      = outlineIcon("nodes", `<path d="m12 2.5 9 5v9l-9 5-9-5v-9zM3 7.5l9 5 9-5M12 12.5v9M7.5 5l9 5"/>`)
	iconRules      = outlineIcon("rules", `<path d="M3 6h3c5 0 7 12 12 12h3m-4-4 4 4-4 4M3 18h3c2 0 3.5-2 5-5m2-3c1.5-2.5 3-4 5-4h3m-4-4 4 4-4 4"/>`)
	iconConfig     = outlineIcon("config", `<path d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8zM14 3v5h5M8 12h8M8 16h8"/>`)
	iconMore       = outlineIcon("more", `<circle cx="12" cy="12" r="9"/><circle cx="7.5" cy="12" r=".6"/><circle cx="12" cy="12" r=".6"/><circle cx="16.5" cy="12" r=".6"/>`)
	iconGlobe      = outlineIcon("globe", `<circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18M5 6.5h14M5 17.5h14"/>`)
	iconLink       = outlineIcon("link", `<path d="m10 14 4-4M9 16l-1 1a4.2 4.2 0 0 1-6-6l4-4a4.2 4.2 0 0 1 6 0m0 10a4.2 4.2 0 0 0 6 0l4-4a4.2 4.2 0 0 0-6-6l-1 1"/>`)
	iconPower      = outlineIcon("power", `<path d="M12 2v10M6.3 5.3a9 9 0 1 0 11.4 0"/>`)
	iconShield     = outlineIcon("shield", `<path d="m12 2 8 3v6c0 5-3.5 8.5-8 11-4.5-2.5-8-6-8-11V5z"/><path d="m8 11 3 3 5-6"/>`)
	iconMoon       = outlineIcon("moon", `<path d="M20.5 13.2A9 9 0 0 1 10.8 3.5 9 9 0 1 0 20.5 13.2Z"/>`)
	iconAppearance = outlineIcon("appearance", `<circle cx="12" cy="12" r="9"/><path d="M12 3v18M15 4v16M18 6v12"/>`)
	iconSpeed      = outlineIcon("speed", `<path d="M4 19a10 10 0 1 1 16 0M12 12l5-5M5 12H3M7 6 5.5 4.5M17 6l1.5-1.5M19 12h2"/><circle cx="12" cy="12" r="1.5"/>`)
	iconArrow      = outlineIcon("arrow", `<path d="M4 12h16m-6-6 6 6-6 6"/>`)
	iconChevron    = outlineIcon("chevron", `<path d="m9 5 7 7-7 7"/>`)
	iconRefresh    = outlineIcon("refresh", `<path d="M20 5v6h-6M20 11a8 8 0 1 0-1 6"/>`)
	iconAdd        = outlineIcon("add", `<path d="M12 4v16M4 12h16"/>`)
	iconEdit       = outlineIcon("edit", `<path d="m15 4 5 5M4 20l5-1L21 7a2 2 0 0 0-5-5L4 14z"/>`)
	iconTrash      = outlineIcon("trash", `<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>`)
	iconSearch     = outlineIcon("search", `<circle cx="10.5" cy="10.5" r="7"/><path d="m16 16 5 5"/>`)
	iconCheck      = outlineIcon("check", `<path d="m5 12 4 4L20 5"/>`)
	iconClose      = outlineIcon("close", `<path d="m6 6 12 12M18 6 6 18"/>`)
	iconSave       = outlineIcon("save", `<path d="M20 21H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h13l4 4v13a1 1 0 0 1-1 1ZM7 3v6h10V3M7 21v-8h10v8"/>`)
	iconFolder     = outlineIcon("folder", `<path d="M3 7V4h6l3 3h9v13H3zM3 7h18"/>`)
	iconImport     = outlineIcon("import", `<path d="M5 4H3v17h18v-2M14 3h7v7M21 3l-12 12"/>`)
	iconExport     = outlineIcon("export", `<path d="M14 3h7v17H3v-2M3 10h7V3M3 10l11-7"/>`)
	iconUp         = outlineIcon("up", `<path d="M12 21V3m-7 7 7-7 7 7"/>`)
	iconDown       = outlineIcon("down", `<path d="M12 3v18m-7-7 7 7 7-7"/>`)
	iconLoop       = outlineIcon("brand", `<path d="M8.5 7.5a5 5 0 0 1 7.1 0l2.9 2.9a5 5 0 0 1-7.1 7.1l-1.3-1.3M15.5 16.5a5 5 0 0 1-7.1 0l-2.9-2.9a5 5 0 0 1 7.1-7.1l1.3 1.3" stroke-width="3"/>`)
)

var outlineThemeIcons = map[fyne.ThemeIconName]fyne.Resource{
	theme.IconNameHome: iconHome, theme.IconNameComputer: iconNodes,
	theme.IconNameList: iconPolicy, theme.IconNameSettings: iconPolicy,
	theme.IconNameStorage: iconRules, theme.IconNameDocument: iconConfig,
	theme.IconNameMoreHorizontal: iconMore, theme.IconNameMoreVertical: iconMore,
	theme.IconNameMediaPlay: iconPower, theme.IconNameMediaStop: iconPower,
	theme.IconNameNavigateNext: iconChevron, theme.IconNameViewRefresh: iconRefresh,
	theme.IconNameContentAdd: iconAdd, theme.IconNameDocumentCreate: iconAdd,
	theme.IconNameDocumentSave: iconSave, theme.IconNameMailCompose: iconEdit,
	theme.IconNameDelete: iconTrash, theme.IconNameSearch: iconSearch,
	theme.IconNameConfirm: iconCheck, theme.IconNameCancel: iconClose,
	theme.IconNameFolder: iconFolder, theme.IconNameLogin: iconImport,
	theme.IconNameLogout: iconExport, theme.IconNameMoveUp: iconUp,
	theme.IconNameMoveDown: iconDown,
}

func (t *clientTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	if icon, ok := outlineThemeIcons[name]; ok {
		return icon
	}
	return t.Theme.Icon(name)
}
