package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s Store) Sources() ([]Subscription, error) {
	var manual NodeList
	if err := readJSON(filepath.Join(s.Dir, "nodes.json"), &manual); err != nil {
		return nil, err
	}
	var subscriptions Subscriptions
	if err := readJSON(filepath.Join(s.Dir, "subscriptions.json"), &subscriptions); err != nil {
		return nil, err
	}
	sources := append([]Subscription{{ID: "manual", Name: "手动节点", Nodes: manual.Nodes}}, subscriptions.Subscriptions...)
	ids := map[string]bool{}
	for _, source := range sources {
		if strings.TrimSpace(source.ID) == "" || ids[source.ID] {
			return nil, fmt.Errorf("节点来源 ID 为空或重复: %q", source.ID)
		}
		ids[source.ID] = true
		nodeIDs := map[string]bool{}
		for _, node := range source.Nodes {
			if strings.TrimSpace(node.ID) == "" || nodeIDs[node.ID] {
				return nil, fmt.Errorf("来源 %s 的节点 ID 为空或重复: %q", source.ID, node.ID)
			}
			nodeIDs[node.ID] = true
			if err := validateNode(node); err != nil {
				return nil, fmt.Errorf("来源 %s: %w", source.ID, err)
			}
		}
	}
	return sources, nil
}

func (s Store) Selection() (Selection, error) {
	var selection Selection
	err := readJSON(filepath.Join(s.Dir, "selection.json"), &selection)
	return selection, err
}

func (s Store) selected(selection Selection) (Node, error) {
	sources, err := s.Sources()
	if err != nil {
		return Node{}, err
	}
	for _, source := range sources {
		if source.ID != selection.Source {
			continue
		}
		for _, node := range source.Nodes {
			if node.ID == selection.Node {
				return node, nil
			}
		}
	}
	return Node{}, fmt.Errorf("找不到所选节点 %q / %q，请重新选择；不会自动切换直连", selection.Source, selection.Node)
}

func (s Store) Select(selection Selection) error {
	if _, err := s.selected(selection); err != nil {
		return err
	}
	states, err := s.PolicyStates()
	if err != nil {
		return err
	}
	key := NodeKey(selection.Source, selection.Node)
	found := false
	for _, state := range states {
		if state.Group.Name != DefaultPolicy {
			continue
		}
		for _, member := range state.Members {
			found = found || member.Key == key
		}
	}
	if !found {
		return errors.New("节点不在 Available 中，请使用策略页或 policy select 选择对应策略的成员")
	}
	choices, err := s.policyChoices()
	if err != nil {
		return err
	}
	choices[DefaultPolicy] = key
	// Keep legacy CLI state in sync; strategy choices remain authoritative.
	path := filepath.Join(s.Dir, "selection.json")
	previous, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := WriteJSON(path, selection); err != nil {
		return err
	}
	if err := WriteJSON(filepath.Join(s.Dir, "policy-selections.json"), choices); err != nil {
		var restoreErr error
		if errors.Is(readErr, os.ErrNotExist) {
			restoreErr = os.Remove(path)
		} else {
			restoreErr = writePrivateFile(path, previous)
		}
		return errors.Join(err, restoreErr)
	}
	return nil
}

var nodeTypes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"hysteria": true, "hysteria2": true, "tuic": true, "anytls": true,
	"socks": true, "http": true, "naive": true, "shadowtls": true, "snell": true,
}

func validateNode(node Node) error {
	typ, _ := node.Outbound["type"].(string)
	if !nodeTypes[typ] {
		return fmt.Errorf("节点 %q 的类型 %q 不受支持", node.ID, typ)
	}
	if detour, ok := node.Outbound["detour"]; ok && detour != "" && detour != nil {
		return fmt.Errorf("节点 %q 依赖 detour，此版本只支持独立节点", node.ID)
	}
	if server, _ := node.Outbound["server"].(string); strings.TrimSpace(server) == "" {
		return fmt.Errorf("节点 %q 缺少 server", node.ID)
	}
	return nil
}
