package tunhelper

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Refuse to replace another VPN's routes: sing-tun can replace existing split
// default routes, which cannot be reconstructed by merely closing our TUN.
func CheckRouteConflict() error {
	for _, address := range []string{"1.1.1.1", "200.1.1.1", "2606:4700:4700::1111"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		output, err := exec.CommandContext(ctx, "/sbin/route", "-n", "get", address).Output()
		cancel()
		if err != nil {
			continue
		} // An IPv6 route need not exist.
		if iface := tunnelRouteInterface(string(output)); iface != "" {
			return fmt.Errorf("已有 VPN / TUN 正在接管网络（%s），请先关闭它，再启动 MiniProxy 的 TUN 模式", iface)
		}
	}
	return nil
}

func tunnelRouteInterface(output string) string {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || key != "interface" {
			continue
		}
		value = strings.TrimSpace(value)
		for _, prefix := range []string{"utun", "ipsec", "ppp"} {
			if strings.HasPrefix(value, prefix) {
				return value
			}
		}
	}
	return ""
}
