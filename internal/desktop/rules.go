package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/profile"
)

var basicRuleKinds = []string{"domain", "domain_suffix", "domain_keyword", "ip_cidr", "ip_is_private"}
var basicRuleLabels = []string{"完整域名", "域名后缀", "域名关键词", "IP / CIDR", "私有 IP"}
var ruleTargetLabels = []string{"直连", "默认策略（旧版 proxy）", "拦截"}
var ruleTargets = []string{"direct", "proxy", "reject"}

func basicRuleLabel(kind string) string {
	for i, value := range basicRuleKinds {
		if value == kind {
			return basicRuleLabels[i]
		}
	}
	return "高级规则"
}

func (p *rulesPane) ruleDescription(rule map[string]any) (string, string) {
	if basic, ok := profile.ReadBasicRule(rule); ok {
		value := strings.Join(basic.Values, " · ")
		if basic.Kind == "ip_is_private" {
			value = "局域网与非公网 IP"
		}
		return basicRuleLabel(basic.Kind) + "  →  " + p.targetLabel(basic.Target), value
	}
	if rule["action"] == "resolve" {
		return "域名解析", "将域名解析为 IP，供后续 IP 规则匹配 · 高级规则"
	}
	if rule["rule_set"] != nil {
		return "规则集  →  " + p.targetLabel(fieldText(rule["outbound"])), policyValues(rule["rule_set"]) + " · 高级规则"
	}
	encoded, _ := json.Marshal(rule)
	return "高级规则 · 在配置页编辑", string(encoded)
}

func ruleTargetLabel(target string) string {
	for i, value := range ruleTargets {
		if value == target {
			return ruleTargetLabels[i]
		}
	}
	if target != "" && target != "direct" && target != "reject" {
		return "策略 · " + target
	}
	return target
}

type rulesPane struct {
	root                   fyne.CanvasObject
	window                 fyne.Window
	controller             *documentController
	groups                 *widget.List
	list                   *widget.GridWrap
	cardWidth              float32
	summary, status, empty *widget.Label
	sectionHeader          *infoRow
	final                  *widget.Select
	add, reload            *widget.Button
	syncingFinal           bool
	body                   *fyne.Container
	finalPage, localPage   fyne.CanvasObject
	baseline               []byte
	routing                profile.RoutingRules
	filtered, remoteSets   []int
	selected, section      int
	available              bool
	loadError              error
	targets, targetLabels  []string
	defaultPolicy          string
	contextMenu            *widget.PopUpMenu
}

func newRulesPane(window fyne.Window, controller *documentController) *rulesPane {
	p := &rulesPane{window: window, controller: controller, selected: -1, section: 1, available: true}
	p.buildView()
	p.Refresh()
	p.groups.Select(1)
	return p
}

func (p *rulesPane) Refresh() {
	selectedTag := ""
	if p.section >= 2 && p.section-2 < len(p.remoteSets) {
		selectedTag = policyValues(p.routing.RuleSets[p.remoteSets[p.section-2]]["tag"])
	}
	data, err := os.ReadFile(p.controller.path)
	var routing profile.RoutingRules
	if err == nil {
		routing, err = profile.ReadRoutingRules(data)
	}
	p.filtered = nil
	p.baseline, p.routing, p.loadError = data, routing, err
	p.targets = []string{"direct"}
	p.targetLabels = []string{"直连"}
	p.defaultPolicy = ""
	if config, parseErr := profile.ParseDocument(data); parseErr == nil {
		if p.controller.name == profile.MainConfigName {
			p.defaultPolicy, _ = profile.DefaultPolicyName(config)
		}
		if groups, groupErr := profile.PolicyGroups(config); groupErr == nil {
			for _, group := range groups {
				p.targets = append(p.targets, group.Name)
				p.targetLabels = append(p.targetLabels, "策略 · "+group.Name)
			}
		}
	}
	if p.controller.name == "" {
		p.targets = append(p.targets, "proxy")
		p.targetLabels = append(p.targetLabels, ruleTargetLabel("proxy"))
	}
	p.final.Options = append([]string(nil), p.targetLabels...)
	p.syncFinalSelection()
	p.remoteSets = nil
	for index, set := range routing.RuleSets {
		if set["type"] == "remote" {
			p.remoteSets = append(p.remoteSets, index)
		}
	}
	p.summary.SetText(fmt.Sprintf("%d 个规则集合 · %d 条本地规则", 2+len(p.remoteSets), len(p.localIndexes())))
	p.hideRuleMenu()
	if selectedTag != "" {
		p.section = 1 // A removed subscription returns to the local rules.
		for row, index := range p.remoteSets {
			if policyValues(p.routing.RuleSets[index]["tag"]) == selectedTag {
				p.section = row + 2
				break
			}
		}
	}
	if p.section >= 2+len(p.remoteSets) {
		p.section = 1
	}
	p.groups.Refresh()
	p.groups.Select(p.section)
	p.showSection()
	p.filter()
	p.updateButtons()
}

// Keep actual route indexes, even when subscription references are displayed
// separately. Mutations always retain the configuration's global priority.
func (p *rulesPane) localIndexes() []int {
	var indexes []int
	for i, rule := range p.routing.Rules {
		remote := false
		for _, index := range p.remoteSets {
			for _, tag := range profile.RuleSetTags(p.routing.RuleSets[index]["tag"]) {
				if _, simple := profile.SimpleRuleSetTarget(rule, tag); simple {
					remote = true
				}
			}
		}
		if !remote {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (p *rulesPane) filter() {
	p.selected = -1
	p.filtered = p.localIndexes()
	if p.section >= 2 {
		p.filtered = nil
		if p.section-2 < len(p.remoteSets) {
			p.filtered = []int{p.remoteSets[p.section-2]}
		}
	}
	p.list.UnselectAll()
	p.list.Refresh()
	p.refreshSection()
	p.updateButtons()
}

func (p *rulesPane) writable() bool {
	return p.available && p.controller.rulesAvailable() && p.loadError == nil && !p.controller.dirty() && !p.controller.working && !p.controller.closed
}

func (p *rulesPane) SetAvailable(available bool) { p.available = available; p.updateButtons() }

// Loading or restoring a saved selection must not trigger another write.
func (p *rulesPane) syncFinalSelection() {
	p.syncingFinal = true
	p.final.SetSelected(p.targetLabel(p.routing.Final))
	p.syncingFinal = false
}

func (p *rulesPane) changeFinal(label string) {
	if p.syncingFinal || label == p.targetLabel(p.routing.Final) {
		return
	}
	target := p.targetValue(label)
	if !p.writable() || target == "" {
		p.syncFinalSelection()
		return
	}
	p.commit(profile.RuleChange{Operation: "final", Final: target}, append([]byte(nil), p.baseline...), func(err error) {
		if err != nil {
			p.syncFinalSelection()
			dialog.ShowError(err, p.window)
		}
	})
	p.updateButtons()
}

func (p *rulesPane) updateButtons() {
	if p.add == nil { // Select widgets invoke callbacks during construction.
		return
	}
	writable := p.writable()
	setEnabled(p.add, writable)
	setEnabled(p.final, writable)
	setEnabled(p.reload, !p.controller.working)

	switch {
	case p.loadError != nil:
		p.status.SetText("读取失败：" + p.loadError.Error())
	case p.controller.dirty():
		p.status.SetText("配置页有未保存的修改，请先保存或重新载入，再修改规则。")
	case !p.available || !p.controller.rulesAvailable():
		p.status.SetText("可浏览规则，请等待当前操作完成后修改。")
	default:
		p.status.SetText("")
	}
	if p.status.Text == "" {
		p.status.Hide()
	} else {
		p.status.Show()
	}
}

func (p *rulesPane) commit(change profile.RuleChange, baseline []byte, completed func(error)) {
	if !p.writable() {
		if completed != nil {
			completed(fmt.Errorf("请先保存配置草稿并等待其他操作完成"))
		}
		return
	}
	draft, err := profile.ChangeRouting(baseline, change)
	if err != nil {
		if completed != nil {
			completed(err)
		} else {
			dialog.ShowError(err, p.window)
		}
		return
	}
	p.controller.RunRules(draft, baseline, func(err error) {
		// Also refresh after rollback so forms and the JSON editor share a baseline.
		p.Refresh()
		if err == nil {
			if p.section == 1 && (change.Operation == "move" || change.Operation == "replace") {
				index := change.Index
				if change.Operation == "move" {
					index = change.To
				}
				for row, actual := range p.filtered {
					if actual == index {
						p.selected = index
						p.list.ScrollTo(row)
						break
					}
				}
			}
			p.status.SetText("已保存 · 下次启动生效")
			if p.controller.Running != nil && p.controller.Running() {
				text := "已保存并应用 · 代理已连接 · 原配置已备份为 .bak"
				if p.controller.name == profile.MainConfigName {
					if mode, _ := p.controller.service.Store.Mode(); mode != profile.ModeRule {
						text = "已保存 · 当前连接保持 · 切回自动分流后生效"
					}
				}
				p.status.SetText(text)
			}
		} else {
			p.status.SetText(err.Error())
		}
		p.status.Show()
		if completed != nil {
			completed(err)
		} else if err != nil {
			dialog.ShowError(err, p.window)
		}
	})
}

func (p *rulesPane) editRule(index int) {
	if !p.writable() {
		return
	}
	basic := profile.BasicRule{Kind: "domain_suffix", Target: p.defaultTarget()}
	if index >= 0 {
		if index >= len(p.routing.Rules) {
			return
		}
		var ok bool
		basic, ok = profile.ReadBasicRule(p.routing.Rules[index])
		if !ok {
			return
		}
	}
	baseline := append([]byte(nil), p.baseline...)
	kind := widget.NewSelect(basicRuleLabels, nil)
	kind.SetSelected(basicRuleLabel(basic.Kind))
	// Keep a rule's match phase while editing; changing type is a new rule.
	if index >= 0 {
		kind.Disable()
	}
	values := widget.NewMultiLineEntry()
	values.SetMinRowsVisible(5)
	values.SetPlaceHolder("每行一个值，例如 example.com 或 192.168.0.0/16")
	values.SetText(strings.Join(basic.Values, "\n"))
	kind.OnChanged = func(label string) { setEnabled(values, label != basicRuleLabel("ip_is_private")) }
	kind.OnChanged(kind.Selected)
	target := widget.NewSelect(append(append([]string(nil), p.targetLabels...), "拦截"), nil)
	target.SetSelected(p.targetLabel(basic.Target))
	validation := wrappedLabel("")
	var editor *dialog.CustomDialog
	save := widget.NewButtonWithIcon("检查并保存", theme.DocumentSaveIcon(), func() {
		key := ""
		for i, label := range basicRuleLabels {
			if label == kind.Selected {
				key = basicRuleKinds[i]
			}
		}
		if target.SelectedIndex() < 0 {
			validation.SetText("请选择处理方式")
			return
		}
		rule, err := profile.BuildBasicRule(key, values.Text, p.targetValue(target.Selected), p.targets...)
		if err != nil {
			validation.SetText(err.Error())
			return
		}
		operation := "add"
		if index >= 0 {
			operation = "replace"
		}
		p.commit(profile.RuleChange{Operation: operation, Index: index, Rule: rule}, baseline, func(err error) {
			if err != nil {
				validation.SetText(err.Error())
				return
			}
			editor.Hide()
			p.groups.Select(1)
		})
	})
	save.Importance = widget.HighImportance
	content := container.NewVBox(widget.NewForm(widget.NewFormItem("规则类型", kind), widget.NewFormItem("匹配内容", values), widget.NewFormItem("处理方式", target)), wrappedLabel("同一条规则中的多个值满足任意一个即可。IP 支持 IPv4、IPv6 和网段。DNS 解析策略仍在配置页管理。"), validation, container.NewHBox(save, widget.NewButton("取消", func() { editor.Hide() })))
	title := "添加规则"
	if index >= 0 {
		title = "编辑规则"
	}
	editor = dialog.NewCustomWithoutButtons(title, content, p.window)
	editor.Resize(fyne.NewSize(540, 440))
	editor.Show()
}

func (p *rulesPane) deleteRule() {
	if !p.writable() || p.selected < 0 || p.selected >= len(p.routing.Rules) {
		return
	}
	index, baseline := p.selected, append([]byte(nil), p.baseline...)
	name, value := p.ruleDescription(p.routing.Rules[index])
	message := fmt.Sprintf("删除第 %d 条规则？\n%s\n%s", index+1, name, value)
	if p.routing.Rules[index]["action"] == "resolve" {
		message += "\n删除域名解析步骤可能影响后续 IP 规则。"
	}
	dialog.ShowConfirm("删除规则", message, func(ok bool) {
		if ok {
			p.commit(profile.RuleChange{Operation: "delete", Index: index}, baseline, nil)
		}
	}, p.window)
}

func (p *rulesPane) move(delta int) {
	if !p.writable() || p.selected < 0 || p.selected >= len(p.routing.Rules) {
		return
	}
	p.commit(profile.RuleChange{Operation: "move", Index: p.selected, To: p.selected + delta}, append([]byte(nil), p.baseline...), nil)
}

// Match the compiler's legacy proxy alias without rewriting the source document.
// In an external full configuration, proxy is a literal outbound tag instead.
func (p *rulesPane) resolvedTarget(target string) string {
	if target == "proxy" && p.defaultPolicy != "" {
		return p.defaultPolicy
	}
	return target
}

func (p *rulesPane) targetLabel(target string) string {
	return ruleTargetLabel(p.resolvedTarget(target))
}

func (p *rulesPane) targetValue(label string) string {
	if label == "拦截" {
		return "reject"
	}
	for i, value := range p.targetLabels {
		if value == label {
			return p.targets[i]
		}
	}
	return ""
}

func (p *rulesPane) defaultTarget() string {
	config, err := profile.ParseDocument(p.baseline)
	if err == nil {
		if name, err := profile.DefaultPolicyName(config); err == nil && name != "" {
			return name
		}
	}
	return "direct"
}
