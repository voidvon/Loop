package app

import (
	"context"
	"errors"
	"fmt"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

// StrategyProbeTargets snapshots current members and resolves group references
// to their selected leaf nodes. Taking a snapshot never persists a choice.
func (s *Service) StrategyProbeTargets(group string) ([]proxy.ProbeTarget, error) {
	states, err := s.Store.PolicyStates()
	if err != nil {
		return nil, err
	}
	sources, err := s.Store.Sources()
	if err != nil {
		return nil, err
	}
	nodes := map[string]profile.Node{}
	groups := map[string]profile.PolicyState{}
	for _, source := range sources {
		for _, node := range source.Nodes {
			nodes[profile.NodeKey(source.ID, node.ID)] = node
		}
	}
	for _, state := range states {
		groups[state.Group.Name] = state
	}
	state, ok := groups[group]
	if !ok {
		return nil, errors.New("策略组不存在")
	}
	var leaf func(string) (string, error)
	leaf = func(key string) (string, error) {
		if _, ok := nodes[key]; ok {
			return key, nil
		}
		child, ok := groups[key]
		if !ok {
			return "", errors.New("节点已不可用")
		}
		if child.Error != "" {
			return "", fmt.Errorf("%s：%s", key, child.Error)
		}
		return leaf(child.Selected)
	}
	seen := map[string]bool{}
	targets := []proxy.ProbeTarget{}
	for _, member := range state.Members {
		key, err := leaf(member.Key)
		if err != nil {
			continue
		} // Empty optional countries have nothing to measure.
		if seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, proxy.ProbeTarget{Key: key, Outbound: nodes[key].Outbound})
	}
	if len(targets) == 0 {
		return nil, errors.New("当前组没有可测速节点")
	}
	return targets, nil
}
func (s *Service) MeasureLatencies(ctx context.Context, targets []proxy.ProbeTarget, result func(string, proxy.LatencyResult)) error {
	if s.Remote != nil {
		values, err := s.Remote.Execute(ctx, Operation{Action: "measure_latencies", Targets: targets})
		for _, value := range values.Latencies {
			var probeErr error
			if value.Error != "" {
				probeErr = errors.New(value.Error)
			}
			if value.Timeout {
				probeErr = context.DeadlineExceeded
			}
			if value.Cancelled {
				probeErr = context.Canceled
			}
			result(value.Key, proxy.LatencyResult{Duration: value.Duration, Err: probeErr})
		}
		return err
	}
	if s.coreErr != nil {
		return s.coreErr
	}
	return (proxy.LatencyProber{CorePath: s.CorePath}).Run(ctx, targets, result)
}
