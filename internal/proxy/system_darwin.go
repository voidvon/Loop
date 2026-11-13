package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("另一个 Loop 实例正在运行，请先停止它")
	}
	return f, nil
}

type proxySetting struct {
	Service string
	Kind    string
	Enabled bool
	Server  string
	Port    string
}

type systemProxy struct {
	journal string
	// Injectable boundaries keep rollback tests independent of host settings.
	services func() ([]string, error)
	run      func(...string) (string, error)
}

func (p *systemProxy) command(args ...string) (string, error) {
	if p.run != nil {
		return p.run(args...)
	}
	return networksetup(args...)
}

var serviceLine = regexp.MustCompile(`^\((\d+|\*)\) (.+)$`)
var deviceLine = regexp.MustCompile(`Device: ([^)]+)\)`)

func networksetup(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", args...).CombinedOutput()
	text := strings.TrimSpace(string(output))
	// networksetup sometimes reports errors with a successful exit status.
	if err != nil || strings.Contains(text, "Error") || strings.Contains(text, "error") {
		return "", fmt.Errorf("networksetup %s: %v %s", args[0], err, text)
	}
	return text, nil
}

func fields(text string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok {
			result[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return result
}

// Select connected physical services, excluding utun/VPN interfaces. A network
// change while connected is deliberately handled by stopping and starting again.
func activeServices() ([]string, error) {
	output, err := networksetup("-listnetworkserviceorder")
	if err != nil {
		return nil, err
	}
	var services []string
	var current string
	for _, line := range strings.Split(output, "\n") {
		if match := serviceLine.FindStringSubmatch(line); match != nil {
			current = ""
			if match[1] != "*" {
				current = match[2]
			}
			continue
		}
		match := deviceLine.FindStringSubmatch(line)
		if current == "" || match == nil || !strings.HasPrefix(match[1], "en") {
			continue
		}
		iface, err := net.InterfaceByName(match[1])
		if err != nil || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			ip, _, _ := net.ParseCIDR(address.String())
			if ip != nil && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() {
				services = append(services, current)
				break
			}
		}
	}
	if len(services) == 0 {
		return nil, errors.New("未找到已连接的 Wi-Fi 或以太网服务")
	}
	return services, nil
}

func (p *systemProxy) enable(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	getServices := p.services
	if getServices == nil {
		getServices = activeServices
	}
	services, err := getServices()
	if err != nil {
		return err
	}
	var settings []proxySetting
	for _, service := range services {
		for _, kind := range []string{"autoproxyurl", "proxyautodiscovery"} {
			output, err := p.command("-get"+kind, service)
			if err != nil {
				return err
			}
			if fields(output)["Enabled"] == "Yes" || strings.HasSuffix(output, "On") {
				return errors.New("当前网络启用了 PAC 或自动代理发现，请先关闭后再启动")
			}
		}
		for _, kind := range []string{"webproxy", "securewebproxy", "socksfirewallproxy"} {
			output, err := p.command("-get"+kind, service)
			if err != nil {
				return err
			}
			values := fields(output)
			if values["Authenticated Proxy Enabled"] != "0" {
				return errors.New("当前网络配置了认证代理，此版本无法无损恢复，请先停用该配置")
			}
			if _, err := strconv.Atoi(values["Port"]); err != nil {
				return fmt.Errorf("无法读取 %s 的代理端口", service)
			}
			settings = append(settings, proxySetting{service, kind, values["Enabled"] == "Yes", values["Server"], values["Port"]})
		}
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	// Persist before the first mutation so the next launch can recover after a crash.
	if err := os.WriteFile(p.journal+".tmp", data, 0600); err != nil {
		return err
	}
	if err := os.Rename(p.journal+".tmp", p.journal); err != nil {
		return err
	}
	for _, setting := range settings {
		if _, err := p.command("-set"+setting.Kind, setting.Service, host, port, "off"); err != nil {
			return errors.Join(err, p.restore())
		}
		if _, err := p.command("-set"+setting.Kind+"state", setting.Service, "on"); err != nil {
			return errors.Join(err, p.restore())
		}
	}
	return nil
}

func (p *systemProxy) restore() error {
	data, err := os.ReadFile(p.journal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var settings []proxySetting
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	var result error
	for _, setting := range settings {
		// Restore host/port even if the old proxy was disabled.
		_, err := p.command("-set"+setting.Kind, setting.Service, setting.Server, setting.Port, "off")
		result = errors.Join(result, err)
		state := "off"
		if setting.Enabled {
			state = "on"
		}
		_, err = p.command("-set"+setting.Kind+"state", setting.Service, state)
		result = errors.Join(result, err)
	}
	if result == nil {
		return os.Remove(p.journal)
	}
	return result
}
