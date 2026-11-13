package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"local/miniproxy/internal/profile"
)

// Fyne maps these two operations to the native system tray. Keep the menu
// model separate from the window, with all callbacks dispatched to Fyne's UI.
type trayTarget interface {
	SetSystemTrayMenu(*fyne.Menu)
	SetSystemTrayIcon(fyne.Resource)
}

type trayState struct {
	Running, Busy, Closing, CanToggle, CanSelect bool
	Mode                                         profile.Mode
	CustomConfig                                 bool
	Policies                                     []profile.PolicyState
	Error                                        string
	Preferences                                  menuBarPreferences
}

type trayActions struct {
	Toggle, Show, Quit func()
	Select             func(group, key string)
}

type menuBar struct {
	target  trayTarget
	read    func() trayState
	post    func(func())
	actions trayActions
	last    []byte
	iconKey string
	closed  bool
}

func newMenuBar(target trayTarget, read func() trayState, post func(func()), actions trayActions) *menuBar {
	t := &menuBar{target: target, read: read, post: post, actions: actions}
	t.Refresh()
	return t
}

func (t *menuBar) Refresh() {
	if t.closed {
		return
	}
	state := t.read()
	snapshot, _ := json.Marshal(state)
	if bytes.Equal(snapshot, t.last) {
		return
	}
	t.last = snapshot
	// Install the menu before the icon, as required by Fyne's tray interface.
	t.target.SetSystemTrayMenu(t.menu(state))
	key := fmt.Sprintf("%s-%t-%t-%t", state.Mode, state.Running, state.Preferences.ShowModeLetter, state.Preferences.DimDisconnected)
	if t.iconKey != key {
		t.iconKey = key
		t.target.SetSystemTrayIcon(trayIconWithPreferences(state.Mode, state.Running, state.Preferences))
	}
}

func (t *menuBar) Close() { t.closed = true }

// A native menu can still deliver an action queued before a refresh. Re-read
// availability and resolve stable group/node keys at execution time.
func (t *menuBar) action(run func(trayState)) func() {
	return func() {
		t.post(func() {
			if !t.closed {
				state := t.read()
				if !state.Closing {
					run(state)
				}
			}
		})
	}
}

func (t *menuBar) menu(state trayState) *fyne.Menu {
	toggle := fyne.NewMenuItem("启用代理", t.action(func(live trayState) {
		if live.CanToggle && t.actions.Toggle != nil {
			t.actions.Toggle()
		}
	}))
	toggle.Checked = state.Running
	toggle.Disabled = !state.CanToggle || state.Closing
	if state.Busy {
		toggle.Label = "正在处理…"
	}
	items := []*fyne.MenuItem{toggle}
	if state.Error != "" || state.CustomConfig || state.Preferences.ShowPolicies {
		items = append(items, fyne.NewMenuItemSeparator())
	}
	if state.Error != "" {
		item := fyne.NewMenuItem("策略读取失败 · 请在主窗口检查", nil)
		item.Disabled = true
		items = append(items, item)
	} else if state.CustomConfig {
		item := fyne.NewMenuItem("自定义配置 · 策略在配置中管理", nil)
		item.Disabled = true
		items = append(items, item)
	} else if state.Preferences.ShowPolicies && len(state.Policies) == 0 {
		item := fyne.NewMenuItem("暂无策略", nil)
		item.Disabled = true
		items = append(items, item)
	} else if state.Preferences.ShowPolicies {
		for _, policy := range state.Policies {
			selected := "未选择"
			for _, member := range policy.Members {
				if member.Key == policy.Selected {
					selected = member.Name
					break
				}
			}
			if policy.Error != "" {
				selected = "需要重新选择"
			}
			group := fyne.NewMenuItem(policy.Group.Name+" · "+selected, nil)
			group.Disabled = !state.CanSelect || state.Closing
			var members []*fyne.MenuItem
			for _, member := range policy.Members {
				label := member.Name
				if member.IsGroup {
					label = "策略 · " + label
				} else if member.Detail != "" {
					label += "  ·  " + member.Detail
				}
				groupName, key := policy.Group.Name, member.Key
				item := fyne.NewMenuItem(label, t.action(func(live trayState) {
					if live.Preferences.ShowPolicies && live.CanSelect && trayHasMember(live.Policies, groupName, key) && t.actions.Select != nil {
						t.actions.Select(groupName, key)
					}
				}))
				item.Checked = member.Key == policy.Selected
				item.Disabled = !state.CanSelect || !trayHasMember(state.Policies, groupName, key) || state.Closing
				members = append(members, item)
			}
			if len(members) == 0 {
				item := fyne.NewMenuItem("暂无可用节点", nil)
				item.Disabled = true
				members = append(members, item)
			}
			group.ChildMenu = fyne.NewMenu(policy.Group.Name, members...)
			items = append(items, group)
		}
	}
	show := fyne.NewMenuItem("显示主窗口", t.action(func(trayState) {
		if t.actions.Show != nil {
			t.actions.Show()
		}
	}))
	quit := fyne.NewMenuItem("退出 MiniProxy", t.action(func(trayState) {
		if t.actions.Quit != nil {
			t.actions.Quit()
		}
	}))
	// Mark explicitly so Fyne does not append a second, unguarded Quit action.
	quit.IsQuit = true
	show.Disabled, quit.Disabled = state.Closing, state.Closing
	items = append(items, fyne.NewMenuItemSeparator(), show, quit)
	return fyne.NewMenu("MiniProxy", items...)
}

func trayHasMember(policies []profile.PolicyState, group, key string) bool {
	for _, policy := range policies {
		if policy.Group.Name != group {
			continue
		}
		for _, member := range policy.Members {
			if member.Key != key {
				continue
			}
			if member.IsGroup {
				for _, child := range policies {
					if child.Group.Name == key {
						return child.Error == ""
					}
				}
				return false
			}
			return true
		}
	}
	return false
}

func trayModeLetter(mode profile.Mode) string {
	switch mode {
	case profile.ModeGlobal:
		return "G"
	case profile.ModeDirect:
		return "D"
	default:
		return "R"
	}
}

// Filled vector glyphs stay sharp at Retina scale without a font dependency.
// macOS templates use the alpha mask and automatically follow menu-bar colors.
func trayIcon(mode profile.Mode, running bool) fyne.Resource {
	return trayIconWithPreferences(mode, running, defaultMenuBarPreferences())
}

func trayIconWithPreferences(mode profile.Mode, running bool, prefs menuBarPreferences) fyne.Resource {
	letter := trayModeLetter(mode)
	glyph := map[string]string{
		"G": `M23 15l-1.5 1.2c-.6-.9-1.2-1.2-2-1.2-1.4 0-2.3 1.2-2.3 3s.9 3 2.3 3c.6 0 1.1-.1 1.5-.4V19h-1.8v-1.8H23v4.4c-1 .9-2.1 1.3-3.5 1.3-2.7 0-4.4-1.9-4.4-4.9s1.7-4.9 4.4-4.9c1.5 0 2.7.6 3.5 1.9z`,
		"R": `M15 13h3.8c2.4 0 3.8 1.2 3.8 3.2 0 1.4-.7 2.3-1.8 2.8l2.3 3.8h-2.4l-2-3.4H17v3.4h-2V13zm2 1.9v2.6h1.6c1.3 0 1.9-.4 1.9-1.3s-.6-1.3-1.9-1.3H17z`,
		"D": `M15 13h3.1c3.2 0 4.9 1.8 4.9 4.9s-1.7 4.9-4.9 4.9H15V13zm2 1.9v6h1c2 0 2.9-1 2.9-3s-.9-3-2.9-3h-1z`,
	}[letter]
	colorName := colorTrayActive
	if !running && prefs.DimDisconnected {
		colorName = colorTrayInactive
	}
	// Fyne rewrites fill opacity while theming SVGs, so encode state in its
	// theme color's alpha rather than an SVG opacity attribute that gets lost.
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M2 2h16v10h-2V4H4v9h9v2H2V2zM0 16h13v2H0z"/><path d="` + strings.TrimSpace(glyph) + `"/></svg>`
	if !prefs.ShowModeLetter {
		letter = "plain"
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path d="M3 4h18v14H3V4zm2 2v10h14V6H5zM1 19h22v2H1z"/></svg>`
	}
	return theme.NewColoredResource(fyne.NewStaticResource("miniproxy-tray-"+letter+".svg", []byte(svg)), colorName)
}

func (s *desktopShell) menuBarState(busy, closing, toggleEnabled bool) trayState {
	state := trayState{Running: s.running(), Busy: busy || s.documents.working, Closing: closing, CustomConfig: s.rawConfig,
		CanToggle:   !busy && !s.documents.working && !closing && toggleEnabled && !s.documents.closed,
		Preferences: loadMenuBarPreferences(s.more.preferences)}
	if s.strategies != nil {
		state.CanSelect = !busy && !closing && s.strategies.canSelect()
	}
	if s.rawConfig {
		state.Mode = profile.ModeRule
		return state
	}
	mode, err := s.store.Mode()
	state.Mode = mode
	if err != nil {
		state.Error = err.Error()
	}
	states, err := s.store.PolicyStates()
	state.Policies = states
	if err != nil {
		state.Error = err.Error()
	}
	return state
}
