package statusbar

import (
	"encoding/json"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"strings"
	"testing"
)

func TestNativeMenuPoliciesAndOperationState(t *testing.T) {
	state := State{Mode: profile.ModeRule, Preferences: app.DefaultTrayPreferences(), Policies: []profile.PolicyState{
		{Group: profile.PolicyGroup{Name: "Available"}, Selected: "node-a", Members: []profile.PolicyMember{{Key: "node-a", Name: "A"}, {Key: "node-b", Name: "B"}}},
		{Group: profile.PolicyGroup{Name: "YouTube"}, Selected: "Available", Members: []profile.PolicyMember{{Key: "Available", Name: "Available", IsGroup: true}, {Key: "US", Name: "US", IsGroup: true}}},
		{Group: profile.PolicyGroup{Name: "US"}, Error: "empty"},
	}}
	items := Model(state)
	if items[0].Action.Enabled == nil || !*items[0].Action.Enabled || items[0].Checked {
		t.Fatal("disconnected toggle action")
	}
	if len(items) != 8 || len(items[2].Children) != 2 || !items[2].Children[0].Checked {
		t.Fatal("policy list missing selections", items)
	}
	if !items[3].Children[0].Enabled || items[3].Children[1].Enabled {
		t.Fatal("invalid group reference can be selected")
	}
	if action := items[2].Children[1].Action; action.Group != "Available" || action.Key != "node-b" {
		t.Fatal("node identity lost")
	}
	state.Running = true
	state.Busy = true
	items = Model(state)
	if items[0].Enabled || *items[0].Action.Enabled || !items[0].Checked || items[2].Children[0].Enabled {
		t.Fatal("busy state failed to lock mutations")
	}
	state.Preferences.ShowPolicies = false
	items = Model(state)
	if len(items) != 4 || items[2].Action.Action != "show_ui" || items[3].Action.Action != "shutdown" {
		t.Fatal("preferences removed core actions")
	}
	data := encode(state)
	var model map[string]any
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "node-a") {
		t.Fatal("hidden policies remain in native model")
	}
}
