package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"local/miniproxy/internal/profile"
	"local/miniproxy/internal/proxy"
)

// The UI serializes this operation with connection and profile edits.
func SwitchMode(ctx context.Context, backend proxy.Backend, store profile.Store, mode profile.Mode, running bool) (bool, error) {
	if !mode.Valid() {
		return running, errors.New("不支持的分流模式")
	}
	previous, err := store.Mode()
	if err != nil {
		// While stopped, selecting a valid mode can repair an invalid settings file.
		if running {
			return running, err
		}
		return false, store.SaveMode(mode)
	}
	if mode == previous {
		return running, nil
	}
	if !running {
		return false, store.SaveMode(mode)
	}
	if _, err := store.CompileMode(mode); err != nil {
		return true, err
	}
	if err := backend.Stop(); err != nil {
		return false, fmt.Errorf("停止连接失败，模式未修改：%w", err)
	}
	start := func() error {
		startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return backend.Start(startCtx)
	}
	if err := store.SaveMode(mode); err != nil {
		restartErr := start()
		return restartErr == nil, errors.Join(fmt.Errorf("保存模式失败：%w", err), restartErr)
	}
	if err := start(); err != nil {
		cleanupErr := backend.Stop()
		if restoreErr := store.SaveMode(previous); restoreErr != nil {
			return false, errors.Join(fmt.Errorf("切换失败：%w", err), cleanupErr, fmt.Errorf("恢复原模式设置失败：%w", restoreErr))
		}
		if cleanupErr != nil {
			return false, errors.Join(err, cleanupErr)
		}
		if ctx.Err() != nil {
			return false, errors.Join(err, ctx.Err())
		}
		if restartErr := start(); restartErr != nil {
			return false, errors.Join(fmt.Errorf("切换失败，已恢复原模式设置：%w", err), fmt.Errorf("重新连接原模式失败：%w", restartErr))
		}
		return true, fmt.Errorf("切换失败，已恢复%s并重新连接：%w", previous.Label(), err)
	}
	return true, nil
}
