package profile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxSubscriptionSize = 8 << 20

// FetchClash never returns URLs or credentials in errors. Some providers use
// User-Agent negotiation, so request their Clash representation explicitly.
func FetchClash(ctx context.Context, rawURL string) ([]Node, error) {
	if err := ValidateSubscriptionURL(rawURL); err != nil {
		return nil, err
	}
	transport := &http.Transport{} // Do not inherit stale shell proxy variables.
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSpace(rawURL), nil)
	if err != nil {
		return nil, errors.New("订阅地址无效")
	}
	req.Header.Set("User-Agent", "clash.meta")
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("订阅请求已取消或超时")
		}
		return nil, errors.New("订阅请求失败，请检查网络和订阅地址")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("订阅服务器返回 HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSubscriptionSize+1))
	if err != nil {
		return nil, errors.New("读取订阅失败")
	}
	if len(data) > maxSubscriptionSize {
		return nil, errors.New("订阅文件超过 8 MB")
	}
	return ParseClash(data)
}

func ParseClash(data []byte) ([]Node, error) {
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if len(data) > maxSubscriptionSize {
		return nil, errors.New("订阅文件超过 8 MB")
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		// YAML decoder messages can quote password/token contents.
		return nil, errors.New("无法解析 Clash YAML，请确认订阅返回了正确格式")
	}
	if len(document.Proxies) == 0 {
		return nil, errors.New("Clash 订阅中没有 proxies 节点列表")
	}
	nodes := make([]Node, 0, len(document.Proxies))
	ids := map[string]bool{}
	for i, entry := range document.Proxies {
		node, err := clashNode(entry)
		if err != nil {
			return nil, fmt.Errorf("第 %d 个节点: %w", i+1, err)
		}
		if ids[node.ID] {
			return nil, fmt.Errorf("第 %d 个节点名称重复，请在订阅端修正", i+1)
		}
		ids[node.ID] = true
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func text(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func truth(value any) bool { return value == true || text(value) == "true" || text(value) == "1" }

func object(value any) map[string]any {
	if value, ok := value.(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func stringList(value any) []string {
	var result []string
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			result = append(result, text(item))
		}
	case []string:
		result = value
	case string:
		if value != "" {
			result = strings.Split(value, ",")
		}
	}
	return result
}

func clashNode(entry map[string]any) (Node, error) {
	name := strings.TrimSpace(text(entry["name"]))
	if name == "" {
		return Node{}, errors.New("缺少节点名称")
	}
	port, err := strconv.Atoi(text(entry["port"]))
	if err != nil || port < 1 || port > 65535 {
		return Node{}, errors.New("端口必须为 1–65535")
	}
	if text(entry["dialer-proxy"]) != "" {
		return Node{}, errors.New("暂不支持链式 dialer-proxy 节点")
	}
	typ := text(entry["type"])
	switch typ {
	case "ss":
		typ = "shadowsocks"
	case "socks5":
		typ = "socks"
	case "hy2":
		typ = "hysteria2"
	}
	out := map[string]any{"type": typ, "server": text(entry["server"]), "server_port": port}
	tlsEnabled := truth(entry["tls"]) || typ == "trojan" || typ == "hysteria2" || typ == "tuic" || typ == "anytls"
	switch typ {
	case "vless", "vmess":
		out["uuid"] = text(entry["uuid"])
		if typ == "vless" && text(entry["flow"]) != "" {
			out["flow"] = text(entry["flow"])
		}
		if typ == "vmess" {
			security := text(entry["cipher"])
			if security == "" {
				security = "auto"
			}
			out["security"] = security
			if alterID, err := strconv.Atoi(text(entry["alterId"])); err == nil && alterID != 0 {
				out["alter_id"] = alterID
			}
		}
	case "trojan", "hysteria2", "anytls":
		out["password"] = text(entry["password"])
	case "shadowsocks":
		if text(entry["plugin"]) != "" {
			return Node{}, errors.New("暂不支持带插件的 Shadowsocks 节点")
		}
		out["method"], out["password"] = text(entry["cipher"]), text(entry["password"])
	case "tuic":
		out["uuid"], out["password"] = text(entry["uuid"]), text(entry["password"])
		for key, target := range map[string]string{"congestion-controller": "congestion_control", "udp-relay-mode": "udp_relay_mode"} {
			if text(entry[key]) != "" {
				out[target] = text(entry[key])
			}
		}
	case "http", "socks":
		if text(entry["username"]) != "" {
			out["username"], out["password"] = text(entry["username"]), text(entry["password"])
		}
	default:
		return Node{}, errors.New("该 Clash 节点协议暂不支持")
	}
	if typ == "hysteria2" {
		ports := stringList(entry["ports"])
		if len(ports) == 0 {
			ports = stringList(entry["mport"])
		}
		if len(ports) != 0 {
			for i := range ports {
				ports[i] = strings.ReplaceAll(strings.TrimSpace(ports[i]), "-", ":")
			}
			out["server_ports"] = ports
			delete(out, "server_port")
		}
		if text(entry["obfs"]) != "" {
			out["obfs"] = map[string]any{"type": text(entry["obfs"]), "password": text(entry["obfs-password"])}
		}
	}
	if tlsEnabled {
		tls := map[string]any{"enabled": true}
		sni := text(entry["servername"])
		if sni == "" {
			sni = text(entry["sni"])
		}
		if sni != "" {
			tls["server_name"] = sni
		}
		if truth(entry["skip-cert-verify"]) {
			tls["insecure"] = true
		}
		if alpn := stringList(entry["alpn"]); len(alpn) > 0 {
			tls["alpn"] = alpn
		}
		fingerprint := text(entry["client-fingerprint"])
		reality := object(entry["reality-opts"])
		if text(reality["public-key"]) != "" {
			tls["reality"] = map[string]any{"enabled": true, "public_key": text(reality["public-key"]), "short_id": text(reality["short-id"])}
			if fingerprint == "" {
				fingerprint = "chrome"
			}
		}
		if fingerprint != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
		}
		out["tls"] = tls
	}
	if err := clashTransport(entry, out); err != nil {
		return Node{}, err
	}
	node := Node{ID: name, Name: name, Outbound: out}
	return node, validateNode(node)
}

func clashTransport(entry, out map[string]any) error {
	switch network := text(entry["network"]); network {
	case "", "tcp", "raw":
	case "ws":
		opts := object(entry["ws-opts"])
		transport := map[string]any{"type": "ws", "path": text(opts["path"])}
		if headers := object(opts["headers"]); len(headers) > 0 {
			transport["headers"] = headers
		}
		out["transport"] = transport
	case "grpc":
		out["transport"] = map[string]any{"type": "grpc", "service_name": text(object(entry["grpc-opts"])["grpc-service-name"])}
	case "httpupgrade":
		opts := object(entry["http-upgrade-opts"])
		out["transport"] = map[string]any{"type": "httpupgrade", "path": text(opts["path"]), "host": text(opts["host"])}
	default:
		return errors.New("该传输类型暂不支持")
	}
	return nil
}
