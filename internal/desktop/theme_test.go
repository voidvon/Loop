package desktop

import (
	"bytes"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	prettyview "github.com/ideaconnect/go-fyne-pretty-view/v2"
	"local/loop/internal/app"
	"local/loop/internal/profile"
	"local/loop/internal/testutil"
)

func TestAppearanceRefreshesSurfacesAndPreservesDraftAndMode(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	th := newClientTheme()
	th.mode = appearanceLight
	a.Settings().SetTheme(th)
	w := a.NewWindow("appearance")
	modeChanges := 0
	shell := newDesktopShell(w, app.NewService(app.Options{Store: testutil.NewStore(t)}), nil, widget.NewLabel("未连接"), widget.NewButton("启动", nil), func() bool { return false }, func(profile.Mode) { modeChanges++ }, func(bool) {})
	defer shell.documents.Close()
	w.SetContent(shell.root)
	selector := newAppearanceSelect(a, th)
	shell.appearance.Add(selector)
	a.Settings().AddListener(func(fyne.Settings) { shell.configPane.updateEditorPalette() })
	shell.selectPage(pageConfig)
	editor := shell.configPane.document.view
	editor.SetInputConfig(prettyview.InputConfig{DebounceFor: -1})
	a.Clipboard().SetContent(`{"route":{"final":"direct"}}`)
	editor.SelectAll()
	editor.Paste()
	draft := append([]byte(nil), editor.Source()...)
	savedMode := shell.modeSelect.Selected
	bg := themedSurface(colorPanel, 18).(*surface)
	renderer := test.WidgetRenderer(bg)
	background := renderer.Objects()[0].(*canvas.Rectangle)
	light := color.NRGBAModel.Convert(background.FillColor)
	selector.SetSelected("深色模式")
	renderer.Refresh()
	if color.NRGBAModel.Convert(background.FillColor) == light {
		t.Fatal("surface retained its light color")
	}
	if th.mode != appearanceDark || a.Preferences().String("appearance") != appearanceDark {
		t.Fatal("appearance preference was not saved")
	}
	if !bytes.Equal(editor.Source(), draft) || !shell.configPane.HasDraft() {
		t.Fatal("appearance change discarded the editor draft")
	}
	if savedMode != shell.modeSelect.Selected || modeChanges != 0 {
		t.Fatal("appearance change changed proxy mode")
	}
	for _, variant := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		if th.Color(colorPanel, variant) != palette(theme.VariantDark).panel {
			t.Fatal("explicit dark mode followed OS light appearance")
		}
	}
	selector.SetSelected("跟随系统")
	if th.Color(colorPanel, theme.VariantLight) == th.Color(colorPanel, theme.VariantDark) {
		t.Fatal("system appearance does not distinguish variants")
	}
	selector.SetSelected("浅色模式")
	if th.Color(colorPanel, theme.VariantDark) != palette(theme.VariantLight).panel {
		t.Fatal("explicit light mode followed OS dark appearance")
	}
}
