package inbound

import (
	"encoding/json"
	"net"
	"testing"
)

func TestOwnedListenerIdentityAndValidation(t *testing.T) {
	base := DefaultPorts().Inbounds()
	// System proxy selection must survive list reordering and an optional TUN.
	data, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"type": "tun", "tag": TUNTag}, base[1], base[0]}})
	endpoints, err := Parse(data)
	if err != nil || endpoints.Default != "127.0.0.1:2080" || endpoints.Global != "127.0.0.1:2081" || !endpoints.TUN {
		t.Fatal(endpoints, err)
	}
	for _, name := range []string{"same port", "duplicate tag", "unknown", "missing default", "external", "system proxy", "privileged", "fraction"} {
		t.Run(name, func(t *testing.T) {
			ins := DefaultPorts().Inbounds()
			first, second := ins[0].(map[string]any), ins[1].(map[string]any)
			switch name {
			case "same port":
				second["listen_port"] = 2080
			case "duplicate tag":
				second["tag"] = DefaultTag
			case "unknown":
				second["tag"] = "unowned"
			case "missing default":
				ins = ins[1:]
			case "external":
				first["listen"] = "0.0.0.0"
			case "system proxy":
				second["set_system_proxy"] = true
			case "privileged":
				second["listen_port"] = 80
			case "fraction":
				second["listen_port"] = 2081.5
			}
			data, _ := json.Marshal(map[string]any{"inbounds": ins})
			if _, err := Parse(data); err == nil {
				t.Fatal("invalid listener accepted")
			}
		})
	}
}

func TestAllPortsMustBeAvailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := CheckAvailable([]string{"127.0.0.1:0", listener.Addr().String()}); err == nil {
		t.Fatal("occupied second listener accepted")
	}
}
