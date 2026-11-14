package app

import (
	"context"
	"errors"
	"os"

	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

// Only stable node keys cross RPC. Node credentials and the saved routing
// snapshot are resolved by the daemon at the start of the serialized operation.
type BenchmarkRequest struct {
	ID       string                 `json:"id"`
	Options  proxy.BenchmarkOptions `json:"options"`
	NodeKeys []string               `json:"node_keys,omitempty"`
}

type BenchmarkNode struct{ Key, Name, Source string }

func (s *Service) BenchmarkNodes() ([]BenchmarkNode, error) {
	if s.ConfigPath != "" {
		return nil, nil
	}
	sources, err := s.Store.Sources()
	if err != nil {
		return nil, err
	}
	var nodes []BenchmarkNode
	for _, source := range sources {
		for _, node := range source.Nodes {
			nodes = append(nodes, BenchmarkNode{Key: profile.NodeKey(source.ID, node.ID), Name: node.Name, Source: source.Name})
		}
	}
	return nodes, nil
}

func (s *Service) RunBenchmark(ctx context.Context, request BenchmarkRequest, progress func(proxy.BenchmarkProgress)) (proxy.BenchmarkReport, error) {
	options, err := request.Options.Validated()
	if err != nil {
		return proxy.BenchmarkReport{}, err
	}
	request.Options = options
	if request.ID == "" {
		request.ID = profile.NewID()
	}
	if err := ctx.Err(); err != nil {
		return proxy.BenchmarkReport{}, err
	}
	if s.Remote != nil {
		result, err := s.Remote.Execute(ctx, Operation{Action: "benchmark", Benchmark: &request})
		return result.Benchmark, err
	}
	if s.coreErr != nil {
		return proxy.BenchmarkReport{}, s.coreErr
	}
	var config map[string]any
	var targets []proxy.BenchmarkTarget
	if len(request.NodeKeys) > 0 {
		if s.ConfigPath != "" {
			return proxy.BenchmarkReport{}, errors.New("完整配置模式请使用当前分流测试")
		}
		if len(request.NodeKeys) > 128 {
			return proxy.BenchmarkReport{}, errors.New("一次最多选择 128 个节点")
		}
		sources, err := s.Store.Sources()
		if err != nil {
			return proxy.BenchmarkReport{}, err
		}
		byKey := map[string]proxy.BenchmarkTarget{}
		for _, source := range sources {
			for _, node := range source.Nodes {
				key := profile.NodeKey(source.ID, node.ID)
				byKey[key] = proxy.BenchmarkTarget{Key: key, Name: node.Name, Source: source.Name, Outbound: node.Outbound}
			}
		}
		seen := map[string]bool{}
		for _, key := range request.NodeKeys {
			node, exists := byKey[key]
			if !exists {
				return proxy.BenchmarkReport{}, errors.New("所选节点已不可用，请刷新节点列表")
			}
			if seen[key] {
				return proxy.BenchmarkReport{}, errors.New("测试节点不能重复")
			}
			seen[key] = true
			targets = append(targets, node)
		}
	} else {
		var err error
		name := "当前分流"
		if s.ConfigPath != "" {
			var data []byte
			data, err = os.ReadFile(s.ConfigPath)
			if err == nil {
				config, err = profile.ParseDocument(data)
			}
		} else {
			var mode profile.Mode
			mode, err = s.Store.Mode()
			if err == nil {
				name += " · " + mode.Label()
				config, err = s.Store.CompileMode(mode)
			}
		}
		if err != nil {
			return proxy.BenchmarkReport{}, err
		}
		targets = []proxy.BenchmarkTarget{{Key: "routing", Name: name}}
	}
	return (proxy.BenchmarkProber{CorePath: s.CorePath}).Run(ctx, request.ID, request.Options, config, targets, progress)
}
