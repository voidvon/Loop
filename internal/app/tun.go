package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

// SwitchTUN runs under the daemon's operation lock. Stop waits for the previous
// capture method to release its system proxy or TUN/DNS session before restart.
func SwitchTUN(ctx context.Context, backend proxy.Backend, store profile.Store, enabled, running bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return running, err
	}
	previous, err := store.TUN()
	if err != nil {
		return running, err
	}
	if enabled == previous {
		return running, nil
	}
	if err := store.SaveTUN(enabled); err != nil {
		return running, fmt.Errorf("保存 TUN 设置失败：%w", err)
	}
	if !running {
		return false, nil
	}
	restore := func() error {
		if err := store.SaveTUN(previous); err != nil {
			return fmt.Errorf("恢复原 TUN 设置失败：%w", err)
		}
		return nil
	}
	if _, err := store.Compile(); err != nil {
		return true, errors.Join(err, restore())
	}
	if err := ctx.Err(); err != nil {
		return true, errors.Join(err, restore())
	}
	if err := backend.Stop(); err != nil {
		return false, errors.Join(fmt.Errorf("停止旧连接失败，未切换接管方式：%w", err), restore())
	}
	start := func() error {
		startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return backend.Start(startCtx)
	}
	if err := start(); err != nil {
		cleanupErr := backend.Stop()
		restoreErr := restore()
		if cleanupErr != nil || restoreErr != nil || ctx.Err() != nil {
			return false, errors.Join(fmt.Errorf("切换接管方式失败：%w", err), cleanupErr, restoreErr, ctx.Err())
		}
		if restartErr := start(); restartErr != nil {
			return false, errors.Join(fmt.Errorf("切换接管方式失败，已恢复原设置：%w", err), fmt.Errorf("原连接恢复失败，请重试连接：%w", restartErr), backend.Stop())
		}
		return true, fmt.Errorf("切换接管方式失败，已恢复原设置并重新连接：%w", err)
	}
	return true, nil
}
