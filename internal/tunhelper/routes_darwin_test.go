package tunhelper

import "testing"

func TestTunnelRouteDetection(t *testing.T) {
	for _, iface := range []string{"en0", "en1", "utun6", "ipsec0", "ppp0"} {
		got := tunnelRouteInterface("destination: default\n  interface: " + iface + "\n  flags: UP")
		want := iface
		if iface == "en0" || iface == "en1" {
			want = ""
		}
		if got != want {
			t.Fatal(got, want)
		}
	}
}
