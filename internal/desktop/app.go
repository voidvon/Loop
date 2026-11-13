// Package desktop implements the native Fyne presentation and UI lifecycle.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	application "local/loop/internal/app"
	"local/loop/internal/autostart"
	"local/loop/internal/control"
	"local/loop/internal/profile"
)

// Run must be invoked on the main goroutine, as required by Fyne. The window
// owns only presentation: its destruction never stops the background runtime.
func Run(options application.Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if control.FocusUI(ctx, options.StateDir) == nil {
		return nil
	}
	startupCtx, startupCancel := context.WithTimeout(ctx, 15*time.Second)
	client, err := control.Ensure(startupCtx, options, false)
	startupCancel()
	if err != nil {
		return err
	}
	defer client.Close()
	snapshot, err := client.Status(ctx)
	if err != nil {
		return err
	}
	service := application.NewService(options)
	service.Remote = client
	if err := application.MigrateDesktopPreferences(); err != nil {
		return fmt.Errorf("迁移界面偏好：%w", err)
	}
	a := app.NewWithID(application.ID)
	appearance := newClientTheme()
	appearance.mode = a.Preferences().StringWithFallback("appearance", appearanceSystem)
	if fonts, err := loadSystemFonts(); err != nil {
		log.Print(err)
	} else {
		appearance.fonts = fonts
	}
	a.Settings().SetTheme(appearance)
	// Migrate existing GUI preferences once; thereafter the daemon owns them.
	if !snapshot.TrayConfigured {
		prefs := loadMenuBarPreferences(a.Preferences())
		if value, err := client.Action(ctx, application.Operation{Action: "set_tray", Tray: &prefs}); err == nil {
			snapshot = value.Status
		}
	}
	storeMenuBarPreferences(a.Preferences(), snapshot.Tray)
	w := newAppWindow(a)
	w.Resize(fyne.NewSize(1120, 760))
	w.CenterOnScreen()
	var stopped atomic.Bool
	closing, localBusy, nodeBusy, documentBusy, online := false, false, false, false, true
	var shell *desktopShell
	var manager *nodeManager
	var requestExit func()
	cleanupEndpoint, err := control.UIEndpoint(options.StateDir, func() {
		if !stopped.Load() {
			fyne.Do(func() {
				if !closing {
					w.Show()
					w.RequestFocus()
				}
			})
		}
	}, func() {
		if !stopped.Load() {
			fyne.Do(func() {
				if requestExit != nil {
					requestExit()
				}
			})
		}
	})
	if err != nil {
		a.Quit()
		if control.FocusUI(ctx, options.StateDir) == nil {
			return nil
		}
		return err
	}
	defer cleanupEndpoint()
	if options.ConfigPath == "" {
		manager = newNodeManager(w, service)
		defer manager.Close()
	}
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	button := widget.NewButton("启动", nil)
	button.Importance = widget.HighImportance
	report := func(err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("%w\n\n后台日志：%s", err, filepath.Join(options.StateDir, "daemon.log")), w)
		}
	}
	busy := func() bool {
		return localBusy || nodeBusy || documentBusy || snapshot.Busy || snapshot.Closing || closing || !online
	}
	render := func() {
		locked := busy()
		if snapshot.Running {
			button.SetText("停止")
			button.SetIcon(theme.MediaStopIcon())
			capture := "系统代理"
			if snapshot.TUN {
				capture = "TUN"
			}
			status.SetText("已启用 · " + capture + " · " + snapshot.Mode.Label())
		} else {
			button.SetText("启动")
			button.SetIcon(theme.MediaPlayIcon())
			status.SetText("未连接")
		}
		if !online {
			status.SetText("后台不可用，请关闭并重新打开主窗口")
		} else if locked {
			status.SetText("正在处理…")
		} else if snapshot.Error != "" {
			status.SetText(status.Text + "\n" + snapshot.Error)
		}
		setEnabled(button, !locked)
		if manager != nil {
			manager.SetLocked(locked || snapshot.Running)
		}
		if shell != nil {
			shell.SetModeEnabled(!locked)
			shell.SyncMode()
		}
	}
	apply := func(state control.Snapshot) {
		if state.Revision < snapshot.Revision {
			return
		}
		changed := snapshot.Revision != state.Revision
		snapshot, online = state, true
		if shell != nil {
			shell.more.SetPreferences(state.Tray)
			if changed && !state.Busy {
				if manager != nil {
					manager.refresh()
				}
				shell.RefreshCurrent()
			}
		}
		render()
	}
	execute := func(operation application.Operation) {
		if busy() {
			return
		}
		localBusy = true
		render()
		go func() {
			value, operationErr := client.Action(ctx, operation)
			if stopped.Load() {
				return
			}
			fyne.Do(func() {
				if closing {
					return
				}
				localBusy = false
				if value.Status.Instance == snapshot.Instance {
					apply(value.Status)
				} else {
					render()
				}
				report(operationErr)
			})
		}()
	}
	button.OnTapped = func() {
		enabled := !snapshot.Running
		execute(application.Operation{Action: "connect", Enabled: &enabled})
	}
	shell = newDesktopShell(w, service, manager, status, button, func() bool { return snapshot.Running }, func(mode profile.Mode) {
		if mode != snapshot.Mode {
			execute(application.Operation{Action: "set_mode", Mode: mode})
		}
		shell.SyncMode()
	}, func(enabled bool) { execute(application.Operation{Action: "set_tun", Enabled: &enabled}) })
	shell.more.startup.attach(autostart.Service{})
	defer shell.more.startup.Close()
	a.Lifecycle().SetOnEnteredForeground(func() {
		if !stopped.Load() {
			fyne.Do(func() {
				if !closing && shell.current == pageMore {
					shell.more.Refresh()
				}
			})
		}
	})
	if manager != nil {
		manager.OnBusy = func(value bool) { nodeBusy = value; render() }
	}
	shell.documents.Context = ctx
	shell.documents.Running = func() bool { return snapshot.Running }
	shell.documents.SaveRules = func(workCtx context.Context, data, expected []byte, _ bool) (application.RuleSaveResult, error) {
		return service.SaveRules(workCtx, nil, data, expected, false)
	}
	shell.documents.OnRulesApplied = func(result application.RuleSaveResult) { snapshot.Running = result.Running; render() }
	shell.documents.OnBusy = func(value bool) { documentBusy = value; render() }
	if shell.strategies != nil {
		shell.strategies.OnSelect = func(group, key string) {
			if !shell.documents.dirty() {
				execute(application.Operation{Action: "select_policy", Group: group, Key: key})
			}
		}
		shell.strategies.OnBusy = shell.documents.OnBusy
		defer shell.strategies.Close()
	}
	shell.more.OnChanged = func() {
		// Keep settings serialized with other daemon operations. Restore the actual
		// preferences if another frontend started work while this UI was idle.
		if busy() {
			shell.more.SetPreferences(snapshot.Tray)
			return
		}
		prefs := loadMenuBarPreferences(a.Preferences())
		execute(application.Operation{Action: "set_tray", Tray: &prefs})
	}
	shell.appearance.Add(newAppearanceSelect(a, appearance))
	a.Settings().AddListener(func(fyne.Settings) {
		if !stopped.Load() {
			shell.configPane.updateEditorPalette()
		}
	})
	defer shell.documents.Close()
	finish := func() {
		if stopped.Swap(true) {
			return
		}
		closing = true
		cancel()
		if manager != nil {
			manager.Close()
		}
		shell.more.startup.Close()
		if shell.strategies != nil {
			shell.strategies.Close()
		}
		shell.documents.Close()
		a.Quit()
	}
	// Closing the window destroys the UI process; only explicit Quit requests
	// stop the daemon. Operations already accepted by it continue independently.
	guard := func(action func()) {
		if closing {
			return
		}
		if shell.configPane.HasDraft() {
			dialog.ShowConfirm("配置尚未保存", "关闭界面会丢失未保存的配置修改，是否继续？", func(ok bool) {
				if ok {
					action()
				}
			}, w)
		} else {
			action()
		}
	}
	requestClose := func() { guard(finish) }
	requestExit = func() {
		guard(func() {
			if !online {
				finish()
				return
			}
			closing = true
			render()
			go func() {
				quitCtx, quitCancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer quitCancel()
				operationErr := shutdownBackground(quitCtx, client)
				fyne.Do(func() {
					if operationErr != nil {
						closing = false
						render()
						report(operationErr)
						return
					}
					finish()
				})
			}()
		})
	}
	w.SetCloseIntercept(requestClose)
	decorateWindow(w, shell, requestClose)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			fyne.Do(finish)
		case <-ctx.Done():
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
				value, probeErr := client.Status(probeCtx)
				probeCancel()
				if stopped.Load() {
					return
				}
				fyne.Do(func() {
					if closing {
						return
					}
					if probeErr != nil {
						online = false
						render()
						return
					}
					if snapshot.Revision != value.Revision || !online {
						apply(value)
					}
				})
			}
		}
	}()
	render()
	w.Show()
	configureWindow(w, shell.sidebar.MinSize().Width)
	// Font parsing and the first frame use temporary buffers. Reclaim their
	// pages once startup settles, without adding a recurring GC/CPU cost.
	reclaimStartup := time.AfterFunc(2*time.Second, debug.FreeOSMemory)
	defer reclaimStartup.Stop()
	cleanupQuit := configureQuit(w, requestExit)
	defer cleanupQuit()
	defer cleanupWindow(w)
	a.Run()
	stopped.Store(true)
	return nil
}

// Explicit app Quit waits for an accepted transaction to finish before stopping
// the daemon; ordinary window close never calls this function.
func shutdownBackground(ctx context.Context, client *control.Client) error {
	for {
		_, err := client.Execute(ctx, application.Operation{Action: "shutdown"})
		if !errors.Is(err, control.ErrBusy) {
			return err
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
