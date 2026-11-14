package desktop

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestAppearanceSwitchesSynchronizeAndSavePreference(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	th := newClientTheme()
	th.mode = appearanceLight
	a.Settings().SetTheme(th)
	otherDark, otherSystem := newAppearanceSwitches(a)
	dark, system := newAppearanceSwitches(a)
	if dark.Checked || system.Checked {
		t.Fatal("explicit light appearance has enabled switches")
	}
	test.Tap(dark)
	if th.mode != appearanceDark || !dark.Checked || system.Checked || !otherDark.Checked || a.Preferences().String("appearance") != appearanceDark {
		t.Fatal("dark switch did not synchronize and persist the appearance")
	}
	test.Tap(system)
	if th.mode != appearanceSystem || !system.Checked || !otherSystem.Checked {
		t.Fatal("system switch did not synchronize the other controls")
	}
	effectiveDark := th.variant(a.Settings().ThemeVariant()) == theme.VariantDark
	test.Tap(system)
	if system.Checked || dark.Checked != effectiveDark || th.mode == appearanceSystem {
		t.Fatal("leaving system appearance changed the effective variant")
	}
	setAppearanceMode(a, th, appearanceLight)
	if dark.Checked || system.Checked {
		t.Fatal("appearance change left stale switch values")
	}
	// Keep keyboard access and disabled semantics of the embedded Fyne Check.
	dark.TypedRune(' ')
	if th.mode != appearanceDark {
		t.Fatal("keyboard could not enable dark appearance")
	}
	dark.Disable()
	test.Tap(dark)
	dark.TypedRune(' ')
	if th.mode != appearanceDark {
		t.Fatal("disabled switch accepted input")
	}
}
