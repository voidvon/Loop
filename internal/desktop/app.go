// Package desktop implements the native Fyne presentation and UI lifecycle.
package desktop

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	application "local/miniproxy/internal/app"
	"local/miniproxy/internal/autostart"
	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/proxy"
)

// Run must be invoked on the main goroutine, as required by Fyne.
func Run(options application.Options) error {
	service := application.NewService(options)
	store, stateDir := options.Store, options.StateDir
	coreErr := service.CoreError()
	a := app.NewWithID("local.miniproxy.desktop")
	appearance := newClientTheme()
	appearance.mode = a.Preferences().StringWithFallback("appearance", appearanceSystem)
	if fonts, err := loadSystemFonts(); err != nil {
		log.Print(err)
	} else {
		appearance.fonts = fonts
	}
	a.Settings().SetTheme(appearance)
	w := newAppWindow(a)
	w.Resize(fyne.NewSize(1120, 760))
	w.CenterOnScreen()
	d := &proxy.Desktop{CorePath: service.CorePath, ConfigPath: options.ConfigPath, StateDir: stateDir, HelperPath: options.HelperPath}
	if options.ConfigPath == "" {
		d.PrepareConfig = func() (string, error) {
			return application.PrepareProfile(store, filepath.Join(stateDir, "runtime", "config.json"))
		}
	}
	var backend proxy.Backend = d
	ctx, cancel := context.WithCancel(context.Background())
	// Application-menu Quit / Cmd+Q can bypass the window close interceptor.
	defer func() {
		cancel()
		if stopErr := backend.Stop(); stopErr != nil {
			log.Printf("退出时恢复代理失败: %v", stopErr)
		}
	}()
	running, busy, closing := false, false, false
	status := widget.NewLabel("未连接")
	var manager *nodeManager
	if options.ConfigPath == "" {
		manager = newNodeManager(w, service)
		defer func() {
			if manager.cancelWork != nil {
				manager.cancelWork()
			}
		}()
	}
	setManagerLocked := func(locked bool) {
		if manager != nil {
			manager.SetLocked(locked)
		}
	}
	var shell *desktopShell
	var button *widget.Button
	activeStatus := func() string {
		if options.ConfigPath == "" {
			if mode, modeErr := store.Mode(); modeErr == nil {
				capture := "系统代理"
				if tun, _ := store.TUN(); tun {
					capture = "TUN"
				}
				return "已启用 · " + capture + " · " + mode.Label()
			}
		}
		return "已启用 · 系统代理"
	}
	report := func(err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("%w\n\n内核日志：%s", err, filepath.Join(stateDir, "sing-box.log")), w)
		}
	}
	d.OnExit = func(exitErr error) {
		fyne.Do(func() {
			if closing {
				return
			}
			// The document operation owns the final connection state while applying
			// rules; an exit notification must not unlock competing operations.
			if shell != nil && shell.documents.working {
				return
			}
			running, busy = false, false
			button.SetText("启动")
			button.SetIcon(theme.MediaPlayIcon())
			status.SetText("未连接")
			setManagerLocked(false)
			button.Enable()
			if shell != nil {
				shell.SetModeEnabled(true)
				shell.RefreshCurrent()
			}
			report(exitErr)
		})
	}
	button = widget.NewButton("启动", func() {
		if busy || closing || (manager != nil && manager.locked && !running) {
			return
		}
		if coreErr != nil {
			report(coreErr)
			return
		}
		busy = true
		if shell != nil {
			shell.SetModeEnabled(false)
		}
		setManagerLocked(true)
		button.Disable()
		wasRunning := running
		if wasRunning {
			button.SetText("停止中…")
			status.SetText("正在停止连接…")
		} else {
			button.SetText("启动中…")
			status.SetText("正在检查并连接…")
		}
		go func() {
			var operationErr error
			if wasRunning {
				operationErr = backend.Stop()
			} else {
				startCtx, startCancel := context.WithTimeout(ctx, 60*time.Second)
				operationErr = backend.Start(startCtx)
				startCancel()
			}
			fyne.Do(func() {
				if closing {
					return
				}
				busy = false
				running = !wasRunning && operationErr == nil
				if running {
					button.SetText("停止")
					button.SetIcon(theme.MediaStopIcon())
					status.SetText(activeStatus())
				} else {
					button.SetText("启动")
					button.SetIcon(theme.MediaPlayIcon())
					status.SetText("未连接")
				}
				setManagerLocked(running)
				button.Enable()
				if shell != nil {
					shell.SetModeEnabled(true)
					shell.RefreshCurrent()
				}
				report(operationErr)
			})
		}()
	})
	button.Importance = widget.HighImportance
	if manager != nil {
		manager.OnBusy = func(value bool) {
			setEnabled(button, !value && !busy && !closing)
			if shell != nil {
				shell.SetModeEnabled(!value && !busy && !closing)
			}
		}
	}
	shell = newDesktopShell(w, service, manager, status, button, func() bool { return running }, func(mode profile.Mode) {
		if busy || closing || (manager != nil && manager.locked && !running) {
			shell.SyncMode()
			return
		}
		if current, modeErr := store.Mode(); modeErr == nil && current == mode {
			return
		}
		wasRunning := running
		busy = true
		setManagerLocked(true)
		button.Disable()
		shell.SetModeEnabled(false)
		shell.SyncMode()
		status.SetText("正在切换为" + mode.Label() + "…")
		go func() {
			connected, operationErr := application.SwitchMode(ctx, backend, store, mode, wasRunning)
			fyne.Do(func() {
				if closing {
					return
				}
				running, busy = connected, false
				if running {
					button.SetText("停止")
					button.SetIcon(theme.MediaStopIcon())
					status.SetText(activeStatus())
				} else {
					button.SetText("启动")
					button.SetIcon(theme.MediaPlayIcon())
					status.SetText("未启用")
				}
				setManagerLocked(running)
				button.Enable()
				shell.SetModeEnabled(true)
				shell.RefreshCurrent()
				report(operationErr)
			})
		}()
	}, func(enabled bool) {
		if busy || closing || running || (manager != nil && manager.locked) {
			shell.SyncMode()
			return
		}
		report(store.SaveTUN(enabled))
		shell.RefreshCurrent()
	})
	shell.more.startup.attach(autostart.Service{})
	defer shell.more.startup.Close()
	a.Lifecycle().SetOnEnteredForeground(func() {
		fyne.Do(func() {
			if !closing && shell.current == pageMore {
				shell.more.Refresh()
			}
		})
	})
	if shell.strategies != nil {
		shell.strategies.OnSelect = func(group, key string) {
			if busy || closing || (manager != nil && manager.locked && !running) || shell.documents.dirty() {
				return
			}
			wasRunning := running
			busy = true
			setManagerLocked(true)
			button.Disable()
			shell.SetModeEnabled(false)
			status.SetText("正在切换策略 " + group + "…")
			go func() {
				connected, operationErr := application.SwitchStrategy(ctx, backend, store, group, key, wasRunning)
				fyne.Do(func() {
					if closing {
						return
					}
					running, busy = connected, false
					if running {
						button.SetText("停止")
						button.SetIcon(theme.MediaStopIcon())
						status.SetText(activeStatus())
					} else {
						button.SetText("启动")
						button.SetIcon(theme.MediaPlayIcon())
						status.SetText("未连接")
					}
					setManagerLocked(running)
					button.Enable()
					shell.SetModeEnabled(true)
					shell.strategies.Refresh()
					shell.RefreshCurrent()
					report(operationErr)
				})
			}()
		}
	}
	shell.documents.OnBusy = func(value bool) {
		if closing {
			return
		}
		busy = value
		setManagerLocked(value || running)
		setEnabled(button, !value)
		shell.SetModeEnabled(!value)
	}
	shell.documents.Context = ctx
	shell.documents.Running = func() bool { return running }
	shell.documents.SaveRules = func(workCtx context.Context, data, expected []byte, connected bool) (application.RuleSaveResult, error) {
		result, err := service.SaveRules(workCtx, backend, data, expected, connected)
		if result.Running && !d.Running() {
			result.Running = false
			if err == nil {
				err = fmt.Errorf("内核已退出，规则已保存但代理未连接，请重试连接")
			}
		}
		return result, err
	}
	shell.documents.OnRulesApplied = func(result application.RuleSaveResult) {
		running = result.Running
		if running {
			button.SetText("停止")
			button.SetIcon(theme.MediaStopIcon())
			status.SetText(activeStatus())
		} else {
			button.SetText("启动")
			button.SetIcon(theme.MediaPlayIcon())
			status.SetText("未连接")
		}
		shell.SyncMode()
	}
	shell.SetModeEnabled(true)
	if shell.strategies != nil {
		shell.strategies.OnBusy = shell.documents.OnBusy
		defer shell.strategies.Close()
	}
	shell.appearance.Add(newAppearanceSelect(a, appearance))
	a.Settings().AddListener(func(fyne.Settings) {
		if !closing {
			shell.configPane.updateEditorPalette()
		}
	})
	defer shell.documents.Close()
	var closeApp func()
	closeApp = func() {
		if closing {
			return
		}
		closing = true
		shell.more.startup.Close()
		if shell.strategies != nil {
			shell.strategies.Close()
		}
		shell.documents.Close()
		shell.SetModeEnabled(false)
		setManagerLocked(true)
		if manager != nil && manager.cancelWork != nil {
			manager.cancelWork()
		}
		cancel()
		button.Disable()
		go func() {
			if stopErr := backend.Stop(); stopErr != nil {
				fyne.Do(func() {
					dialog.NewError(stopErr, w).Show()
					closing = false
					button.SetText("重试退出")
					button.OnTapped = closeApp
					button.Enable()
				})
				return
			}
			fyne.Do(a.Quit)
		}()
	}
	requestClose := func() {
		if !closing && shell.configPane.HasDraft() {
			dialog.ShowConfirm("配置尚未保存", "退出会丢失未保存的配置修改，是否退出？", func(ok bool) {
				if ok {
					closeApp()
				}
			}, w)
			return
		}
		closeApp()
	}
	w.SetCloseIntercept(requestClose)
	if trayApp, ok := a.(desktop.App); ok {
		bar := newMenuBar(trayApp, func() trayState {
			return shell.menuBarState(busy, closing, !button.Disabled())
		}, fyne.Do, trayActions{
			Toggle: func() { button.OnTapped() },
			Select: func(group, key string) {
				if shell.strategies != nil && shell.strategies.canSelect() && shell.strategies.OnSelect != nil {
					shell.strategies.OnSelect(group, key)
				}
			},
			Show: func() { w.Show(); w.RequestFocus() },
			Quit: requestClose,
		})
		shell.OnStateChanged = bar.Refresh
		defer bar.Close()
	}
	decorateWindow(w, shell, requestClose)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			fyne.Do(closeApp)
		case <-ctx.Done():
		}
	}()
	w.Show()
	configureWindow(w, shell.sidebar.MinSize().Width)
	defer cleanupWindow(w)
	a.Run()
	return nil
}
