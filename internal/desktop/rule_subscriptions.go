package desktop

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/profile"
)

func (p *rulesPane) addRuleDialog() {
	if !p.writable() {
		return
	}
	var chooser *dialog.CustomDialog
	local := widget.NewButtonWithIcon("本地规则", theme.DocumentIcon(), func() { chooser.Hide(); p.editRule(-1) })
	remote := widget.NewButtonWithIcon("订阅规则 · sing-box", theme.FolderIcon(), func() { chooser.Hide(); p.editRuleSubscription(-1) })
	chooser = dialog.NewCustom("添加规则", "取消", container.NewVBox(local, remote), p.window)
	chooser.Resize(fyne.NewSize(360, 190))
	chooser.Show()
}

func subscriptionFormatLabel(set map[string]any) string {
	if set["format"] == "binary" {
		return "SRS"
	}
	if set["format"] == "source" {
		return "JSON"
	}
	u, _ := url.Parse(fieldText(set["url"]))
	if u != nil && strings.HasSuffix(strings.ToLower(u.Path), ".srs") {
		return "SRS"
	}
	return "JSON"
}

// Return a target only when this set has exactly one simple route binding.
// Other bindings are preserved and described without inventing a default.
func (p *rulesPane) subscriptionBinding(index int) (string, bool) {
	tags := profile.RuleSetTags(p.routing.RuleSets[index]["tag"])
	if len(tags) != 1 {
		return "", false
	}
	target, count := "", 0
	for _, rule := range p.routing.Rules {
		if profile.ReferencesRuleSet(rule, tags[0]) {
			value, simple := profile.SimpleRuleSetTarget(rule, tags[0])
			if !simple {
				return "", false
			}
			target = value
			count++
		}
	}
	return target, count == 1
}
func (p *rulesPane) subscriptionTarget(index int) string {
	if target, ok := p.subscriptionBinding(index); ok {
		return target
	}
	return "在配置页管理引用"
}

func (p *rulesPane) subscriptionRouteIndex(index int) int {
	if _, ok := p.subscriptionBinding(index); !ok {
		return -1
	}
	tag := profile.RuleSetTags(p.routing.RuleSets[index]["tag"])[0]
	for i, rule := range p.routing.Rules {
		if _, ok := profile.SimpleRuleSetTarget(rule, tag); ok {
			return i
		}
	}
	return -1
}

func (p *rulesPane) editRuleSubscription(index int) {
	if !p.writable() {
		return
	}
	sub := profile.RuleSubscription{Format: "binary", UpdateInterval: "1d", Target: profile.DefaultPolicy, AfterResolve: true}
	editableTarget := true
	if index >= 0 {
		if index >= len(p.routing.RuleSets) {
			return
		}
		set := p.routing.RuleSets[index]
		tags := profile.RuleSetTags(set["tag"])
		if set["type"] != "remote" || len(tags) != 1 {
			return
		}
		sub.Tag, sub.URL = tags[0], fieldText(set["url"])
		if subscriptionFormatLabel(set) == "JSON" {
			sub.Format = "source"
		}
		if value := fieldText(set["update_interval"]); value != "" {
			sub.UpdateInterval = value
		}
		sub.Target, editableTarget = p.subscriptionBinding(index)
	}
	baseline := append([]byte(nil), p.baseline...)
	name, address, interval := widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	name.SetPlaceHolder("例如 geosite-youtube")
	name.SetText(sub.Tag)
	if index >= 0 {
		name.Disable()
	}
	address.SetPlaceHolder("https://example.com/rules.srs")
	address.SetText(sub.URL)
	interval.SetPlaceHolder("例如 6h、1d、7d")
	interval.SetText(sub.UpdateInterval)
	format := widget.NewSelect([]string{"SRS 二进制 (.srs)", "JSON 源格式 (.json)"}, nil)
	format.SetSelectedIndex(0)
	if sub.Format == "source" {
		format.SetSelectedIndex(1)
	}
	address.OnChanged = func(value string) {
		if u, err := url.Parse(value); err == nil {
			switch {
			case strings.HasSuffix(strings.ToLower(u.Path), ".srs"):
				format.SetSelectedIndex(0)
			case strings.HasSuffix(strings.ToLower(u.Path), ".json"):
				format.SetSelectedIndex(1)
			}
		}
	}
	target := widget.NewSelect(append(append([]string(nil), p.targetLabels...), "拦截"), nil)
	target.SetSelected(ruleTargetLabel(sub.Target))
	if !editableTarget {
		target.SetOptions([]string{"复杂引用 · 在配置页修改"})
		target.SetSelectedIndex(0)
		target.Disable()
	}
	phase := widget.NewSelect([]string{"包含 IP · 解析后匹配", "仅域名 · 优先匹配"}, nil)
	phase.SetSelectedIndex(0)
	form := widget.NewForm(widget.NewFormItem("名称", name), widget.NewFormItem("订阅链接", address), widget.NewFormItem("格式", format), widget.NewFormItem("更新间隔", interval), widget.NewFormItem("处理策略", target))
	if index < 0 {
		form.Append("匹配位置", phase)
	}
	validation := wrappedLabel("")
	var editor *dialog.CustomDialog
	save := widget.NewButtonWithIcon("保存", theme.DocumentSaveIcon(), func() {
		current := profile.RuleSubscription{Tag: strings.TrimSpace(name.Text), URL: strings.TrimSpace(address.Text), UpdateInterval: strings.TrimSpace(interval.Text), Format: "binary", AfterResolve: phase.SelectedIndex() == 0}
		if format.SelectedIndex() == 1 {
			current.Format = "source"
		}
		if editableTarget {
			current.Target = p.targetValue(target.Selected)
			if current.Target == "" {
				validation.SetText("请选择处理策略")
				return
			}
		}
		operation := "set-add"
		if index >= 0 {
			operation = "set-replace"
		}
		p.commit(profile.RuleChange{Operation: operation, Index: index, Subscription: current}, baseline, func(err error) {
			if err != nil {
				validation.SetText(err.Error())
				return
			}
			editor.Hide()
			p.groups.Select(2)
		})
	})
	save.Importance = widget.HighImportance
	content := container.NewVBox(form, quietLabel("填写 sing-box 规则集链接，内核连接时下载并按间隔更新。"), validation, container.NewHBox(save, widget.NewButton("取消", func() { editor.Hide() })))
	title := "添加订阅规则"
	if index >= 0 {
		title = "编辑订阅规则"
	}
	editor = dialog.NewCustomWithoutButtons(title, content, p.window)
	editor.Resize(fyne.NewSize(580, 500))
	editor.Show()
}

func (p *rulesPane) deleteRuleSubscription(index int) {
	if !p.writable() || index < 0 || index >= len(p.routing.RuleSets) {
		return
	}
	baseline := append([]byte(nil), p.baseline...)
	tags := profile.RuleSetTags(p.routing.RuleSets[index]["tag"])
	if len(tags) != 1 {
		return
	}
	dialog.ShowConfirm("删除订阅规则", fmt.Sprintf("删除“%s”及其简单路由引用？", tags[0]), func(ok bool) {
		if ok {
			p.commit(profile.RuleChange{Operation: "set-delete", Index: index, Subscription: profile.RuleSubscription{Tag: tags[0]}}, baseline, nil)
		}
	}, p.window)
}
