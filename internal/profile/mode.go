package profile

import (
	"errors"
	"os"
	"path/filepath"
)

type Mode string

const (
	ModeDirect Mode = "direct"
	ModeRule   Mode = "rule"
	ModeGlobal Mode = "global"
)

func (m Mode) Valid() bool { return m == ModeDirect || m == ModeRule || m == ModeGlobal }

func (m Mode) Label() string {
	switch m {
	case ModeDirect:
		return "全局直连"
	case ModeRule:
		return "自动分流"
	case ModeGlobal:
		return "全局代理"
	default:
		return "未知模式"
	}
}

func (m Mode) Description() string {
	switch m {
	case ModeDirect:
		return "所有进入本软件的流量直连，不使用代理节点。"
	case ModeRule:
		return "按已保存的分流规则处理流量，默认国内直连、其他代理。"
	case ModeGlobal:
		return "局域网保持直连，其余进入本软件的流量均使用 Available 默认策略。"
	default:
		return "请重新选择分流模式。"
	}
}

type Settings struct {
	Mode Mode `json:"mode"`
	TUN  bool `json:"tun"`
}

func (s Store) settings() (Settings, error) {
	settings := Settings{Mode: ModeRule}
	err := readJSON(filepath.Join(s.Dir, "settings.json"), &settings)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	return settings, err
}

func (s Store) Mode() (Mode, error) {
	settings, err := s.settings()
	if err != nil {
		return "", err
	}
	if !settings.Mode.Valid() {
		return "", errors.New("分流模式无效，请在首页重新选择")
	}
	return settings.Mode, nil
}

func (s Store) SaveMode(mode Mode) error {
	if !mode.Valid() {
		return errors.New("不支持的分流模式")
	}
	settings, err := s.settings()
	if err != nil {
		return err
	}
	settings.Mode = mode
	return WriteJSON(filepath.Join(s.Dir, "settings.json"), settings)
}

// CompileMode produces a temporary mode override, leaving the user's rules,
// nodes, subscriptions and current selection untouched.
func (s Store) CompileMode(mode Mode) (map[string]any, error) {
	config, err := s.compileMode(mode)
	if err != nil {
		return nil, err
	}
	tun, err := s.TUN()
	if err != nil {
		return nil, err
	}
	if tun {
		applyTUN(config)
	}
	return config, nil
}

func (s Store) compileMode(mode Mode) (map[string]any, error) {
	if !mode.Valid() {
		return nil, errors.New("不支持的分流模式")
	}
	// Candidate compilation validates policy structure without requiring a selected node.
	var candidate *Node
	if mode != ModeDirect {
		candidate = &Node{ID: "policy-check", Outbound: map[string]any{"type": "socks", "server": "127.0.0.1", "server_port": 9}}
	}
	config, err := s.compileNode(candidate)
	if err != nil {
		return nil, err
	}
	if mode != ModeDirect {
		// Global mode ignores individual application routing, but all modes use Available.
		if mode == ModeGlobal {
			config["route"] = map[string]any{"final": DefaultPolicy}
			delete(config, "dns")
			delete(config, "http_clients")
		}
		config, err = s.compileStrategies(config)
		if err != nil {
			return nil, err
		}
	}
	if mode == ModeRule {
		return config, nil
	}
	route := config["route"].(map[string]any)
	delete(route, "rule_set")
	delete(config, "http_clients")
	route["default_domain_resolver"] = "dns-direct"
	route["rules"] = []any{}
	route["final"] = "direct"
	dns := map[string]any{
		"servers": []any{map[string]any{"type": "local", "tag": "dns-direct"}},
		"final":   "dns-direct", "strategy": "prefer_ipv4",
	}
	if mode == ModeGlobal {
		localDomains := []string{"lan", "local", "localhost"}
		route["rules"] = []any{
			map[string]any{"domain_suffix": localDomains, "action": "route", "outbound": "direct"},
			map[string]any{"action": "resolve"},
			map[string]any{"ip_is_private": true, "action": "route", "outbound": "direct"},
		}
		route["final"] = DefaultPolicy
		dns["servers"] = append(dns["servers"].([]any), map[string]any{"type": "https", "tag": "dns-proxy", "server": "1.1.1.1", "detour": DefaultPolicy})
		dns["rules"] = []any{map[string]any{"domain_suffix": localDomains, "action": "route", "server": "dns-direct"}}
		dns["final"] = "dns-proxy"
	}
	config["dns"] = dns
	return config, nil
}
