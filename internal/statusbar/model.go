// Package statusbar implements a small native menu without loading Fyne.
package statusbar

import (
	"encoding/json"

	"local/loop/internal/app"
	"local/loop/internal/profile"
)

type State struct {
	Running        bool                  `json:"running"`
	Busy           bool                  `json:"busy"`
	Closing        bool                  `json:"closing"`
	Mode           profile.Mode          `json:"mode"`
	Custom         bool                  `json:"custom"`
	Error          string                `json:"error"`
	OperationError string                `json:"operation_error,omitempty"`
	Policies       []profile.PolicyState `json:"-"`
	Preferences    app.TrayPreferences   `json:"preferences"`
}

type Item struct {
	Label     string         `json:"label,omitempty"`
	Enabled   bool           `json:"enabled"`
	Checked   bool           `json:"checked,omitempty"`
	Separator bool           `json:"separator,omitempty"`
	Action    *app.Operation `json:"action,omitempty"`
	Children  []Item         `json:"children,omitempty"`
}

func Model(state State) []Item {
	canChange := !state.Busy && !state.Closing
	enabled := !state.Running
	toggle := Item{Label: "启用代理", Enabled: canChange, Checked: state.Running, Action: &app.Operation{Action: "connect", Enabled: &enabled}}
	if state.Busy {
		toggle.Label = "正在处理…"
	}
	items := []Item{toggle}
	if state.OperationError != "" {
		items = append(items, Item{Label: "操作失败 · 打开主窗口查看"})
	}
	if state.Preferences.ShowPolicies {
		items = append(items, Item{Separator: true})
		if state.Error != "" {
			items = append(items, Item{Label: "策略读取失败 · 请在主窗口检查"})
		} else if state.Custom {
			items = append(items, Item{Label: "自定义配置 · 策略在配置中管理"})
		} else if len(state.Policies) == 0 {
			items = append(items, Item{Label: "暂无策略"})
		} else {
			for _, policy := range state.Policies {
				selected := "未选择"
				for _, member := range policy.Members {
					if member.Key == policy.Selected {
						selected = member.Name
					}
				}
				if policy.Error != "" {
					selected = "需要重新选择"
				}
				group := Item{Label: policy.Group.Name + " · " + selected, Enabled: canChange}
				for _, member := range policy.Members {
					label := member.Name
					valid := true
					if member.IsGroup {
						label = "策略 · " + label
						valid = false
						for _, child := range state.Policies {
							if child.Group.Name == member.Key && child.Error == "" {
								valid = true
							}
						}
					} else if member.Detail != "" {
						label += " · " + member.Detail
					}
					group.Children = append(group.Children, Item{Label: label, Enabled: canChange && valid, Checked: member.Key == policy.Selected, Action: &app.Operation{Action: "select_policy", Group: policy.Group.Name, Key: member.Key}})
				}
				if len(group.Children) == 0 {
					group.Children = []Item{{Label: "暂无可用节点"}}
				}
				items = append(items, group)
			}
		}
	}
	return append(items, Item{Separator: true}, Item{Label: "显示主窗口", Enabled: !state.Closing, Action: &app.Operation{Action: "show_ui"}}, Item{Label: "退出 Loop", Enabled: !state.Closing, Action: &app.Operation{Action: "shutdown"}})
}

func encode(state State) []byte {
	value := struct {
		State State  `json:"state"`
		Items []Item `json:"items"`
	}{state, Model(state)}
	data, _ := json.Marshal(value)
	return data
}
