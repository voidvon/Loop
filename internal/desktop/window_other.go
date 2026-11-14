//go:build !darwin || ios || !cgo

package desktop

import "fyne.io/fyne/v2"

func newAppWindow(a fyne.App) fyne.Window { return a.NewWindow("Loop") }

// Other platforms retain solid surfaces until a native backdrop adapter is
// available; transparent framebuffers alone would expose unblurred windows.
func prepareWindowSurface(fyne.Window) bool       { return false }
func refreshWindowAppearance(fyne.Window, string) {}

// Keep native decorations on platforms without a custom window adapter.
func decorateWindow(w fyne.Window, shell *desktopShell, _ func()) {
	w.SetContent(shell.root)
}
func configureWindow(fyne.Window, float32) {}
func cleanupWindow(fyne.Window)            {}

func configureQuit(w fyne.Window, action func()) func() {
	quit := fyne.NewMenuItem("退出 Loop", action)
	quit.IsQuit = true
	w.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("Loop", quit)))
	return func() {}
}
