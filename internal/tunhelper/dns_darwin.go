package tunhelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"strings"
	"time"
)

// This temporary Dynamic Store key belongs to scutil's connection. Closing
// stdin (also on helper death) removes our resolver without rewriting the
// user's network service settings or requiring a recovery journal.
func startDNS(config map[string]any) (func(), <-chan struct{}, error) {
	var server string
	inbounds, _ := config["inbounds"].([]any)
	for _, item := range inbounds {
		inbound, _ := item.(map[string]any)
		if inbound["type"] != "tun" {
			continue
		}
		addresses, _ := inbound["address"].([]any)
		for _, item := range addresses {
			address, _ := item.(string)
			prefix, err := netip.ParsePrefix(address)
			if err == nil && prefix.Addr().Is4() {
				server = prefix.Addr().Next().String()
				break
			}
		}
	}
	if server == "" {
		return nil, nil, errors.New("TUN 缺少 IPv4 DNS 地址")
	}
	key := fmt.Sprintf("State:/Network/Service/MiniProxy-%d/DNS", time.Now().UnixNano())
	cmd := exec.Command("/usr/sbin/scutil")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	release := func() {
		stdin.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}
	script := "d.init\nd.add ServerAddresses * " + server + "\nd.add SupplementalMatchDomains * \"\"\nd.add SupplementalMatchOrders * # 0\nadd " + key + " temporary\n"
	if _, err := io.WriteString(stdin, script); err != nil {
		release()
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "/usr/sbin/scutil", "-w", key, "-t", "3").Run(); err != nil {
		release()
		return nil, nil, fmt.Errorf("创建 TUN 临时 DNS 失败: %w", err)
	}
	check := exec.CommandContext(ctx, "/usr/sbin/scutil")
	check.Stdin = strings.NewReader("show " + key + "\n")
	output, err := check.CombinedOutput()
	if err != nil || !strings.Contains(string(output), server) || !strings.Contains(string(output), "SupplementalMatchDomains") {
		release()
		return nil, nil, errors.New("TUN 临时 DNS 校验失败")
	}
	return release, done, nil
}
