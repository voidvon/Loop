package profile

import (
	"fmt"
	"path/filepath"
)

// BuiltinMember keys are also native sing-box outbound tags.
func BuiltinMember(key string) PolicyMember {
	if key == "direct" {
		return PolicyMember{Key: key, Name: "DIRECT", Detail: "直连", IsBuiltin: true}
	}
	return PolicyMember{Key: key, Name: "REJECT", Detail: "拒绝连接", IsBuiltin: true}
}

// ValidatePolicyEdit prevents a membership edit from silently selecting a new
// first member. The user must explicitly switch away before removing a choice.
func (s Store) ValidatePolicyEdit(draft, expected []byte) error {
	before, after := s, s
	before.documents = map[string][]byte{MainConfigName: expected}
	after.documents = map[string][]byte{MainConfigName: draft}
	old, err := before.PolicyStates()
	if err != nil {
		return err
	}
	next, err := after.PolicyStates()
	if err != nil {
		return err
	}
	for _, prev := range old {
		if prev.Error != "" {
			continue
		}
		for _, state := range next {
			if state.Group.Name != prev.Group.Name {
				continue
			}
			found := false
			for _, member := range state.Members {
				found = found || member.Key == prev.Selected
			}
			if !found {
				return fmt.Errorf("%s：请先切换当前选择，再移除该成员", prev.Group.Name)
			}
		}
	}
	return nil
}

// PreservePolicyChoices pins only implicit choices whose first member would
// change after an edit (for example when adding DIRECT before existing nodes).
// Pinning retains the same effective choice in both the new and old document,
// so a subsequent document rollback needs no selection-file rollback.
func (s Store) PreservePolicyChoices(previous []byte) error {
	before := s
	before.documents = map[string][]byte{MainConfigName: previous}
	old, err := before.PolicyStates()
	if err != nil {
		return err
	}
	next, err := s.PolicyStates()
	if err != nil {
		return err
	}
	choices, err := s.policyChoices()
	if err != nil {
		return err
	}
	changed := false
	for _, prev := range old {
		if prev.Error != "" {
			continue
		}
		for _, state := range next {
			if state.Group.Name != prev.Group.Name || state.Selected == prev.Selected {
				continue
			}
			for _, member := range state.Members {
				if member.Key == prev.Selected {
					choices[state.Group.Name] = prev.Selected
					changed = true
					break
				}
			}
		}
	}
	if !changed {
		return nil
	}
	return WriteJSON(filepath.Join(s.Dir, "policy-selections.json"), choices)
}
