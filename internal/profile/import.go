package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ImportSubscription accepts sing-box JSON exports. Only standalone outbounds
// are extracted: remote route/DNS/inbounds/experimental settings are ignored.
// A subscription refresh replaces its own snapshot, never policy or selection.
func (s Store) ImportSubscription(id, name, path string) (int, error) {
	if id == "" || id == "manual" {
		return 0, errors.New("订阅 ID 不能为空，也不能使用保留名称 manual")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var document struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return 0, fmt.Errorf("订阅需为 sing-box JSON 格式: %w", err)
	}
	var nodes []Node
	ids := map[string]bool{}
	for _, outbound := range document.Outbounds {
		typ, _ := outbound["type"].(string)
		if !nodeTypes[typ] {
			continue // selector/urltest/direct/reject are not nodes.
		}
		tag, _ := outbound["tag"].(string)
		if tag == "" || ids[tag] {
			return 0, fmt.Errorf("订阅节点 tag 为空或重复: %q", tag)
		}
		ids[tag] = true
		node := Node{ID: tag, Name: tag, Outbound: outbound}
		if err := validateNode(node); err != nil {
			return 0, err
		}
		// The selected node will inherit our policy's resolver instead.
		delete(outbound, "domain_resolver")
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return 0, errors.New("订阅中没有可用的独立代理节点；目前支持 sing-box JSON，不支持 Clash YAML 或 Base64 订阅")
	}
	if name == "" {
		name = id
	}
	return len(nodes), s.SaveSubscription(Subscription{ID: id, Name: name, Format: "sing-box", Nodes: nodes})
}
