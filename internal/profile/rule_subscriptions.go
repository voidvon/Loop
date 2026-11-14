package profile

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// RuleSubscription describes a single remote sing-box rule-set. Its contents
// remain managed by the core, independently of node subscriptions.
type RuleSubscription struct {
	Tag, URL, Format, UpdateInterval, Target string
	AfterResolve                             bool
}

func RuleSetTags(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		var tags []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				tags = append(tags, s)
			}
		}
		return tags
	}
	return nil
}

// SimpleRuleSetTarget excludes extra match/action fields, so a form cannot
// silently simplify a complex rule or change the behavior of another set.
func SimpleRuleSetTarget(rule map[string]any, tag string) (string, bool) {
	tags := RuleSetTags(rule["rule_set"])
	if len(tags) != 1 || tags[0] != tag {
		return "", false
	}
	for key := range rule {
		if key != "rule_set" && key != "action" && key != "outbound" {
			return "", false
		}
	}
	if rule["action"] == "reject" {
		return "reject", true
	}
	if action := rule["action"]; action != nil && action != "route" {
		return "", false
	}
	target, ok := rule["outbound"].(string)
	return target, ok && target != ""
}

func ReferencesRuleSet(value any, tag string) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "rule_set" {
				for _, candidate := range RuleSetTags(child) {
					if candidate == tag {
						return true
					}
				}
			}
			if ReferencesRuleSet(child, tag) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if ReferencesRuleSet(child, tag) {
				return true
			}
		}
	}
	return false
}

func validateRuleSubscription(sub RuleSubscription) error {
	if sub.Tag == "" || len(sub.Tag) > 128 || strings.IndexFunc(sub.Tag, unicode.IsSpace) >= 0 || strings.IndexFunc(sub.Tag, unicode.IsControl) >= 0 {
		return errors.New("名称不能为空、包含空白或超过 128 字节")
	}
	parsed, err := url.Parse(sub.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("请输入有效的 HTTP / HTTPS 规则集链接")
	}
	if sub.Format != "source" && sub.Format != "binary" {
		return errors.New("规则格式请选择 JSON 或 SRS")
	}
	interval := sub.UpdateInterval
	if strings.HasSuffix(interval, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(interval, "d"))
		if err == nil && days > 0 && days <= 3650 {
			return nil
		}
	} else if duration, err := time.ParseDuration(interval); err == nil && duration > 0 {
		return nil
	}
	return errors.New("更新间隔请填写正数时长，例如 6h、1d 或 7d")
}

func changeRuleSubscription(config, route map[string]any, current RoutingRules, change RuleChange) error {
	sub := change.Subscription
	sets := current.RuleSets
	index := change.Index
	if change.Operation != "set-add" {
		if index < 0 || index >= len(sets) {
			return errors.New("规则订阅已变化，请重新载入")
		}
		if sets[index]["type"] != "remote" {
			return errors.New("此入口仅支持远程 sing-box 规则集")
		}
		tags := RuleSetTags(sets[index]["tag"])
		if len(tags) != 1 {
			return errors.New("多标签规则集请在配置页管理")
		}
		if sub.Tag != "" && sub.Tag != tags[0] {
			return errors.New("订阅名称不可修改，以免破坏已有引用")
		}
		sub.Tag = tags[0]
	}
	if change.Operation == "set-delete" {
		// A DNS or logical rule may rely on this definition. Never remove those
		// dependencies or broaden their conditions as a side effect of deletion.
		for key, value := range config {
			if key != "route" && ReferencesRuleSet(value, sub.Tag) {
				return fmt.Errorf("%s 仍引用规则集 %s，请先在配置页移除引用", key, sub.Tag)
			}
		}
		var kept []map[string]any
		for _, rule := range current.Rules {
			if ReferencesRuleSet(rule, sub.Tag) {
				if _, ok := SimpleRuleSetTarget(rule, sub.Tag); !ok {
					return errors.New("复杂路由规则仍引用此订阅，请先在配置页修改")
				}
			} else {
				kept = append(kept, rule)
			}
		}
		if kept == nil {
			kept = []map[string]any{}
		}
		route["rules"] = kept
		route["rule_set"] = append(sets[:index], sets[index+1:]...)
		return nil
	}
	if err := validateRuleSubscription(sub); err != nil {
		return err
	}
	for i, set := range sets {
		if change.Operation == "set-replace" && i == index {
			continue
		}
		for _, tag := range RuleSetTags(set["tag"]) {
			if tag == sub.Tag {
				return errors.New("规则集名称已存在")
			}
		}
	}
	rules := current.Rules
	if change.Operation == "set-add" {
		if sub.Target == "" {
			return errors.New("请选择处理策略")
		}
		rule := map[string]any{"rule_set": sub.Tag, "action": "route", "outbound": sub.Target}
		if sub.Target == "reject" {
			delete(rule, "outbound")
			rule["action"] = "reject"
		}
		position := 0
		if sub.AfterResolve {
			found := false
			for i, r := range rules {
				if r["action"] == "resolve" && unconditionalResolve(r) {
					position, found = i+1, true
					break
				}
			}
			if !found {
				rules = append([]map[string]any{{"action": "resolve"}}, rules...)
				position = 1
			}
		}
		rules = append(rules, nil)
		copy(rules[position+1:], rules[position:])
		rules[position] = rule
		// Explicit download routing follows the configured default strategy.
		defaultPolicy, err := DefaultPolicyName(config)
		if err != nil {
			return err
		}
		if defaultPolicy == "" {
			return errors.New("请先在策略页设置默认策略，再添加订阅规则")
		}
		set := map[string]any{"type": "remote", "tag": sub.Tag, "url": sub.URL, "format": sub.Format, "update_interval": sub.UpdateInterval, "http_client": map[string]any{"detour": defaultPolicy}}
		sets = append(sets, set)
	} else {
		set := sets[index]
		// A changed URL/format must not keep serving the previous bundled snapshot.
		if set["url"] != sub.URL || ruleSetFormat(set) != sub.Format {
			delete(set, "initial_path")
		}
		set["url"], set["format"], set["update_interval"] = sub.URL, sub.Format, sub.UpdateInterval
		if sub.Target != "" {
			matches := 0
			for _, rule := range rules {
				if ReferencesRuleSet(rule, sub.Tag) {
					if _, ok := SimpleRuleSetTarget(rule, sub.Tag); !ok {
						return errors.New("复杂路由引用的处理策略请在配置页修改")
					}
					matches++
				}
			}
			if matches != 1 {
				return errors.New("此订阅有零个或多个路由引用，请在配置页修改处理策略")
			}
			for _, rule := range rules {
				if _, ok := SimpleRuleSetTarget(rule, sub.Tag); ok {
					if sub.Target == "reject" {
						delete(rule, "outbound")
						rule["action"] = "reject"
					} else {
						rule["action"], rule["outbound"] = "route", sub.Target
					}
				}
			}
		}
	}
	route["rules"], route["rule_set"] = rules, sets
	return nil
}

func ruleSetFormat(set map[string]any) string {
	if format, ok := set["format"].(string); ok {
		return format
	}
	u, _ := url.Parse(fmt.Sprint(set["url"]))
	if u != nil && strings.HasSuffix(strings.ToLower(u.Path), ".srs") {
		return "binary"
	}
	return "source"
}
