package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"local/loop/internal/inbound"
)

func ProxyPorts(config map[string]any) (inbound.Ports, error) {
	var ports inbound.Ports
	value, ok := config["proxy_ports"]
	if !ok || value == nil {
		return ports, errors.New("主配置需要 proxy_ports 代理端口设置")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ports, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ports); err != nil {
		return ports, fmt.Errorf("代理端口设置无效：%w", err)
	}
	return ports, ports.Validate()
}

func (s Store) ProxyPorts() (inbound.Ports, error) {
	config, err := s.Config()
	if err != nil {
		return inbound.Ports{}, err
	}
	return ProxyPorts(config)
}

func ChangeProxyPorts(data []byte, ports inbound.Ports) ([]byte, error) {
	if err := ports.Validate(); err != nil {
		return nil, err
	}
	config, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	config["proxy_ports"] = ports
	encoded, err := json.MarshalIndent(config, "", "  ")
	return append(encoded, '\n'), err
}
