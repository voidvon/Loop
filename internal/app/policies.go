package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

// SwitchStrategy serializes with mode/connection changes. The core is restarted
// transactionally, so UI selection and running traffic cannot silently diverge.
func SwitchStrategy(ctx context.Context, backend proxy.Backend, store profile.Store, group, key string, running bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return running, err
	}
	previous, err := store.StrategyChoice(group)
	if err != nil {
		return running, err
	}
	if previous == key {
		// Persist an implicit first member when the user explicitly chooses it.
		return running, store.SelectStrategy(group, key)
	}
	if err := store.SelectStrategy(group, key); err != nil {
		return running, err
	}
	if !running {
		return false, nil
	}
	rollback := func() error { return store.SelectStrategy(group, previous) }
	if _, err := store.Compile(); err != nil {
		return true, errors.Join(err, rollback())
	}
	if mode, _ := store.Mode(); mode == profile.ModeDirect {
		return true, nil
	}
	start := func() error {
		startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return backend.Start(startCtx)
	}
	if err := backend.Stop(); err != nil {
		return false, errors.Join(err, rollback())
	}
	if err := start(); err != nil {
		cleanupErr := backend.Stop()
		restoreErr := rollback()
		if cleanupErr != nil || restoreErr != nil || ctx.Err() != nil {
			return false, errors.Join(err, cleanupErr, restoreErr, ctx.Err())
		}
		restartErr := start()
		return restartErr == nil, errors.Join(fmt.Errorf("切换失败，已恢复原策略选择：%w", err), restartErr)
	}
	return true, nil
}

// SavePolicies applies group definitions in every mode, preserving the live
// connection and current choices. The daemon serializes it with selections.
func (s *Service) SavePolicies(ctx context.Context, backend proxy.Backend, draft, expected []byte, running bool) (DocumentSaveResult, error) {
	if s.Remote != nil {
		result, err := s.Remote.Execute(ctx, Operation{Action: "save_policies", Data: draft, Expected: expected})
		return result.Policies, err
	}
	if s.ConfigPath != "" {
		return DocumentSaveResult{Running: running}, errors.New("完整配置模式不支持编辑策略组")
	}
	if err := s.Store.ValidatePolicyEdit(draft, expected); err != nil {
		return DocumentSaveResult{Running: running}, err
	}
	return s.saveLiveDocument(ctx, backend, draft, expected, running, false)
}
