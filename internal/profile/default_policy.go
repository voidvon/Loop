package profile

import (
	"encoding/json"
	"fmt"
)

// Available is a seed name, not a mandatory strategy. Older configurations
// implicitly use it until the user sets or clears default_policy explicitly.
func DefaultPolicyName(config map[string]any) (string, error) {
	name := DefaultPolicy
	if raw, exists := config["default_policy"]; exists {
		var ok bool
		name, ok = raw.(string)
		if !ok {
			return "", fmt.Errorf("default_policy 必须是策略组名称或空字符串")
		}
	}
	groups, err := PolicyGroups(config)
	if err != nil {
		return "", err
	}
	for _, g := range groups {
		if g.Name == name {
			return name, nil
		}
	}
	if name != "" {
		if _, explicit := config["default_policy"]; explicit {
			return "", fmt.Errorf("默认策略组 %s 不存在", name)
		}
	}
	return "", nil
}

func (s Store) DefaultStrategy() (string, error) {
	config, err := s.Config()
	if err != nil {
		return "", err
	}
	return DefaultPolicyName(config)
}

func ChangeDefaultPolicy(data []byte, name string) ([]byte, error) {
	config, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	config["default_policy"] = name
	groups, err := PolicyGroups(config)
	if err != nil {
		return nil, err
	}
	if err := validatePolicyGroups(config, groups); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	return append(encoded, '\n'), err
}

func legacyAvailable(g PolicyGroup) bool {
	return g.Name == DefaultPolicy && g.AllNodes && g.Filter == "" && len(g.Groups)+len(g.Builtins)+len(g.Nodes)+len(g.Sources) == 0
}
