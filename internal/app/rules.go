package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

type RuleSaveResult struct {
	Running bool `json:"running"`
	// Document is the resulting source, including a successful rollback on error.
	Document []byte `json:"document,omitempty"`
}

// SaveRules applies a serialized rule edit while preserving connection intent.
// The caller's context represents application shutdown, not a restart timeout;
// each individual start has its own deadline so rollback can still reconnect.
func (s *Service) SaveRules(ctx context.Context, backend proxy.Backend, draft, expected []byte, running bool) (RuleSaveResult, error) {
	if s.Remote != nil {
		result, err := s.Remote.Execute(ctx, Operation{Action: "save_rules", Data: draft, Expected: expected})
		return result.Rules, err
	}
	result := RuleSaveResult{Running: running}
	checkCtx, checkCancel := context.WithTimeout(ctx, 20*time.Second)
	saved, err := s.SaveDocument(checkCtx, draft, expected)
	checkCancel()
	if err != nil {
		return result, err
	}
	result.Document = saved
	if !running || bytes.Equal(saved, expected) {
		return result, nil
	}
	rollback := func() error {
		_, path := s.Document()
		if err := profile.RestoreDocument(path, expected, saved); err != nil {
			result.Document = nil
			return fmt.Errorf("恢复原配置失败：%w", err)
		}
		result.Document = append([]byte(nil), expected...)
		return nil
	}
	// Global overrides do not use saved local rules, so keep their core running.
	if s.ConfigPath == "" {
		mode, err := s.Store.Mode()
		if err == nil && mode != profile.ModeRule {
			return result, nil
		}
		if err == nil {
			var config map[string]any
			config, err = s.Store.Compile()
			if err == nil {
				checkCtx, checkCancel := context.WithTimeout(ctx, 20*time.Second)
				err = proxy.CheckConfiguration(checkCtx, s.CorePath, config)
				checkCancel()
			}
		}
		if err != nil {
			return result, errors.Join(err, rollback())
		}
	}
	if err := ctx.Err(); err != nil {
		return result, errors.Join(err, rollback())
	}
	start := func() error {
		startCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return backend.Start(startCtx)
	}
	if err := backend.Stop(); err != nil {
		result.Running = false
		return result, errors.Join(fmt.Errorf("停止旧连接失败，未应用新规则，请重试连接：%w", err), rollback())
	}
	result.Running = false
	if err := start(); err != nil {
		cleanupErr := backend.Stop()
		restoreErr := rollback()
		if cleanupErr != nil || restoreErr != nil || ctx.Err() != nil {
			return result, errors.Join(fmt.Errorf("新规则连接失败：%w", err), cleanupErr, restoreErr, ctx.Err())
		}
		if restartErr := start(); restartErr != nil {
			return result, errors.Join(fmt.Errorf("新规则连接失败，已恢复原配置：%w", err), fmt.Errorf("原连接恢复失败，请重试连接：%w", restartErr), backend.Stop())
		}
		result.Running = true
		return result, fmt.Errorf("新规则连接失败，已恢复原配置并重新连接：%w", err)
	}
	result.Running = true
	return result, nil
}
