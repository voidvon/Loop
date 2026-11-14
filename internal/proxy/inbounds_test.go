package proxy

import "testing"

func TestOwnedInbounds(t *testing.T) {
	for _, test := range []struct {
		config string
		valid  bool
		tun    bool
	}{
		{`{"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2080}]}`, true, false},
		{`{"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2080},{"type":"tun","tag":"tun-in"}]}`, true, true},
		{`{"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"0.0.0.0","listen_port":2080},{"type":"tun","tag":"tun-in"}]}`, false, true},
		{`{"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2080,"set_system_proxy":true},{"type":"tun","tag":"tun-in"}]}`, false, true},
		{`{"inbounds":[{"type":"mixed","tag":"mixed-in","listen":"127.0.0.1","listen_port":2080},{"type":"mixed"}]}`, false, false},
	} {
		_, err := LocalAddress([]byte(test.config))
		if (err == nil) != test.valid || HasTUN([]byte(test.config)) != test.tun {
			t.Fatal(test.config, err)
		}
	}
}
