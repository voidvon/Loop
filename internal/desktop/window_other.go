//go:build !darwin || ios || !cgo

package desktop

import "fyne.io/fyne/v2"

func newAppWindow(a fyne.App) fyne.Window { return a.NewWindow("MiniProxy") }

// Keep native decorations on platforms without a custom window adapter.
func decorateWindow(w fyne.Window, shell *desktopShell, _ func()) {
	w.SetContent(shell.root)
}
func configureWindow(fyne.Window, float32) {}
func cleanupWindow(fyne.Window)            {}
