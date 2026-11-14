package proxy

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	routingContextPattern  = regexp.MustCompile(`\[([0-9]+) [^\]]+\] (.*)`)
	routingMatchPattern    = regexp.MustCompile(`^router: match\[([0-9]+)\]\s*(.*?)\s*=>\s*(.*)$`)
	routingOutboundPattern = regexp.MustCompile(`^outbound/[^\[]+\[(.*)\]: outbound connection to (\S+)$`)
	routingBlockPattern    = regexp.MustCompile(`^outbound/block\[(.*)\]: blocked (?:packet )?connection to (\S+)$`)
)

// Keep only bounded diagnostic fragments for the one authenticated request.
// IDs exclude rule-set downloads, DNS traffic and the readiness connection.
type routingTrace struct {
	mu           sync.Mutex
	config       map[string]any
	pending      string
	id           string
	destination  string
	rule, target string
	rejected     bool
	outputs      []string
}

func (t *routingTrace) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending += string(data)
	for {
		end := strings.IndexByte(t.pending, '\n')
		if end < 0 {
			break
		}
		line := t.pending[:end]
		t.pending = t.pending[end+1:]
		t.line(line)
	}
	if len(t.pending) > 32768 {
		t.pending = ""
	}
	return len(data), nil
}

func (t *routingTrace) line(line string) {
	parts := routingContextPattern.FindStringSubmatch(line)
	if parts == nil {
		return
	}
	id, message := parts[1], parts[2]
	if strings.HasPrefix(message, "inbound/mixed[") && strings.Contains(message, "]: [routing-test] inbound connection to ") {
		if t.id == "" {
			t.id = id
			_, t.destination, _ = strings.Cut(message, "]: [routing-test] inbound connection to ")
		}
	}
	if t.id == "" || t.id != id {
		return
	}
	if match := routingMatchPattern.FindStringSubmatch(message); match != nil {
		index, _ := strconv.Atoi(match[1])
		route, _ := t.config["route"].(map[string]any)
		rules := anySlice(route["rules"])
		if index >= len(rules) {
			return
		}
		rule, _ := rules[index].(map[string]any)
		action, _ := rule["action"].(string)
		if action == "route" || action == "route-options" || action == "bypass" || action == "" {
			host, port, err := net.SplitHostPort(t.destination)
			if err == nil {
				if override := stringField(rule, "override_address"); override != "" {
					host = override
				}
				if number, ok := rule["override_port"].(float64); ok && number > 0 {
					port = strconv.Itoa(int(number))
				}
				t.destination = net.JoinHostPort(host, port)
			}
		}
		if action == "reject" {
			t.rule, t.rejected = match[2], true
		} else if action == "route" || action == "" || action == "bypass" {
			t.rule, t.target = match[2], stringField(rule, "outbound")
		}
		if (t.rejected || t.target != "") && t.rule == "" {
			t.rule = fmt.Sprintf("规则 #%d", index+1)
		}
	}
	if match := routingOutboundPattern.FindStringSubmatch(message); match != nil && strings.EqualFold(match[2], t.destination) && len(t.outputs) < 32 {
		t.outputs = append(t.outputs, match[1])
	}
	if match := routingBlockPattern.FindStringSubmatch(message); match != nil && strings.EqualFold(match[2], t.destination) && len(t.outputs) < 32 {
		t.outputs = append(t.outputs, match[1])
	}
}

func (t *routingTrace) apply(result *RoutingResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	result.Rejected = t.rejected
	if t.rejected {
		result.Rule = t.rule
		if result.Rule == "" {
			result.Rule = "拒绝规则"
		}
		return
	}
	route, _ := t.config["route"].(map[string]any)
	target := t.target
	if target == "" {
		target = stringField(route, "final")
	}
	outs := map[string]map[string]any{}
	for _, value := range anySlice(t.config["outbounds"]) {
		out, _ := value.(map[string]any)
		tag := stringField(out, "tag")
		outs[tag] = out
		if target == "" {
			target = tag
		}
	}
	// Require a real outbound event. Selector defaults alone cannot establish
	// that routing completed (a preceding DNS resolve may have failed).
	for _, actual := range t.outputs {
		chain := routingChain(outs, target, actual, map[string]bool{})
		if len(chain) == 0 {
			continue
		}
		result.Outbound, result.Chain = actual, chain
		result.Direct = outs[actual]["type"] == "direct"
		result.Rejected = outs[actual]["type"] == "block"
		result.Rule = t.rule
		if result.Rule == "" {
			result.Rule = "FINAL"
		}
		return
	}
}

func routingChain(outbounds map[string]map[string]any, tag, actual string, seen map[string]bool) []string {
	if seen[tag] {
		return nil
	}
	seen[tag] = true
	defer delete(seen, tag)
	out := outbounds[tag]
	kind := stringField(out, "type")
	if kind != "selector" && kind != "urltest" {
		if tag == actual && out != nil {
			return []string{tag}
		}
		return nil
	}
	members := anySlice(out["outbounds"])
	if kind == "selector" {
		selected := stringField(out, "default")
		if selected == "" && len(members) > 0 {
			selected, _ = members[0].(string)
		}
		members = []any{selected}
	}
	for _, member := range members {
		key, _ := member.(string)
		if path := routingChain(outbounds, key, actual, seen); len(path) > 0 {
			return append([]string{tag}, path...)
		}
	}
	return nil
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}
