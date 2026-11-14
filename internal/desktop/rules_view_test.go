package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestRuleCollectionsPreserveSubscriptionIdentityAcrossReload(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(newClientTheme())
	store := testutil.NewStore(t)
	config, err := store.Config()
	if err != nil {
		t.Fatal(err)
	}
	sets := []any{
		map[string]any{"type": "local", "tag": "local", "path": "local.srs"},
		map[string]any{"type": "remote", "tag": "First", "url": "https://example.org/first.srs", "format": "binary"},
		map[string]any{"type": "remote", "tag": "Second", "url": "https://example.org/second.srs", "format": "binary"},
		map[string]any{"type": "remote", "tag": "Unused", "url": "https://example.org/unused.srs", "format": "binary"},
	}
	route := map[string]any{"final": "proxy", "rule_set": sets, "rules": []any{
		map[string]any{"domain_suffix": "example.com", "outbound": "direct"},
		map[string]any{"rule_set": "First", "outbound": "Available"},
		map[string]any{"rule_set": "local", "outbound": "direct"},
		map[string]any{"rule_set": "Second", "outbound": "direct"},
	}}
	config["route"] = route
	path := filepath.Join(store.Dir, profile.MainConfigName)
	write := func() {
		t.Helper()
		if err := profile.WriteJSON(path, config); err != nil {
			t.Fatal(err)
		}
	}
	write()
	source, _ := os.ReadFile(path)
	window := a.NewWindow("collections")
	c := newDocumentController(window, app.NewService(app.Options{Store: store}))
	defer c.Close()
	p := newRulesPane(window, c)
	if p.groups.Length() != 5 || !reflect.DeepEqual(p.filtered, []int{0, 2}) {
		t.Fatal("collections lost subscriptions or local rules", p.filtered)
	}
	row := p.groups.CreateItem().(*infoRow)
	p.groups.UpdateItem(3, row)
	if row.title.Text != "Second" || row.detail.Text != "DIRECT" || row.detailBadge.background.name != colorBadgeDirect {
		t.Fatal("subscription row does not show its own routing target")
	}
	p.groups.UpdateItem(4, row)
	if row.detail.Text != "按规则设置" || row.detailBadge.background.name != colorBadgeMuted {
		t.Fatal("unbound subscription inherited a pooled target badge")
	}
	p.groups.Select(3)
	card := newRuleCard()
	p.updateRuleCard(0, card)
	if p.sectionHeader.title.Text != "Second" || !reflect.DeepEqual(p.filtered, []int{2}) || card.title.Text != "Second" {
		t.Fatal("selected subscription did not use its source index")
	}
	staleMenu := card.onMenu
	got, _ := os.ReadFile(path)
	if !bytes.Equal(source, got) {
		t.Fatal("browsing collections changed configuration")
	}
	// Removing an earlier collection must retain the selected subscription.
	route["rule_set"] = []any{sets[0], sets[2], sets[3]}
	write()
	p.Refresh()
	if p.section != 2 || p.sectionHeader.title.Text != "Second" || !reflect.DeepEqual(p.filtered, []int{1}) {
		t.Fatal("reload selected a different subscription", p.section, p.filtered)
	}
	staleMenu(fyne.NewPos(0, 0))
	if p.contextMenu != nil {
		t.Fatal("stale subscription card opened a menu after reload")
	}
	p.updateRuleCard(0, card)
	card.TappedSecondary(&fyne.PointEvent{})
	if p.contextMenu == nil || p.selected != 1 {
		t.Fatal("recycled card menu targets a different subscription")
	}
	p.hideRuleMenu()
	route["rule_set"] = []any{sets[0], sets[3]}
	write()
	p.Refresh()
	if p.section != 1 || p.body.Objects[0] != p.localPage || p.sectionHeader.title.Text != "本地规则" {
		t.Fatal("deleted collection did not return to local rules")
	}
}

func TestCompactRuleCardsKeepBadgesAndGridWithinBounds(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	th := newClientTheme()
	if fonts, err := loadSystemFonts(); err == nil {
		th.fonts = fonts
	}
	a.Settings().SetTheme(th)
	store := testutil.NewStore(t)
	path := filepath.Join(store.Dir, profile.MainConfigName)
	if err := os.WriteFile(path, []byte(`{"route":{"final":"Available","rules":[{"domain_suffix":["ai.nodeisle.com"],"outbound":"Available"},{"domain_suffix":["cn-ai.nodeisle.com"],"outbound":"direct"}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	w := a.NewWindow("grid")
	c := newDocumentController(w, app.NewService(app.Options{Store: store}))
	defer c.Close()
	p := newRulesPane(w, c)
	w.SetContent(p.root)
	w.Resize(fyne.NewSize(820, 680))
	for _, mode := range []string{appearanceDark, appearanceLight} {
		th.mode = mode
		a.Settings().SetTheme(th)
		p.root.Refresh()
		if p.list.ColumnCount() != 2 {
			t.Fatal("desktop rule grid must have two columns", p.list.ColumnCount())
		}
		card := newRuleCard()
		p.updateRuleCard(0, card)
		card.Resize(fyne.NewSize(p.cardWidth, card.MinSize().Height))
		test.WidgetRenderer(card).Layout(card.Size())
		objects := card.content.Objects
		kind, title, badge := objects[2], objects[3], objects[4]
		if kind.Position().X+kind.Size().Width > badge.Position().X-5.9 || title.Position().Y < kind.Position().Y+kind.Size().Height+3.9 || card.Size().Height-title.Position().Y-title.Size().Height < 9.9 {
			t.Fatal("rule text overlaps the badge or card bottom", mode)
		}
		if card.badge.Size().Width < fyne.MeasureText("Available", card.badge.Theme().Size(sizeMemberDetail), card.badge.TextStyle).Width || strings.Join(memberTitleLines(card.badge), "") != "Available" {
			t.Fatal("short strategy badge was truncated", card.badge.Size(), memberTitleLines(card.badge), mode)
		}
		// Recycle a strategy card for DIRECT, then a long custom strategy.
		p.updateRuleCard(1, card)
		if card.badge.Text != "DIRECT" || card.badge.Importance != widget.SuccessImportance || card.tag.background.name != colorBadgeDirect {
			t.Fatal("recycled card retained proxy styling")
		}
		card.tag.setTarget(strings.Repeat("Custom", 30), "Custom")
		card.Refresh()
		if badge.Position().X+badge.Size().Width > card.Size().Width-11.9 || kind.Size().Width < 0 {
			t.Fatal("long strategy badge escaped card bounds")
		}
	}
	w.Resize(fyne.NewSize(680, 680))
	p.root.Refresh()
	if p.list.ColumnCount() != 1 {
		t.Fatal("narrow rule grid must wrap to one column", p.list.ColumnCount())
	}
}

func TestRulesResolveLegacyDefaultToActualStrategy(t *testing.T) {
	for _, tc := range []struct {
		name, final, defaultPolicy, wantFinal, wantAlias string
		external                                         bool
	}{
		{name: "legacy", final: "proxy", wantFinal: "Available", wantAlias: "Available"},
		{name: "explicit Available", final: "Available", wantFinal: "Available", wantAlias: "Available"},
		{name: "custom default", final: "proxy", defaultPolicy: "Custom", wantFinal: "Custom", wantAlias: "Custom"},
		{name: "explicit strategy", final: "Custom", wantFinal: "Custom", wantAlias: "Available"},
		{name: "explicit direct", final: "direct", wantFinal: "direct", wantAlias: "Available"},
		{name: "external outbound", final: "proxy", wantFinal: "proxy", wantAlias: "proxy", external: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := test.NewApp()
			a.Settings().SetTheme(newClientTheme())
			defer a.Quit()
			store := testutil.NewStore(t)
			config, err := store.Config()
			if err != nil {
				t.Fatal(err)
			}
			if tc.defaultPolicy != "" || tc.final == "Custom" {
				config["policy_groups"] = []profile.PolicyGroup{{Name: "Available", AllNodes: true}, {Name: "Custom", AllNodes: true}}
			}
			if tc.defaultPolicy != "" {
				config["default_policy"] = tc.defaultPolicy
			}
			config["route"] = map[string]any{
				"final": tc.final,
				"rules": []any{
					map[string]any{"domain_suffix": "example.com", "outbound": "proxy"},
					map[string]any{"rule_set": "remote", "outbound": "proxy"},
				},
				"rule_set": []any{map[string]any{"type": "remote", "tag": "remote", "url": "https://example.org/rules.srs", "format": "binary"}},
			}
			path := filepath.Join(store.Dir, profile.MainConfigName)
			if err := profile.WriteJSON(path, config); err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			window := a.NewWindow("rules")
			controller := newDocumentController(window, app.NewService(app.Options{Store: store}))
			defer controller.Close()
			if tc.external {
				controller.name = ""
			}
			pane := newRulesPane(window, controller)
			pane.groups.Select(0)
			row := pane.groups.CreateItem().(*infoRow)
			pane.groups.UpdateItem(0, row)
			if row.detail.Text != compactRuleTarget(tc.wantFinal) || pane.final.Selected != ruleTargetLabel(tc.wantFinal) || pane.final.SelectedIndex() < 0 || controller.working {
				t.Fatal("Final display/selection does not match effective strategy", row.detail.Text, pane.final.Selected)
			}
			if pane.targetValue(pane.final.Selected) != tc.wantFinal {
				t.Fatal("Final selection would save a different target")
			}
			for _, section := range []int{1, 2} {
				pane.groups.Select(section)
				card := newRuleCard()
				pane.updateRuleCard(0, card)
				if card.badge.Text != tc.wantAlias || pane.targetLabel("proxy") != ruleTargetLabel(tc.wantAlias) {
					t.Fatal("rule badge/editor disagrees with effective default", card.badge.Text)
				}
			}
			pane.Refresh()
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(source, got) {
				t.Fatal("browsing changed the source config", err)
			}
			if tc.external {
				return
			}
			compiled, err := store.CompileMode(profile.ModeRule)
			if err != nil || compiled["route"].(map[string]any)["final"] != tc.wantFinal {
				t.Fatal("Final display differs from compiled routing", err)
			}
			// Saving a concrete choice must retain it, including after reloading.
			draft, err := profile.ChangeRouting(source, profile.RuleChange{Operation: "final", Final: pane.targetValue(ruleTargetLabel("Available"))})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := profile.ReadRoutingRules(draft)
			if err != nil || saved.Final != "Available" {
				t.Fatal("selection did not produce a concrete Available final", err)
			}
			if err := os.WriteFile(path, draft, 0600); err != nil {
				t.Fatal(err)
			}
			pane.Refresh()
			if pane.final.Selected != ruleTargetLabel("Available") || controller.working {
				t.Fatal("saved Available selection was lost after reload")
			}
		})
	}
}

func TestRulesSectionsKeepGlobalIndexesAndGuardRecycledMenus(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newClientTheme())
	defer a.Quit()
	store := testutil.NewStore(t)
	path := filepath.Join(store.Dir, profile.MainConfigName)
	source := []byte(`{"route":{"final":"proxy","rules":[
 {"domain_suffix":"example.com","outbound":"direct"},
 {"rule_set":"remote","outbound":"direct"},
 {"type":"logical","mode":"and","rules":[{"rule_set":"remote"},{"port":443}],"outbound":"proxy"},
 {"action":"resolve"}],"rule_set":[{"type":"remote","tag":"remote","url":"https://example.org/rules.srs","format":"binary"}]}}`)
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	window := a.NewWindow("rules")
	controller := newDocumentController(window, app.NewService(app.Options{Store: store}))
	defer controller.Close()
	pane := newRulesPane(window, controller)
	window.SetContent(pane.root)
	window.Resize(fyne.NewSize(880, 700))
	if pane.groups.Length() != 3 || !reflect.DeepEqual(pane.filtered, []int{0, 2, 3}) {
		t.Fatal("local section hid advanced rules or lost real indexes", pane.filtered)
	}
	card := newRuleCard()
	pane.updateRuleCard(0, card)
	pane.groups.Select(2)
	if !reflect.DeepEqual(pane.filtered, []int{0}) || pane.sectionHeader.title.Text != "remote" {
		t.Fatal("remote section missing")
	}
	card.onMenu(fyne.NewPos(0, 0))
	if pane.contextMenu != nil {
		t.Fatal("recycled card opened a menu for another section")
	}
	pane.updateRuleCard(0, card)
	card.onMenu(fyne.NewPos(0, 0))
	if pane.contextMenu == nil {
		t.Fatal("subscription menu unavailable")
	}
	pane.hideRuleMenu()
	baseline := append([]byte(nil), pane.baseline...)
	pane.baseline = append(pane.baseline, ' ')
	card.onMenu(fyne.NewPos(0, 0))
	if pane.contextMenu != nil {
		t.Fatal("stale card menu accepted changed document")
	}
	pane.baseline = baseline
	pane.groups.Select(0)
	if pane.body.Objects[0] != pane.finalPage {
		t.Fatal("Final not editable")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(source, got) {
		t.Fatal("browsing rules changed the config")
	}
}
