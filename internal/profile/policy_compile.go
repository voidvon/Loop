package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func validatePolicyGroups(config map[string]any, groups []PolicyGroup) error {
	allowed := map[string]bool{"proxy": true, "direct": true}
	for _, g := range groups {
		allowed[g.Name] = true
	}
	var visit func(any) error
	visit = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "outbound" || key == "detour" || key == "download_detour" {
					tag, ok := child.(string)
					if !ok || !allowed[tag] {
						return fmt.Errorf("%s 只能引用策略组或 direct，不能绑定具体节点：%v", key, child)
					}
				}
				if err := visit(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(config); err != nil {
		return err
	}
	route, ok := config["route"].(map[string]any)
	if !ok {
		return fmt.Errorf("config.json 必须有 route 对象")
	}
	final, _ := route["final"].(string)
	if !allowed[final] {
		return fmt.Errorf("route.final 必须为策略组或 direct")
	}
	return nil
}

// compileStrategies emits only resolvable groups. Empty unreferenced country
// groups are harmless; any routing dependency on one fails explicitly.
func (s Store) compileStrategies(config map[string]any) (map[string]any, error) {
	states, err := s.PolicyStates()
	if err != nil {
		return nil, err
	}
	byName := map[string]PolicyState{}
	for _, state := range states {
		byName[state.Group.Name] = state
	}
	validity := map[string]error{}
	done := map[string]bool{}
	var resolve func(string) error
	resolve = func(name string) error {
		if done[name] {
			return validity[name]
		}
		state := byName[name]
		var err error
		if state.Error != "" {
			err = fmt.Errorf("%s：%s", name, state.Error)
		} else if _, ok := byName[state.Selected]; ok {
			err = resolve(state.Selected)
		}
		done[name] = true
		validity[name] = err
		return err
	}
	for _, state := range states {
		resolve(state.Group.Name)
	}
	if err := validity[DefaultPolicy]; err != nil {
		return nil, err
	}
	replaceProxy(config)
	// All referenced strategy groups must resolve, even inside logical rules/DNS.
	var check func(any) error
	check = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			for key, x := range v {
				if key == "outbound" || key == "detour" || key == "download_detour" {
					if name, ok := x.(string); ok {
						if err := validity[name]; err != nil {
							return err
						}
					}
				}
				if err := check(x); err != nil {
					return err
				}
			}
		case []any:
			for _, x := range v {
				if err := check(x); err != nil {
					return err
				}
			}
		}
		return nil
	}
	route := config["route"].(map[string]any)
	if name, ok := route["final"].(string); ok {
		if err := validity[name]; err != nil {
			return nil, err
		}
	}
	if err := check(config); err != nil {
		return nil, err
	}
	sources, err := s.Sources()
	if err != nil {
		return nil, err
	}
	outbounds := []any{}
	for _, source := range sources {
		for _, n := range source.Nodes {
			data, _ := json.Marshal(n.Outbound)
			var out map[string]any
			json.Unmarshal(data, &out)
			out["tag"] = NodeKey(source.ID, n.ID)
			delete(out, "domain_resolver")
			outbounds = append(outbounds, out)
		}
	}
	for _, state := range states {
		if validity[state.Group.Name] != nil {
			continue
		}
		members := []string{}
		for _, m := range state.Members {
			if !m.IsGroup || validity[m.Key] == nil {
				members = append(members, m.Key)
			}
		}
		outbounds = append(outbounds, map[string]any{"type": "selector", "tag": state.Group.Name, "outbounds": members, "default": state.Selected, "interrupt_exist_connections": true})
	}
	config["outbounds"] = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	// The client owns choices. Give each selection combination a separate cache
	// namespace so old sing-box selector state cannot override generated defaults.
	if experimental, ok := config["experimental"].(map[string]any); ok {
		if cache, ok := experimental["cache_file"].(map[string]any); ok {
			data, _ := json.Marshal(outbounds)
			sum := sha256String(data)
			prefix := ""
			if value, exists := cache["cache_id"]; exists {
				prefix = fmt.Sprint(value) + "/"
			}
			cache["cache_id"] = prefix + "loop-" + sum
		}
	}
	return config, nil
}

func sha256String(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}
