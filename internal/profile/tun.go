package profile

import "path/filepath"

// TUN is a capture setting, independent of routing mode and node selection.
func (s Store) TUN() (bool, error) {
	settings, err := s.settings()
	return settings.TUN, err
}

func (s Store) SaveTUN(enabled bool) error {
	settings, err := s.settings()
	if err != nil {
		return err
	}
	settings.TUN = enabled
	return WriteJSON(filepath.Join(s.Dir, "settings.json"), settings)
}

func applyTUN(config map[string]any) {
	// TUN's privileged session owns only temporary files; it does not write a
	// root-owned cache into the user's profile directory.
	delete(config, "experimental")
	config["log"] = map[string]any{"level": "info", "timestamp": true}
	inbounds, _ := config["inbounds"].([]any)
	config["inbounds"] = append(inbounds, map[string]any{
		"type": "tun", "tag": "tun-in", "mtu": 1500,
		"address":    []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
		"auto_route": true, "dns_mode": "hijack",
	})
	route := config["route"].(map[string]any)
	route["auto_detect_interface"] = true
	rules, _ := route["rules"].([]any)
	route["rules"] = append([]any{
		map[string]any{"inbound": "tun-in", "action": "sniff"},
		map[string]any{"inbound": "tun-in", "port": 53, "action": "hijack-dns"},
	}, rules...)
	if dns, ok := config["dns"].(map[string]any); ok {
		dns["reverse_mapping"] = true
		// Native system DNS now points into the TUN. An explicit direct resolver
		// avoids recursively asking that resolver for the proxy server itself.
		servers, _ := dns["servers"].([]any)
		for i, item := range servers {
			if server, ok := item.(map[string]any); ok && server["type"] == "local" {
				servers[i] = map[string]any{"type": "udp", "tag": server["tag"], "server": "223.5.5.5", "detour": "direct"}
			}
		}
	}
}
