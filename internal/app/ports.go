package app

import (
	"context"
	"errors"
	"reflect"

	"local/loop/internal/inbound"
	"local/loop/internal/profile"
	"local/loop/internal/proxy"
)

// SaveProxyPorts is serialized by the daemon. Only listener settings may change
// through this operation; rule/policy edits retain their own application rules.
func (s *Service) SaveProxyPorts(ctx context.Context, backend proxy.Backend, draft, expected []byte, running bool) (DocumentSaveResult, error) {
	if s.Remote != nil {
		result, err := s.Remote.Execute(ctx, Operation{Action: "save_ports", Data: draft, Expected: expected})
		return result.Ports, err
	}
	result := DocumentSaveResult{Running: running}
	if s.ConfigPath != "" {
		return result, errors.New("完整配置调试模式请直接编辑配置文件中的入站")
	}
	before, err := profile.ParseDocument(expected)
	if err != nil {
		return result, err
	}
	after, err := profile.ParseDocument(draft)
	if err != nil {
		return result, err
	}
	oldPorts, err := profile.ProxyPorts(before)
	if err != nil {
		return result, err
	}
	ports, err := profile.ProxyPorts(after)
	if err != nil {
		return result, err
	}
	delete(before, "proxy_ports")
	delete(after, "proxy_ports")
	if !reflect.DeepEqual(before, after) {
		return result, errors.New("代理端口设置不能修改规则、策略或其他配置")
	}
	// Existing listeners belong to the live process; probe only additional binds.
	owned := map[string]bool{}
	if running {
		owned[inbound.Address(oldPorts.Default)] = true
		if oldPorts.GlobalEnabled {
			owned[inbound.Address(oldPorts.Global)] = true
		}
	}
	addresses := []string{inbound.Address(ports.Default)}
	if ports.GlobalEnabled {
		addresses = append(addresses, inbound.Address(ports.Global))
	}
	var changed []string
	for _, address := range addresses {
		if !owned[address] {
			changed = append(changed, address)
		}
	}
	if err := inbound.CheckAvailable(changed); err != nil {
		return result, err
	}
	return s.saveLiveDocument(ctx, backend, draft, expected, running, portsImpact)
}
