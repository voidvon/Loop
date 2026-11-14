package profile

import (
	"errors"
	"os"
	"path/filepath"

	"local/loop/internal/inbound"
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
		return "默认入口与 TUN 流量直连；独立全局入口仍使用默认代理策略。"
	case ModeRule:
		return "默认入口与 TUN 按已保存的分流规则处理流量。"
	case ModeGlobal:
		return "默认入口与 TUN 使用默认代理策略，局域网保持直连。"
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
	ports, err := s.ProxyPorts()
	if err != nil {
		return nil, err
	}
	needsProxy := mode != ModeDirect || ports.GlobalEnabled
	var candidate *Node
	if needsProxy {
		candidate = &Node{ID: "policy-check", Outbound: map[string]any{"type": "socks", "server": "127.0.0.1", "server_port": 9}}
	}
	config, err := s.compileNode(candidate)
	if err != nil {
		return nil, err
	}
	defaultPolicy, err := s.DefaultStrategy()
	if err != nil {
		return nil, err
	}
	if (mode == ModeGlobal || ports.GlobalEnabled) && defaultPolicy == "" {
		return nil, errors.New("未设置全局代理默认策略，请在策略页设置")
	}
	if mode != ModeRule {
		applyModeRouting(config, mode, defaultPolicy)
	}
	if ports.GlobalEnabled {
		if err := applyGlobalPort(config, defaultPolicy); err != nil {
			return nil, err
		}
	}
	if needsProxy {
		return s.compileStrategies(config)
	}
	return config, nil
}

func applyModeRouting(config map[string]any, mode Mode, defaultPolicy string) {
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
		route["final"] = defaultPolicy
		dns["servers"] = append(dns["servers"].([]any), map[string]any{"type": "https", "tag": "dns-proxy", "server": "1.1.1.1", "detour": defaultPolicy})
		dns["rules"] = []any{map[string]any{"domain_suffix": localDomains, "action": "route", "server": "dns-direct"}}
		dns["final"] = "dns-proxy"
	}
	config["dns"] = dns
}

// Global-port rules precede saved rules and are always scoped to their inbound.
// Its DNS transports and caches are separate from the default inlet's DNS policy.
func applyGlobalPort(config map[string]any, policy string) error {
	const proxyDNS = "loop-global-dns"
	const localDNS = "loop-global-local"
	localDomains := []string{"lan", "local", "localhost"}
	dns, _ := config["dns"].(map[string]any)
	if dns == nil {
		dns = map[string]any{}
		config["dns"] = dns
	}
	servers, _ := dns["servers"].([]any)
	for _, item := range servers {
		server, _ := item.(map[string]any)
		if server["tag"] == proxyDNS || server["tag"] == localDNS {
			return errors.New("DNS 标识 loop-global-dns / loop-global-local 由全局代理入口保留")
		}
	}
	dns["servers"] = append(servers,
		map[string]any{"type": "local", "tag": localDNS},
		map[string]any{"type": "https", "tag": proxyDNS, "server": "1.1.1.1", "detour": policy})
	dnsRules, _ := dns["rules"].([]any)
	dns["rules"] = append([]any{
		map[string]any{"inbound": inbound.GlobalTag, "domain_suffix": localDomains, "action": "route", "server": localDNS},
		map[string]any{"inbound": inbound.GlobalTag, "action": "route", "server": proxyDNS},
	}, dnsRules...)
	route := config["route"].(map[string]any)
	rules, _ := route["rules"].([]any)
	route["rules"] = append([]any{
		map[string]any{"inbound": inbound.GlobalTag, "domain_suffix": localDomains, "action": "route", "outbound": "direct"},
		map[string]any{"inbound": inbound.GlobalTag, "action": "resolve", "server": proxyDNS},
		map[string]any{"inbound": inbound.GlobalTag, "ip_is_private": true, "action": "route", "outbound": "direct"},
		map[string]any{"inbound": inbound.GlobalTag, "action": "route", "outbound": policy},
	}, rules...)
	return nil
}
