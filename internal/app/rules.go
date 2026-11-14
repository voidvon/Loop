package app

import (
	"context"
	"local/loop/internal/proxy"
)

// SaveRules applies a serialized rule edit while preserving connection intent.
// The caller's context represents application shutdown, not a restart timeout;
// each individual start has its own deadline so rollback can still reconnect.
func (s *Service) SaveRules(ctx context.Context, backend proxy.Backend, draft, expected []byte, running bool) (RuleSaveResult, error) {
	if s.Remote != nil {
		result, err := s.Remote.Execute(ctx, Operation{Action: "save_rules", Data: draft, Expected: expected})
		return result.Rules, err
	}
	return s.saveLiveDocument(ctx, backend, draft, expected, running, rulesImpact)
}
