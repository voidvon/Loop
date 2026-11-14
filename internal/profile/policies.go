package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const DefaultPolicy = "Available"

// PolicyGroup belongs to the main configuration. Choices belong to local state.
// AllNodes and Filter expand against all sources on every compilation.
type PolicyGroup struct {
	Name     string   `json:"name"`
	AllNodes bool     `json:"all_nodes,omitempty"`
	Filter   string   `json:"filter,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Builtins []string `json:"builtins,omitempty"`
	Nodes    []string `json:"nodes,omitempty"`
	Sources  []string `json:"sources,omitempty"`
}
type PolicyMember struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Detail    string `json:"detail,omitempty"`
	IsGroup   bool   `json:"is_group,omitempty"`
	IsBuiltin bool   `json:"is_builtin,omitempty"`
	Source    string `json:"source,omitempty"`
}
type PolicyState struct {
	Group    PolicyGroup    `json:"group"`
	Members  []PolicyMember `json:"members"`
	Selected string         `json:"selected"`
	Error    string         `json:"error,omitempty"`
}

func DefaultPolicyGroups() []PolicyGroup {
	countries := []string{"HK", "TW", "SG", "JP", "KR", "US"}
	groups := []PolicyGroup{{Name: DefaultPolicy, AllNodes: true}}
	for _, name := range []string{"YouTube", "Telegram"} {
		groups = append(groups, PolicyGroup{Name: name, Groups: append([]string{DefaultPolicy}, countries...), AllNodes: true})
	}
	filters := []string{`(?i)香港|hong.?kong|\bHK\b|🇭🇰`, `(?i)台湾|台灣|taiwan|\bTW\b|🇹🇼`, `(?i)新加坡|狮城|singapore|\bSG\b|🇸🇬`, `(?i)日本|东京|大阪|japan|\bJP\b|🇯🇵`, `(?i)韩国|韓國|首尔|korea|\bKR\b|🇰🇷`, `(?i)美国|美國|united.?states|\bUSA?\b|🇺🇸`}
	for i, name := range countries {
		groups = append(groups, PolicyGroup{Name: name, AllNodes: true, Filter: filters[i]})
	}
	return groups
}

func PolicyGroups(config map[string]any) ([]PolicyGroup, error) {
	raw, ok := config["policy_groups"]
	if !ok {
		return DefaultPolicyGroups(), nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var groups []PolicyGroup
	// Reject misspelled fields rather than silently broadening a node filter.
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&groups); err != nil {
		return nil, fmt.Errorf("policy_groups: %w", err)
	}
	names := map[string]bool{}
	for _, g := range groups {
		if strings.TrimSpace(g.Name) != g.Name || g.Name == "" || g.Name == "direct" || g.Name == "proxy" || g.Name == "reject" || g.Name == "DIRECT" || g.Name == "REJECT" || strings.HasPrefix(g.Name, "node-") || names[g.Name] {
			return nil, fmt.Errorf("策略名称无效或重复：%s", g.Name)
		}
		names[g.Name] = true
		if g.Filter != "" {
			if _, err := regexp.Compile(g.Filter); err != nil {
				return nil, fmt.Errorf("%s 节点筛选：%w", g.Name, err)
			}
		}
		for _, key := range g.Builtins {
			if key != "direct" && key != "reject" {
				return nil, fmt.Errorf("%s：不支持的内置类型 %s", g.Name, key)
			}
		}
		for _, key := range g.Nodes {
			if !strings.HasPrefix(key, "node-") || len(key) != 37 {
				return nil, fmt.Errorf("%s：节点标识无效 %s", g.Name, key)
			}
			if _, err := hex.DecodeString(strings.TrimPrefix(key, "node-")); err != nil {
				return nil, fmt.Errorf("%s：节点标识无效", g.Name)
			}
		}
		for _, id := range g.Sources {
			if strings.TrimSpace(id) == "" {
				return nil, fmt.Errorf("%s：节点来源不能为空", g.Name)
			}
		}
	}
	byName := map[string]PolicyGroup{}
	for _, g := range groups {
		byName[g.Name] = g
	}
	marks := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if marks[name] == 1 {
			return fmt.Errorf("策略组存在循环引用：%s", name)
		}
		if marks[name] == 2 {
			return nil
		}
		g, ok := byName[name]
		if !ok {
			return fmt.Errorf("引用了不存在的策略组：%s", name)
		}
		marks[name] = 1
		for _, ref := range g.Groups {
			if err := visit(ref); err != nil {
				return err
			}
		}
		marks[name] = 2
		return nil
	}
	for _, g := range groups {
		if err := visit(g.Name); err != nil {
			return nil, err
		}
	}
	return groups, nil
}

func NodeKey(source, node string) string {
	data, _ := json.Marshal([]string{source, node})
	sum := sha256.Sum256(data)
	return "node-" + hex.EncodeToString(sum[:16])
}
func (s Store) policyChoices() (map[string]string, error) {
	choices := map[string]string{}
	err := readJSON(filepath.Join(s.Dir, "policy-selections.json"), &choices)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if choices == nil {
		choices = map[string]string{}
	}
	return choices, err
}
func (s Store) PolicyStates() ([]PolicyState, error) {
	config, err := s.Config()
	if err != nil {
		return nil, err
	}
	groups, err := PolicyGroups(config)
	if err != nil {
		return nil, err
	}
	sources, err := s.Sources()
	if err != nil {
		return nil, err
	}
	choices, err := s.policyChoices()
	if err != nil {
		return nil, err
	}
	selection, selectionErr := s.Selection()
	states := []PolicyState{}
	for _, g := range groups {
		state := PolicyState{Group: g, Selected: choices[g.Name]}
		seen := map[string]bool{}
		appendMember := func(m PolicyMember) {
			if !seen[m.Key] {
				state.Members = append(state.Members, m)
				seen[m.Key] = true
			}
		}
		for _, ref := range g.Groups {
			appendMember(PolicyMember{Key: ref, Name: ref, Detail: "策略组", IsGroup: true})
		}
		for _, key := range g.Builtins {
			appendMember(BuiltinMember(key))
		}
		var filter *regexp.Regexp
		if g.Filter != "" {
			filter = regexp.MustCompile(g.Filter)
		}
		selectedNodes, selectedSources := map[string]bool{}, map[string]bool{}
		for _, key := range g.Nodes {
			selectedNodes[key] = true
		}
		for _, id := range g.Sources {
			selectedSources[id] = true
		}
		for _, source := range sources {
			for _, node := range source.Nodes {
				key := NodeKey(source.ID, node.ID)
				if selectedNodes[key] || ((g.AllNodes || selectedSources[source.ID]) && (filter == nil || filter.MatchString(node.Name))) {
					appendMember(PolicyMember{Key: key, Name: node.Name, Detail: source.Name, Source: source.ID})
				}
			}
		}
		if legacyAvailable(g) && state.Selected == "" {
			if selectionErr != nil && !errors.Is(selectionErr, os.ErrNotExist) {
				state.Error = "无法读取 Available 的选择：" + selectionErr.Error()
			} else if selectionErr == nil {
				state.Selected = NodeKey(selection.Source, selection.Node)
			}
		}
		if state.Selected == "" && len(state.Members) > 0 {
			state.Selected = state.Members[0].Key
		}
		found := false
		for _, m := range state.Members {
			found = found || m.Key == state.Selected
		}
		if len(state.Members) == 0 {
			state.Error = "暂无可用子策略"
		} else if !found && state.Error == "" {
			state.Error = "所选节点已不可用，请重新选择"
		}
		states = append(states, state)
	}
	return states, nil
}

func (s Store) StrategyChoice(group string) (string, error) {
	states, err := s.PolicyStates()
	if err != nil {
		return "", err
	}
	for _, state := range states {
		if state.Group.Name == group {
			return state.Selected, nil
		}
	}
	return "", errors.New("策略组不存在")
}
func (s Store) SelectStrategy(group, key string) error {
	states, err := s.PolicyStates()
	if err != nil {
		return err
	}
	found := false
	for _, state := range states {
		if state.Group.Name == group {
			for _, m := range state.Members {
				found = found || m.Key == key
			}
		}
	}
	if !found {
		return errors.New("子策略已不可用，请刷新后重新选择")
	}
	var resolve func(string) error
	resolve = func(name string) error {
		for _, state := range states {
			if state.Group.Name == name {
				if state.Error != "" {
					return fmt.Errorf("%s：%s", name, state.Error)
				}
				return resolve(state.Selected)
			}
		}
		return nil
	}
	if err := resolve(key); err != nil {
		return err
	}

	if group == DefaultPolicy {
		sources, err := s.Sources()
		if err != nil {
			return err
		}
		for _, source := range sources {
			for _, n := range source.Nodes {
				if NodeKey(source.ID, n.ID) == key {
					return s.Select(Selection{source.ID, n.ID})
				}
			}
		}
	}
	choices, err := s.policyChoices()
	if err != nil {
		return err
	}
	// Pin a referenced country's current default when the user chooses it.
	// Later provider reordering must not silently pick a different node.
	var pin func(string)
	pin = func(name string) {
		for _, state := range states {
			if state.Group.Name == name {
				choices[name] = state.Selected
				pin(state.Selected)
				return
			}
		}
	}
	pin(key)
	choices[group] = key
	return WriteJSON(filepath.Join(s.Dir, "policy-selections.json"), choices)
}

// ChangePolicyGroups preserves all DNS/routing fields; validation rejects deletion
// of a group still referenced by a rule, another group, or Final.
func ChangePolicyGroups(data []byte, groups []PolicyGroup) ([]byte, error) {
	config, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	config["policy_groups"] = groups
	validated, err := PolicyGroups(config)
	if err != nil {
		return nil, err
	}
	if err := validatePolicyGroups(config, validated); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	return append(encoded, '\n'), err
}

// UpgradePolicyGroups is idempotent and retains a backup of the user's config.
// Existing routing is preserved; only the former proxy outlet becomes Available.
func (s Store) UpgradePolicyGroups() error {
	config, err := s.Config()
	if err != nil {
		return err
	}
	if _, ok := config["policy_groups"]; ok {
		return nil
	}
	path := filepath.Join(s.Dir, MainConfigName)
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	config["policy_groups"] = DefaultPolicyGroups()
	replaceProxy(config, DefaultPolicy)
	// App domain rules are explicit and only seeded when adopting strategy groups.
	route, ok := config["route"].(map[string]any)
	if !ok {
		return errors.New("配置缺少 route")
	}
	rules, _ := route["rules"].([]any)
	appRules := []any{
		map[string]any{"domain_suffix": []string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtubei.googleapis.com", "youtube-nocookie.com"}, "action": "route", "outbound": "YouTube"},
		map[string]any{"domain_suffix": []string{"telegram.org", "t.me", "telegram.me", "tdesktop.com", "telegra.ph"}, "action": "route", "outbound": "Telegram"},
	}
	route["rules"] = append(rules, appRules...)
	// Telegram publishes these IPv4/IPv6 prefixes at core.telegram.org/resources/cidr.txt.
	// Retain the user's earlier matches and resolve only at the end for this IP rule.
	resolved := false
	for _, rule := range rules {
		if rule, ok := rule.(map[string]any); ok && rule["action"] == "resolve" && unconditionalResolve(rule) {
			resolved = true
		}
	}
	if !resolved {
		route["rules"] = append(route["rules"].([]any), map[string]any{"action": "resolve"})
	}
	route["rules"] = append(route["rules"].([]any), map[string]any{"ip_cidr": []string{"91.108.56.0/22", "91.108.4.0/22", "91.108.8.0/22", "91.108.16.0/22", "91.108.12.0/22", "149.154.160.0/20", "91.105.192.0/23", "91.108.20.0/22", "185.76.151.0/24", "2001:b28:f23d::/48", "2001:b28:f23f::/48", "2001:67c:4e8::/48", "2001:b28:f23c::/48", "2a0a:f280::/32"}, "action": "route", "outbound": "Telegram"})
	if err := writePrivateFile(path+".pre-policies.bak", old); err != nil {
		return err
	}
	return WriteJSON(path, config)
}
func replaceProxy(value any, policy string) {
	if config, ok := value.(map[string]any); ok {
		if route, ok := config["route"].(map[string]any); ok && route["final"] == "proxy" {
			route["final"] = policy
		}
	}

	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			if (k == "outbound" || k == "detour" || k == "download_detour") && x == "proxy" {
				v[k] = policy
			} else {
				replaceProxy(x, policy)
			}
		}
	case []any:
		for _, x := range v {
			replaceProxy(x, policy)
		}
	}
}
