// Package inbound defines the local listeners owned by the client.
package inbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
)

const (
	DefaultTag = "mixed-in"
	GlobalTag  = "global-in"
	TUNTag     = "tun-in"
)

// Ports is the single source of listener settings. Runtime inbounds are derived
// from it; disabling the global listener retains its configured port.
type Ports struct {
	Default       int  `json:"default"`
	Global        int  `json:"global"`
	GlobalEnabled bool `json:"global_enabled"`
}

func DefaultPorts() Ports { return Ports{Default: 2080, Global: 2081, GlobalEnabled: true} }

func (p Ports) Validate() error {
	if p.Default < 1024 || p.Default > 65535 || p.Global < 1024 || p.Global > 65535 {
		return errors.New("代理端口必须是 1024–65535 的整数")
	}
	if p.Default == p.Global {
		return errors.New("默认代理与全局代理不能使用相同端口")
	}
	return nil
}

func Address(port int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }

func (p Ports) Inbounds() []any {
	listener := func(tag string, port int) any {
		return map[string]any{"type": "mixed", "tag": tag, "listen": "127.0.0.1", "listen_port": port}
	}
	result := []any{listener(DefaultTag, p.Default)}
	if p.GlobalEnabled {
		result = append(result, listener(GlobalTag, p.Global))
	}
	return result
}

type Endpoints struct {
	Default string
	Global  string
	TUN     bool
}

func (e Endpoints) Addresses() []string {
	result := []string{e.Default}
	if e.Global != "" {
		result = append(result, e.Global)
	}
	return result
}

// Parse validates owned listeners by identity, independently of array order.
// The same contract is enforced by the desktop and the privileged TUN helper.
func Parse(config []byte) (Endpoints, error) {
	var c struct {
		Inbounds []struct {
			Type           string `json:"type"`
			Tag            string `json:"tag"`
			Listen         string `json:"listen"`
			Port           int    `json:"listen_port"`
			SetSystemProxy bool   `json:"set_system_proxy"`
		} `json:"inbounds"`
	}
	var result Endpoints
	if err := json.Unmarshal(config, &c); err != nil {
		return result, fmt.Errorf("读取代理入口配置：%w", err)
	}
	seen := map[string]bool{}
	for _, in := range c.Inbounds {
		if seen[in.Tag] {
			return result, errors.New("代理入口标识重复")
		}
		seen[in.Tag] = true
		if in.Tag == TUNTag && in.Type == "tun" {
			result.TUN = true
			continue
		}
		if in.Type != "mixed" || (in.Tag != DefaultTag && in.Tag != GlobalTag) || in.Listen != "127.0.0.1" || in.Port < 1024 || in.Port > 65535 || in.SetSystemProxy {
			return result, errors.New("仅支持本机 mixed-in / global-in 代理入口（1024–65535）及 tun-in；系统代理由客户端管理")
		}
		if in.Tag == DefaultTag {
			result.Default = Address(in.Port)
		} else {
			result.Global = Address(in.Port)
		}
	}
	if result.Default == "" || result.Default == result.Global {
		return result, errors.New("需要默认代理入口，且两个代理端口不能相同")
	}
	return result, nil
}

// CheckAvailable holds all candidate binds until validation is complete.
// The core's bind remains authoritative if a port is claimed afterwards.
func CheckAvailable(addresses []string) error {
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			listener.Close()
		}
	}()
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("代理端口 %s 无法使用：%w", address, err)
		}
		listeners = append(listeners, listener)
	}
	return nil
}
