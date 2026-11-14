package app

import (
	"context"
	"os"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

// TestRouting snapshots saved configuration and choices without persisting any
// state or touching the active proxy. The daemon serializes it with mutations.
func (s *Service) TestRouting(ctx context.Context, value string) (proxy.RoutingResult, error) {
	if s.Remote != nil {
		result, err := s.Remote.Execute(ctx, Operation{Action: "test_routing", URL: value})
		return result.Routing, err
	}
	if s.coreErr != nil {
		return proxy.RoutingResult{}, s.coreErr
	}
	var config map[string]any
	var err error
	mode := "完整配置"
	if s.ConfigPath != "" {
		var data []byte
		data, err = os.ReadFile(s.ConfigPath)
		if err == nil {
			config, err = profile.ParseDocument(data)
		}
	} else {
		var current profile.Mode
		current, err = s.Store.Mode()
		if err == nil {
			mode = current.Label()
			config, err = s.Store.CompileMode(current)
		}
	}
	if err != nil {
		return proxy.RoutingResult{}, err
	}
	names, sources := map[string]string{}, map[string]string{}
	if s.ConfigPath == "" {
		all, err := s.Store.Sources()
		if err != nil {
			return proxy.RoutingResult{}, err
		}
		for _, source := range all {
			for _, node := range source.Nodes {
				key := profile.NodeKey(source.ID, node.ID)
				names[key], sources[key] = node.Name, source.Name
			}
		}
	}
	result, err := (proxy.RoutingProber{CorePath: s.CorePath}).Run(ctx, config, value)
	result.Mode = mode
	result.Node, result.Source = result.Outbound, sources[result.Outbound]
	if name := names[result.Outbound]; name != "" {
		result.Node = name
	}
	for i, key := range result.Chain {
		if name := names[key]; name != "" {
			result.Chain[i] = name
		}
	}
	if result.Direct {
		result.Node = "DIRECT · 直连"
	}
	if result.Rejected {
		result.Node = "REJECT · 拒绝"
	}
	return result, err
}
