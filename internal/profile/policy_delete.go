package profile

import (
	"errors"
	"fmt"
	"strings"
)

// RemovePolicyGroup prepares a single-document edit. Optional member references
// are removed together; active choices and routing dependencies must be changed
// explicitly by the user. Nothing is written until the normal document save.
func (s Store) RemovePolicyGroup(data []byte, name string) ([]byte, error) {
	if name == DefaultPolicy {
		return nil, errors.New("不能删除默认策略 Available")
	}
	config, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	groups, err := PolicyGroups(config)
	if err != nil {
		return nil, err
	}
	found := false
	for _, group := range groups {
		found = found || group.Name == name
	}
	if !found {
		return nil, fmt.Errorf("策略组 %s 已不存在，请刷新后重试", name)
	}
	// Report the concrete dependency instead of a generic missing-group error.
	var check func(any, string) error
	check = func(value any, path string) error {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				field := key
				if path != "" {
					field = path + "." + key
				}
				if child == name && (key == "outbound" || key == "detour" || key == "download_detour" || field == "route.final") {
					return fmt.Errorf("策略组 %s 正被 %s 引用，请先在规则页或配置页修改该引用后再删除", name, field)
				}
				if err := check(child, field); err != nil {
					return err
				}
			}
		case []any:
			for i, child := range value {
				if err := check(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := check(config, ""); err != nil {
		return nil, err
	}
	candidate := s
	candidate.documents = map[string][]byte{MainConfigName: data}
	states, err := candidate.PolicyStates()
	if err != nil {
		return nil, err
	}
	selectedBy := []string{}
	for _, state := range states {
		if state.Group.Name != name && state.Selected == name {
			selectedBy = append(selectedBy, state.Group.Name)
		}
	}
	if len(selectedBy) > 0 {
		return nil, fmt.Errorf("策略组 %s 正被 %s 选中，请先在策略页切换这些组的选择后再删除", name, strings.Join(selectedBy, "、"))
	}
	kept := make([]PolicyGroup, 0, len(groups)-1)
	for _, group := range groups {
		if group.Name == name {
			continue
		}
		refs := make([]string, 0, len(group.Groups))
		for _, ref := range group.Groups {
			if ref != name {
				refs = append(refs, ref)
			}
		}
		group.Groups = refs
		kept = append(kept, group)
	}
	return ChangePolicyGroups(data, kept)
}
