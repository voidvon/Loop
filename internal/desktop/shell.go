package desktop

import (
	"encoding/json"
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

const (
	pageHome = iota
	pageNodes
	pagePolicy
	pageRules
	pageConfig
	pageMore
)

type desktopShell struct {
	root            fyne.CanvasObject
	sidebar         fyne.CanvasObject
	sidebarPadding  *fyne.Container
	content         *fyne.Container
	navigation      []*choiceButton
	pages           []fyne.CanvasObject
	current         int
	refreshers      map[int]func()
	store           profile.Store
	modeSelect      *widget.RadioGroup
	modeHelp        *widget.Label
	syncingMode     bool
	rawConfig       bool
	tunSwitch       *widget.Check
	captureLabel    *widget.Label
	connectionLabel *widget.Label
	running         func() bool
	configPane      *configPane
	rulesPane       *rulesPane
	strategies      *strategyPane
	more            *morePane
	currentPolicy   *widget.Label
	documents       *documentController
	appearance      *fyne.Container
	OnStateChanged  func()
}

func pageHeading(title string) fyne.CanvasObject {
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	heading.SizeName = theme.SizeNameHeadingText
	return heading
}

func wrappedLabel(value string) *widget.Label {
	label := widget.NewLabel(value)
	label.Wrapping = fyne.TextWrapWord
	return label
}

func newDesktopShell(window fyne.Window, service *app.Service, manager *nodeManager, status *widget.Label, toggle *widget.Button, running func() bool, changeMode func(profile.Mode), changeTUN func(bool)) *desktopShell {
	store, rawConfig := service.Store, service.ConfigPath
	shell := &desktopShell{refreshers: map[int]func(){}, store: store, rawConfig: rawConfig != "", running: running}
	shell.captureLabel = quietLabel("系统代理")
	shell.connectionLabel = quietLabel("未连接")
	shell.tunSwitch = widget.NewCheck("启用 TUN 模式", func(enabled bool) {
		if !shell.syncingMode {
			changeTUN(enabled)
		}
	})
	shell.modeHelp = quietLabel("")
	modes := []profile.Mode{profile.ModeDirect, profile.ModeRule, profile.ModeGlobal}
	labels := make([]string, len(modes))
	for i, mode := range modes {
		labels[i] = mode.Label()
	}
	shell.modeSelect = widget.NewRadioGroup(labels, func(label string) {
		if shell.syncingMode {
			return
		}
		for _, mode := range modes {
			if mode.Label() == label {
				changeMode(mode)
				return
			}
		}
	})
	shell.modeSelect.Horizontal = true
	shell.modeSelect.Required = true
	shell.SetModeEnabled(true)
	current := wrappedLabel("完整配置模式：节点由指定配置文件管理。")
	shell.currentPolicy = current
	homePage := shell.homePage(status, toggle, current)
	var nodePage fyne.CanvasObject = wrappedLabel("当前使用完整配置文件，节点由该文件管理。")
	if manager != nil {
		nodePage = manager.root
	} else {
		nodePage = container.NewBorder(pageHeading("节点"), nil, nil, nil, panel(nodePage))
	}

	policyBody := container.NewVBox()
	policyScroll := container.NewVScroll(policyBody)
	shell.refreshers[pagePolicy] = func() {
		policy, err := loadPolicy(store, rawConfig)
		if err != nil {
			policyBody.Objects = []fyne.CanvasObject{wrappedLabel("无法读取策略：" + err.Error())}
		} else {
			policyBody.Objects = policySummary(policy)
			if rawConfig == "" {
				mode, modeErr := store.Mode()
				text := "以下为已保存的自动分流策略。"
				if modeErr != nil {
					text = modeErr.Error()
				} else if mode != profile.ModeRule {
					text = "当前为" + mode.Label() + "；以下策略将在切回自动分流后生效。"
				}
				policyBody.Objects = append([]fyne.CanvasObject{quietLabel(text)}, policyBody.Objects...)
			}
		}
		policyBody.Refresh()
		policyScroll.ScrollToTop()
	}
	var policyPage fyne.CanvasObject = container.NewBorder(pageHeading("策略"), nil, nil, nil, policyScroll)

	shell.documents = newDocumentController(window, service)
	shell.configPane = newConfigPane(window, shell.documents)
	shell.refreshers[pageConfig] = shell.configPane.Open
	configPage := shell.configPane.root
	shell.rulesPane = newRulesPane(window, shell.documents)
	if rawConfig == "" {
		shell.strategies = newStrategyPane(window, shell.documents, current)
		shell.strategies.OnChanged = shell.notifyStateChanged
		policyPage = shell.strategies.root
		shell.refreshers[pagePolicy] = func() { shell.configPane.Refresh(); shell.strategies.Refresh() }
		shell.refreshers[pageHome] = shell.strategies.Refresh
	}

	shell.documents.OnStateChanged = func() {
		shell.configPane.updateStatus()
		shell.rulesPane.updateButtons()
		if shell.strategies != nil {
			shell.strategies.updateButtons()
		}
		shell.notifyStateChanged()
	}
	shell.refreshers[pageRules] = func() {
		shell.configPane.Refresh()
		shell.rulesPane.Refresh()
	}

	shell.more = newMorePane(fyne.CurrentApp().Preferences())
	shell.more.OnChanged = shell.notifyStateChanged
	shell.refreshers[pageMore] = shell.more.Refresh
	shell.pages = []fyne.CanvasObject{homePage, nodePage, policyPage, shell.rulesPane.root, configPage, shell.more.root}
	shell.content = container.NewStack(homePage)
	titles := []string{"首页", "节点", "策略", "规则", "配置", "更多"}
	icons := []fyne.Resource{theme.HomeIcon(), theme.ComputerIcon(), theme.ListIcon(), theme.StorageIcon(), theme.DocumentIcon(), theme.MoreHorizontalIcon()}
	navigation := container.NewVBox()
	for index, title := range titles {
		button := newChoiceButton(title, icons[index], func() { shell.selectPage(index) })
		button.Alignment = widget.ButtonAlignLeading
		shell.navigation = append(shell.navigation, button)
		navigation.Add(container.NewGridWrap(fyne.NewSize(176, 44), button.Button))
	}
	brand := sidebarBrand(shell.connectionLabel)
	top := container.NewVBox(brand, inset(32, 0, 0, 0, navigation))
	shell.appearance = container.NewVBox()
	footer := container.NewVBox(shell.captureLabel, shell.appearance, quietLabel(app.DisplayName+"  /  0.16.14"))
	shell.sidebarPadding = inset(26, 20, 16, 16, container.NewBorder(top, footer, nil, nil, layout.NewSpacer()))
	sidebar := container.NewStack(themedSurface(colorSidebar, 0), shell.sidebarPadding)
	shell.sidebar = sidebar
	shell.root = container.NewBorder(nil, nil, sidebar, nil, inset(8, 16, 20, 20, shell.content))
	shell.selectPage(0)
	return shell
}

func (s *desktopShell) selectPage(index int) {
	if index < 0 || index >= len(s.pages) {
		return
	}
	s.current = index
	for i, button := range s.navigation {
		button.Select(i == index)
	}

	s.RefreshCurrent()
	s.content.Objects = []fyne.CanvasObject{s.pages[index]}
	s.content.Refresh()
}

func (s *desktopShell) RefreshCurrent() {
	s.SyncMode()
	if refresh := s.refreshers[s.current]; refresh != nil {
		refresh()
	}
}

func loadPolicy(store profile.Store, rawConfig string) (map[string]any, error) {
	if rawConfig == "" {
		return store.Config()
	}
	path := rawConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("配置 JSON 无效")
	}
	if value == nil {
		return nil, fmt.Errorf("策略必须是 JSON 对象")
	}
	return value, nil
}

func (s *desktopShell) SyncMode() {
	defer s.notifyStateChanged()
	if s.running() {
		s.connectionLabel.SetText("已连接")
		s.connectionLabel.Importance = widget.SuccessImportance
	} else {
		s.connectionLabel.SetText("未连接")
		s.connectionLabel.Importance = widget.LowImportance
	}
	s.connectionLabel.Refresh()
	s.syncingMode = true
	defer func() { s.syncingMode = false }()
	if s.rawConfig {
		s.captureLabel.SetText("完整配置")
		s.modeSelect.SetSelected("")
		s.modeHelp.SetText("完整配置调试模式：分流由指定文件管理。")
		return
	}
	tun, tunErr := s.store.TUN()
	if tunErr == nil {
		s.tunSwitch.SetChecked(tun)
		if tun {
			s.captureLabel.SetText("TUN 模式")
		} else {
			s.captureLabel.SetText("系统代理模式")
		}
	}
	mode, err := s.store.Mode()
	if err != nil {
		s.modeSelect.SetSelected("")
		s.modeHelp.SetText(err.Error())
		return
	}
	s.modeSelect.SetSelected(mode.Label())
	s.modeHelp.SetText(mode.Description())
}

func (s *desktopShell) SetModeEnabled(enabled bool) {
	defer s.notifyStateChanged()
	if s.more != nil {
		s.more.routing.SetAvailable(enabled)
		s.more.benchmark.SetAvailable(enabled)
	}
	if s.strategies != nil {
		s.strategies.available = enabled
		s.strategies.updateButtons()
	}

	if s.configPane != nil {
		s.configPane.SetAvailable(enabled && !s.running())
	}
	if s.rulesPane != nil {
		s.rulesPane.SetAvailable(enabled && (!s.running() || s.documents.SaveRules != nil))
	}
	if s.strategies != nil {
		s.strategies.updateButtons()
	}
	setEnabled(s.modeSelect, enabled && !s.rawConfig)
	setEnabled(s.tunSwitch, enabled && !s.rawConfig && proxy.TUNAvailable())
}

func (s *desktopShell) notifyStateChanged() {
	if s.OnStateChanged != nil {
		s.OnStateChanged()
	}
}
