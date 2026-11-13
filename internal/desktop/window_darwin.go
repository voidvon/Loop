//go:build darwin && !ios && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdint.h>
void miniproxyWindowConfigure(uintptr_t handle, double headerHeight, double controlsWidth, double sidebarWidth);
void miniproxyWindowMinimize(uintptr_t handle);
void miniproxyWindowZoom(uintptr_t handle);
void miniproxyWindowCleanup(uintptr_t handle);
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
)

func newAppWindow(a fyne.App) fyne.Window {
	if desktopDriver, ok := a.Driver().(desktop.Driver); ok {
		// Create without system decorations from the first frame. Native window
		// capabilities are restored by configureWindow with its titlebar hidden.
		w := desktopDriver.CreateSplashWindow()
		w.SetTitle("MiniProxy")
		return w
	}
	return a.NewWindow("MiniProxy")
}

// Fyne owns the window; native operations use its public NativeWindow API and
// are dispatched to Cocoa's main queue, never to an arbitrary key window.
func withMacWindow(w fyne.Window, action func(C.uintptr_t)) {
	if native, ok := w.(driver.NativeWindow); ok {
		native.RunNative(func(context any) {
			if mac, ok := context.(driver.MacWindowContext); ok && mac.NSWindow != 0 {
				action(C.uintptr_t(mac.NSWindow))
			}
		})
	}
}

func decorateWindow(w fyne.Window, shell *desktopShell, requestClose func()) {
	if _, ok := w.(driver.NativeWindow); !ok {
		w.SetContent(shell.root) // Fyne test driver has no native window.
		return
	}
	w.SetPadded(false)
	shell.sidebarPadding.Layout = layout.NewCustomPaddedLayout(windowHeaderHeight+6, 16, 16, 16)
	shell.sidebarPadding.Refresh()
	sidebarWidth := shell.sidebar.MinSize().Width
	header := newWindowHeader(windowActions{
		close:    requestClose,
		minimize: func() { withMacWindow(w, func(handle C.uintptr_t) { C.miniproxyWindowMinimize(handle) }) },
		zoom:     func() { withMacWindow(w, func(handle C.uintptr_t) { C.miniproxyWindowZoom(handle) }) },
	})
	w.SetContent(container.New(windowFrameLayout{sidebarWidth: sidebarWidth}, shell.root, header))
}

// Show creates the borderless native window; configure it as a regular,
// resizable window without ever showing the system titlebar.
func configureWindow(w fyne.Window, sidebarWidth float32) {
	withMacWindow(w, func(handle C.uintptr_t) {
		scale := w.Canvas().Scale()
		C.miniproxyWindowConfigure(handle, C.double(windowHeaderHeight*scale), C.double((windowControlsLeft+3*windowControlPitch+10)*scale), C.double(sidebarWidth*scale))
	})
}

func cleanupWindow(w fyne.Window) {
	withMacWindow(w, func(handle C.uintptr_t) { C.miniproxyWindowCleanup(handle) })
}
