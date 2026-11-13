package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"local/miniproxy/internal/app"
	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/testutil"
)

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
	if !reflect.DeepEqual(pane.filtered, []int{0}) || pane.sectionTitle.Text != "订阅规则" {
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
