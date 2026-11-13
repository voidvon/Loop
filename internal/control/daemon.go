package control

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"local/loop/internal/app"
	"local/loop/internal/statusbar"
)

func RunDaemon(args []string) error {
	flags := flag.NewFlagSet("loopd", flag.ContinueOnError)
	profiles := flags.String("profiles", "", "配置目录")
	stateDir := flags.String("state-dir", "", "后台状态目录")
	config := flags.String("config", "", "完整配置调试路径")
	core := flags.String("core", "", "内核路径")
	helper := flags.String("tun-helper", "", "TUN 辅助程序")
	ui := flags.String("ui", "", "图形界面可执行文件")
	noTray := flags.Bool("no-tray", false, "不创建 macOS 菜单栏，仅运行本地 API")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("后台不接受位置参数")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if *stateDir == "" {
		defaultState, err := app.DefaultStateDirectory()
		if err != nil {
			return err
		}
		*stateDir = defaultState
	}
	*stateDir, err = filepath.Abs(*stateDir)
	if err != nil {
		return err
	}
	options := app.Options{Executable: executable, StateDir: *stateDir, CorePath: *core, HelperPath: *helper}
	if *ui != "" {
		options.Executable = *ui
	} else {
		options.Executable = UIExecutable(options)
	}
	if *config != "" {
		options.ConfigPath, err = filepath.Abs(*config)
	} else {
		options.Store.Dir, err = app.ProfileDirectory(options.Executable, *stateDir, *profiles)
	}
	if err != nil {
		return err
	}
	uiOptions := options
	options = RuntimePaths(uiOptions)
	controller := New(options)
	controller.ShowUI = func() error { return ShowUI(context.Background(), uiOptions) }
	server, err := NewServer(controller)
	if err != nil {
		controller.cancel()
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *noTray {
		return server.Serve(ctx)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	stop := make(chan struct{})
	result := make(chan error, 1)
	go func() { result <- server.Serve(ctx); close(stop) }()
	updates := make(chan statusbar.State, 1)
	convert := func(snapshot Snapshot) statusbar.State {
		return statusbar.State{Running: snapshot.Running, Busy: snapshot.Busy, Closing: snapshot.Closing, Mode: snapshot.Mode, Custom: snapshot.Config != "", Error: snapshot.PolicyError, OperationError: snapshot.Error, Policies: snapshot.Policies, Preferences: snapshot.Tray}
	}
	go func() {
		for {
			select {
			case <-controller.Changed:
				state := convert(controller.Snapshot())
				select {
				case updates <- state:
				default:
					select {
					case <-updates:
					default:
					}
					select {
					case updates <- state:
					default:
					}
				}
			case <-stop:
				return
			}
		}
	}()
	err = statusbar.Run(convert(controller.Snapshot()), updates, stop, func(operation app.Operation) {
		if operation.Action == "shutdown" {
			if RequestUIQuit(context.Background(), options.StateDir) == nil {
				return
			}
			cancel()
			return
		}
		if _, err := controller.Execute(context.Background(), operation); err != nil {
			log.Printf("菜单栏操作失败：%v", err)
		}
	})
	if err != nil {
		cancel()
	}
	return errors.Join(err, <-result)
}
