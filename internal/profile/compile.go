package profile

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// Compile never writes to any profile file. Only stable policy outlet names are
// accepted; selected node tags and subscription names cannot leak into policy.
func (s Store) Compile() (map[string]any, error) {
	mode, err := s.Mode()
	if err != nil {
		return nil, err
	}
	return s.CompileMode(mode)
}

// CompileNode checks a candidate against the saved policy, regardless of mode.
func (s Store) CompileNode(node Node) (map[string]any, error) {
	return s.compileNode(&node)
}

func (s Store) compileNode(node *Node) (map[string]any, error) {
	var outbound map[string]any
	if node != nil {
		if err := validateNode(*node); err != nil {
			return nil, err
		}
		data, err := json.Marshal(node.Outbound)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &outbound); err != nil {
			return nil, err
		}
	}

	base, err := s.Config()
	if err != nil {
		return nil, err
	}
	ports, err := ProxyPorts(base)
	if err != nil {
		return nil, err
	}
	if ports.GlobalEnabled {
		policy, err := DefaultPolicyName(base)
		if err != nil {
			return nil, err
		}
		if policy == "" {
			return nil, fmt.Errorf("全局代理入口需要默认策略，请先在策略页设置，或关闭全局代理入口")
		}
	}
	delete(base, "proxy_ports")
	base["inbounds"] = ports.Inbounds()
	route, ok := base["route"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config.json 必须有 route 对象")
	}
	groups, err := PolicyGroups(base)
	if err != nil {
		return nil, err
	}
	if err := validatePolicyGroups(base, groups); err != nil {
		return nil, err
	}
	delete(base, "policy_groups")
	delete(base, "default_policy")
	// Relative rule-set paths belong to the policy directory, not the runtime dir.
	if sets, ok := route["rule_set"].([]any); ok {
		for _, value := range sets {
			if set, ok := value.(map[string]any); ok {
				for _, key := range []string{"path", "initial_path"} {
					if path, ok := set[key].(string); ok && path != "" && !filepath.IsAbs(path) {
						absolute, err := filepath.Abs(filepath.Join(s.Dir, path))
						if err != nil {
							return nil, err
						}
						set[key] = absolute
					}
				}
			}
		}
	}
	base["outbounds"] = []any{map[string]any{"type": "direct", "tag": "direct"}}
	if node != nil {
		outbound["tag"] = "proxy"
		delete(outbound, "domain_resolver")
		base["outbounds"] = []any{outbound, map[string]any{"type": "direct", "tag": "direct"}}
	}
	if node != nil {
		outbounds := base["outbounds"].([]any)
		for _, group := range groups {
			outbounds = append(outbounds, map[string]any{"type": "selector", "tag": group.Name, "outbounds": []string{"proxy"}, "default": "proxy"})
		}
		base["outbounds"] = outbounds
	}
	return base, nil
}

func onlyKeys(object map[string]any, name string, allowed ...string) error {
	for key := range object {
		found := false
		for _, candidate := range allowed {
			found = found || key == candidate
		}
		if !found {
			return fmt.Errorf("%s 不允许字段 %q，节点与策略必须独立", name, key)
		}
	}
	return nil
}

func (s Store) WriteRuntime(path string) error {
	config, err := s.Compile()
	if err != nil {
		return err
	}
	return WriteJSON(path, config)
}
