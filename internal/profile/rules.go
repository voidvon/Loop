package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// BasicRule contains exactly one match condition. More complex rules remain
// intact in the JSON configuration and are never simplified by the form.
type BasicRule struct {
	Kind, Target string
	Values       []string
}

func BuildBasicRule(kind, input, target string, groups ...string) (map[string]any, error) {
	valid := target == "direct" || target == "proxy" || target == "reject"
	for _, g := range DefaultPolicyGroups() {
		valid = valid || target == g.Name
	}
	for _, g := range groups {
		valid = valid || target == g
	}
	if !valid {
		return nil, errors.New("请选择直连、策略组或拦截")
	}
	result := map[string]any{"action": "route", "outbound": target}
	if target == "reject" {
		result = map[string]any{"action": "reject"}
	}
	if kind == "ip_is_private" {
		result[kind] = true
		return result, nil
	}
	if kind != "domain" && kind != "domain_suffix" && kind != "domain_keyword" && kind != "ip_cidr" {
		return nil, errors.New("不支持的规则类型")
	}
	var values []string
	seen := map[string]bool{}
	for _, value := range strings.FieldsFunc(input, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == '，' }) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if kind == "ip_cidr" {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				addr, addrErr := netip.ParseAddr(value)
				if addrErr != nil || addr.Zone() != "" {
					return nil, fmt.Errorf("无效的 IP 或 CIDR：%s", value)
				}
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			value = prefix.Masked().String()
		} else {
			if strings.ContainsAny(value, "/:*?#@\\") || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
				return nil, fmt.Errorf("请填写域名或关键词，不要包含 URL、端口或通配符：%s", value)
			}
			value = strings.ToLower(value)
			if kind != "domain_keyword" {
				value = strings.TrimSuffix(value, ".")
				leadingDot := kind == "domain_suffix" && strings.HasPrefix(value, ".")
				name := value
				if leadingDot {
					name = strings.TrimPrefix(name, ".")
				}
				ascii, err := idna.Lookup.ToASCII(name)
				if err != nil || ascii == "" || len(ascii) > 253 {
					return nil, fmt.Errorf("无效的域名：%s", value)
				}
				for _, label := range strings.Split(ascii, ".") {
					if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
						return nil, fmt.Errorf("无效的域名：%s", value)
					}
				}
				value = ascii
				if leadingDot {
					value = "." + value
				}
			}
		}
		if !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return nil, errors.New("请至少填写一个匹配值")
	}
	result[kind] = values
	return result, nil
}

func ReadBasicRule(rule map[string]any) (BasicRule, bool) {
	basic := BasicRule{}
	basic.Target, _ = rule["outbound"].(string)
	if rule["action"] == "reject" {
		basic.Target = "reject"
	} else if action := rule["action"]; action != nil && action != "route" {
		return basic, false
	}
	if basic.Target == "" {
		return basic, false
	}
	for key, value := range rule {
		if key == "action" || key == "outbound" {
			continue
		}
		if basic.Kind != "" {
			return basic, false
		}
		switch key {
		case "ip_is_private":
			if value != true {
				return basic, false
			}
		case "domain", "domain_suffix", "domain_keyword", "ip_cidr":
			switch values := value.(type) {
			case string:
				basic.Values = []string{values}
			case []any:
				for _, item := range values {
					text, ok := item.(string)
					if !ok {
						return basic, false
					}
					basic.Values = append(basic.Values, text)
				}
			case []string:
				basic.Values = append([]string(nil), values...)
			default:
				return basic, false
			}
			if len(basic.Values) == 0 {
				return basic, false
			}
		default:
			return basic, false
		}
		basic.Kind = key
	}
	return basic, basic.Kind != ""
}

type RoutingRules struct {
	Final    string
	Rules    []map[string]any
	RuleSets []map[string]any
}

func routingDocument(data []byte) (map[string]any, map[string]any, RoutingRules, error) {
	var result RoutingRules
	config, err := ParseDocument(data)
	if err != nil {
		return nil, nil, result, err
	}
	route, ok := config["route"].(map[string]any)
	if !ok {
		return nil, nil, result, errors.New("配置缺少 route 对象，请先在配置页修正")
	}
	result.Final, _ = route["final"].(string)
	if value, exists := route["rule_set"]; exists {
		sets, ok := value.([]any)
		if !ok {
			return nil, nil, result, errors.New("route.rule_set 必须为数组")
		}
		for _, value := range sets {
			set, ok := value.(map[string]any)
			if !ok {
				return nil, nil, result, errors.New("规则集定义必须为对象")
			}
			result.RuleSets = append(result.RuleSets, set)
		}
	}
	if value, exists := route["rules"]; exists {
		rules, ok := value.([]any)
		if !ok {
			return nil, nil, result, errors.New("route.rules 必须为数组，请先在配置页修正")
		}
		for i, value := range rules {
			rule, ok := value.(map[string]any)
			if !ok {
				return nil, nil, result, fmt.Errorf("第 %d 条规则不是对象", i+1)
			}
			result.Rules = append(result.Rules, rule)
		}
	}
	return config, route, result, nil
}

func ReadRoutingRules(data []byte) (RoutingRules, error) {
	_, _, rules, err := routingDocument(data)
	return rules, err
}

type RuleChange struct {
	Operation    string // final, add, replace, delete, move, set-add, set-replace, set-delete
	Index, To    int
	Final        string
	Rule         map[string]any
	Subscription RuleSubscription
}

// ChangeRouting changes only route.final / route.rules / route.rule_set. Indices refer to the
// complete, unfiltered list. Unknown fields and advanced rules survive intact.
func ChangeRouting(data []byte, change RuleChange) ([]byte, error) {
	config, route, current, err := routingDocument(data)
	if err != nil {
		return nil, err
	}
	rules := current.Rules
	validIndex := func(index int) bool { return index >= 0 && index < len(rules) }
	switch change.Operation {
	case "set-add", "set-replace", "set-delete":
		if err := changeRuleSubscription(config, route, current, change); err != nil {
			return nil, err
		}
	case "final":

		route["final"] = change.Final
	case "add", "replace":
		if _, ok := ReadBasicRule(change.Rule); !ok {
			return nil, errors.New("此表单只支持单条件的常规规则，高级规则请在配置页编辑")
		}
		if change.Operation == "replace" {
			if !validIndex(change.Index) {
				return nil, errors.New("规则位置已失效，请重新载入")
			}
			if _, ok := ReadBasicRule(rules[change.Index]); !ok {
				return nil, errors.New("高级规则不能通过常规表单覆盖")
			}
			rules[change.Index] = change.Rule
		} else {
			index := 0
			// IP rules need resolved destination addresses. Place new ones after
			// an unconditional resolve step; create one when it is absent.
			if change.Rule["ip_cidr"] != nil || change.Rule["ip_is_private"] != nil {
				found := false
				for i, rule := range rules {
					if rule["action"] == "resolve" && unconditionalResolve(rule) {
						index, found = i+1, true
						break
					}
				}
				if !found {
					rules = append([]map[string]any{{"action": "resolve"}}, rules...)
					index = 1
				}
			}
			rules = append(rules, nil)
			copy(rules[index+1:], rules[index:])
			rules[index] = change.Rule
		}
		route["rules"] = rules
	case "delete":
		if !validIndex(change.Index) {
			return nil, errors.New("规则位置已失效，请重新载入")
		}
		route["rules"] = append(rules[:change.Index], rules[change.Index+1:]...)
	case "move":
		if !validIndex(change.Index) || !validIndex(change.To) {
			return nil, errors.New("规则位置已失效，请重新载入")
		}
		moved := rules[change.Index]
		rules = append(rules[:change.Index], rules[change.Index+1:]...)
		rules = append(rules, nil)
		copy(rules[change.To+1:], rules[change.To:])
		rules[change.To] = moved
		route["rules"] = rules
	default:
		return nil, errors.New("不支持的规则操作")
	}
	// Normalize typed slices introduced by form changes before traversing refs.
	// The validator deliberately consumes the same JSON shape as loaded files.
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	validated, err := ParseDocument(encoded)
	if err != nil {
		return nil, err
	}
	groups, err := PolicyGroups(validated)
	if err != nil {
		return nil, err
	}
	if err := validatePolicyGroups(validated, groups); err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func unconditionalResolve(rule map[string]any) bool {
	for key := range rule {
		switch key {
		case "action", "server", "strategy", "disable_cache", "disable_optimistic_cache", "rewrite_ttl", "timeout", "client_subnet":
		default:
			return false
		}
	}
	return true
}
