package desktop

import (
	"encoding/json"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func policyDestination(value any) string {
	switch value {
	case "direct":
		return "直连"
	case "proxy":
		return "Available 默认策略"
	case nil, "":
		return "未指定"
	default:
		return fmt.Sprint(value)
	}
}

func policySummary(policy map[string]any) []fyne.CanvasObject {
	route := mapValue(policy["route"])
	rules, _ := route["rules"].([]any)
	dns := mapValue(policy["dns"])
	dnsRules, _ := dns["rules"].([]any)
	sets, _ := route["rule_set"].([]any)
	summary := sectionCard("分流概况", "", container.NewVBox(
		wrappedLabel("默认出口 · "+policyDestination(route["final"])),
		wrappedLabel(fmt.Sprintf("%d 条分流规则 · %d 个规则集 · %d 条 DNS 规则", len(rules), len(sets), len(dnsRules))),
		wrappedLabel("策略独立保存，切换节点或更新订阅时保持不变。"),
	))
	items := container.NewVBox()
	for i, value := range rules {
		rule := mapValue(value)
		action := policyDestination(rule["outbound"])
		if rule["action"] == "resolve" {
			action = "解析域名"
		} else if rule["action"] == "reject" {
			action = "拦截"
		} else if rule["action"] != nil && rule["action"] != "route" {
			action = fieldText(rule["action"])
		}
		details := map[string]any{}
		for key, value := range rule {
			if key != "action" && key != "outbound" {
				details[key] = value
			}
		}
		condition := "所有请求"
		if len(details) > 0 {
			data, _ := json.Marshal(details)
			condition = string(data)
		}
		if len(details) == 1 {
			switch {
			case rule["ip_is_private"] == true:
				condition = "局域网与私有 IP"
			case rule["domain_suffix"] != nil:
				condition = "域名后缀 · " + policyValues(rule["domain_suffix"])
			case rule["rule_set"] != nil:
				condition = "规则集 · " + policyValues(rule["rule_set"])
			}
		}
		items.Add(wrappedLabel(fmt.Sprintf("%02d  %s", i+1, condition)))
		items.Add(wrappedLabel("      → " + action))
		if i < len(rules)-1 {
			items.Add(widget.NewSeparator())
		}
	}
	if len(rules) == 0 {
		items.Add(wrappedLabel("没有额外规则，使用默认出口。"))
	}
	dnsJSON, _ := json.MarshalIndent(dns, "", "  ")
	dnsView := widget.NewRichText(&widget.TextSegment{Text: string(dnsJSON), Style: widget.RichTextStyleCodeBlock})
	dnsView.Wrapping = fyne.TextWrapWord
	return []fyne.CanvasObject{summary, sectionCard("分流顺序", "", items), sectionCard("DNS 策略", "", dnsView)}
}

func policyValues(value any) string {
	if list, ok := value.([]any); ok {
		result := make([]string, len(list))
		for i, item := range list {
			result[i] = fieldText(item)
		}
		return strings.Join(result, "、")
	}
	return fieldText(value)
}
