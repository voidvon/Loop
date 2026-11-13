package profile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func NewID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

func ValidateNode(node Node) error { return validateNode(node) }

func (s Store) SaveManual(node Node) error {
	if node.ID == "" || strings.TrimSpace(node.Name) == "" {
		return errors.New("请填写节点名称")
	}
	if err := validateNode(node); err != nil {
		return err
	}
	var list NodeList
	if err := readJSON(filepath.Join(s.Dir, "nodes.json"), &list); err != nil {
		return err
	}
	delete(node.Outbound, "domain_resolver")
	for i, existing := range list.Nodes {
		if existing.ID == node.ID {
			list.Nodes[i] = node
			return WriteJSON(filepath.Join(s.Dir, "nodes.json"), list)
		}
	}
	list.Nodes = append(list.Nodes, node)
	return WriteJSON(filepath.Join(s.Dir, "nodes.json"), list)
}

func (s Store) SaveSubscription(value Subscription) error {
	if value.ID == "" || value.ID == "manual" || strings.TrimSpace(value.Name) == "" {
		return errors.New("请填写有效的订阅名称和 ID")
	}
	if value.URL != "" {
		if err := ValidateSubscriptionURL(value.URL); err != nil {
			return err
		}
	}
	if len(value.Nodes) == 0 {
		return errors.New("订阅中没有节点，原订阅保持不变")
	}
	ids := map[string]bool{}
	for _, node := range value.Nodes {
		if node.ID == "" || ids[node.ID] {
			return errors.New("订阅节点名称为空或重复")
		}
		ids[node.ID] = true
		if err := validateNode(node); err != nil {
			return err
		}
	}
	selection, err := s.Selection()
	if err != nil {
		return err
	}
	if selection.Source == value.ID && !ids[selection.Node] {
		return errors.New("更新后的订阅不含当前节点，请先选择其他来源再更新；原订阅未修改")
	}
	choices, err := s.policyChoices()
	if err != nil {
		return err
	}
	sources, err := s.Sources()
	if err != nil {
		return err
	}
	for _, source := range sources {
		if source.ID == value.ID {
			for _, node := range source.Nodes {
				if !ids[node.ID] {
					for group, key := range choices {
						if key == NodeKey(source.ID, node.ID) {
							return fmt.Errorf("更新后的订阅不含策略 %s 所选节点，请先在策略页切换；原订阅未修改", group)
						}
					}
				}
			}
		}
	}
	var list Subscriptions
	if err := readJSON(filepath.Join(s.Dir, "subscriptions.json"), &list); err != nil {
		return err
	}
	for i, existing := range list.Subscriptions {
		if existing.ID == value.ID {
			list.Subscriptions[i] = value
			return WriteJSON(filepath.Join(s.Dir, "subscriptions.json"), list)
		}
	}
	list.Subscriptions = append(list.Subscriptions, value)
	return WriteJSON(filepath.Join(s.Dir, "subscriptions.json"), list)
}

func ValidateSubscriptionURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return errors.New("请输入有效的 HTTP 或 HTTPS 订阅地址")
	}
	return nil
}

func (s Store) DeleteManual(id string) error {
	var list NodeList
	if err := readJSON(filepath.Join(s.Dir, "nodes.json"), &list); err != nil {
		return err
	}
	for i, node := range list.Nodes {
		if node.ID == id {
			list.Nodes = append(list.Nodes[:i], list.Nodes[i+1:]...)
			return s.deleteSelected(Selection{"manual", id}, "nodes.json", list)
		}
	}
	return fmt.Errorf("找不到要删除的节点")
}

func (s Store) DeleteSubscription(id string) error {
	var list Subscriptions
	if err := readJSON(filepath.Join(s.Dir, "subscriptions.json"), &list); err != nil {
		return err
	}
	for i, source := range list.Subscriptions {
		if source.ID == id {
			list.Subscriptions = append(list.Subscriptions[:i], list.Subscriptions[i+1:]...)
			return s.deleteSelected(Selection{Source: id}, "subscriptions.json", list)
		}
	}
	return errors.New("找不到要删除的订阅")
}

func (s Store) deleteSelected(target Selection, file string, value any) error {
	selection, err := s.Selection()
	if err != nil {
		return err
	}
	clear := selection.Source == target.Source && (target.Node == "" || selection.Node == target.Node)
	if clear {
		if err := WriteJSON(filepath.Join(s.Dir, "selection.json"), Selection{}); err != nil {
			return err
		}
	}
	if err := WriteJSON(filepath.Join(s.Dir, file), value); err != nil {
		if clear {
			return errors.Join(err, WriteJSON(filepath.Join(s.Dir, "selection.json"), selection))
		}
		return err
	}
	return nil
}
