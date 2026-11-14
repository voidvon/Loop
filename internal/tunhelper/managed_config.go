package tunhelper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const resourcePrefix = "loop-resource:"
const maxResourceSize = 16 << 20
const maxResourcesSize = 64 << 20
const maxRequestSize = 128 << 20

// BundleConfig reads snapshots as the ordinary user. The privileged service
// receives bytes, never permission to read arbitrary user-supplied paths.
func BundleConfig(config []byte) (Request, error) {
	var value map[string]any
	if err := json.Unmarshal(config, &value); err != nil {
		return Request{}, err
	}
	req := Request{}
	total := 0
	route, _ := value["route"].(map[string]any)
	sets, _ := route["rule_set"].([]any)
	for _, item := range sets {
		set, _ := item.(map[string]any)
		for _, key := range []string{"path", "initial_path"} {
			path, _ := set[key].(string)
			if path == "" {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				return req, fmt.Errorf("读取规则快照：%w", err)
			}
			info, err := f.Stat()
			if err != nil || !info.Mode().IsRegular() {
				f.Close()
				return req, errors.New("规则快照必须是普通文件")
			}
			data, err := io.ReadAll(io.LimitReader(f, maxResourceSize+1))
			f.Close()
			if err != nil {
				return req, err
			}
			total += len(data)
			if len(data) > maxResourceSize || total > maxResourcesSize || len(req.Resources) >= 128 {
				return req, errors.New("规则快照过大（单文件上限 16 MiB，总计 64 MiB）")
			}
			set[key] = resourcePrefix + strconv.Itoa(len(req.Resources))
			req.Resources = append(req.Resources, data)
		}
	}
	var err error
	req.Config, err = json.Marshal(value)
	return req, err
}

func prepareManagedConfig(req Request, state string) (map[string]any, error) {
	if len(req.Config) > 16<<20 || len(req.Resources) > 128 {
		return nil, errors.New("TUN 配置或快照数量超过上限")
	}
	var config map[string]any
	if err := json.Unmarshal(req.Config, &config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, errors.New("配置为空")
	}
	delete(config, "experimental")
	config["log"] = map[string]any{"level": "info", "timestamp": true}
	for key := range config {
		switch key {
		case "log", "inbounds", "outbounds", "dns", "route", "http_clients":
		default:
			return nil, fmt.Errorf("TUN 服务不支持配置字段 %s", key)
		}
	}
	inbounds, _ := config["inbounds"].([]any)
	if len(inbounds) != 2 {
		return nil, errors.New("TUN 服务仅允许本地 mixed 和 TUN 两个入站")
	}
	mixed, _ := inbounds[0].(map[string]any)
	tun, _ := inbounds[1].(map[string]any)
	port, _ := mixed["listen_port"].(float64)
	if mixed["type"] != "mixed" || mixed["listen"] != "127.0.0.1" || port < 1024 || port > 65535 || port != float64(int(port)) || mixed["set_system_proxy"] == true || tun["type"] != "tun" {
		return nil, errors.New("TUN 服务需要监听 127.0.0.1:1024–65535 的 mixed 入站和一个 TUN 入站")
	}
	if _, exists := tun["file_descriptor"]; exists {
		return nil, errors.New("TUN 服务不接受外部文件描述符")
	}
	outbounds, _ := config["outbounds"].([]any)
	for _, item := range outbounds {
		out, _ := item.(map[string]any)
		switch out["type"] {
		case "direct", "block", "selector", "urltest", "vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic", "anytls", "socks", "http":
		default:
			return nil, fmt.Errorf("TUN 服务不支持出站类型 %v", out["type"])
		}
	}
	dns, _ := config["dns"].(map[string]any)
	servers, _ := dns["servers"].([]any)
	for _, item := range servers {
		server, _ := item.(map[string]any)
		switch server["type"] {
		case "local", "udp", "tcp", "tls", "https", "h3", "quic", "dhcp", "hosts", "fakeip":
		default:
			return nil, fmt.Errorf("TUN 服务不支持 DNS 类型 %v", server["type"])
		}
	}
	resourcePaths := map[string]bool{}
	total := 0
	for _, data := range req.Resources {
		total += len(data)
		if len(data) > maxResourceSize || total > maxResourcesSize {
			return nil, errors.New("规则快照超过上限")
		}
	}
	route, _ := config["route"].(map[string]any)
	sets, _ := route["rule_set"].([]any)
	for _, item := range sets {
		set, _ := item.(map[string]any)
		if address, ok := set["url"].(string); ok {
			u, err := url.Parse(address)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
				return nil, errors.New("规则订阅仅允许 HTTP / HTTPS 地址")
			}
		}
		for _, key := range []string{"path", "initial_path"} {
			path, ok := set[key].(string)
			if !ok || path == "" {
				continue
			}
			if !strings.HasPrefix(path, resourcePrefix) {
				return nil, errors.New("规则文件必须由普通用户读取后传给 TUN 服务")
			}
			index, err := strconv.Atoi(strings.TrimPrefix(path, resourcePrefix))
			if err != nil || index < 0 || index >= len(req.Resources) {
				return nil, errors.New("无效的规则快照引用")
			}
			name := filepath.Join(state, fmt.Sprintf("rules-%d.srs", index))
			if set["format"] == "source" {
				name = filepath.Join(state, fmt.Sprintf("rules-%d.json", index))
			}
			if err := os.WriteFile(name, req.Resources[index], 0600); err != nil {
				return nil, err
			}
			resourcePaths[name] = true
			set[key] = name
		}
	}
	// Permit URL/transport paths and process-match paths, but never a file path
	// that could make the root core read keys/certificates or execute plugins.
	var check func(any, string) error
	check = func(value any, context string) error {
		switch value := value.(type) {
		case map[string]any:
			for key, item := range value {
				switch key {
				case "plugin", "plugin_opts", "plugin_options", "command", "script", "exec", "acme":
					return fmt.Errorf("TUN 服务不允许字段 %s.%s", context, key)
				}
				filePath := strings.HasSuffix(key, "_path") || key == "path" || key == "paths" || key == "directory"
				if key == "process_path" {
					filePath = false
				}
				if key == "path" && (strings.HasSuffix(context, ".transport") || value["type"] == "https" || value["type"] == "h3") {
					filePath = false
				}
				if filePath {
					path, _ := item.(string)
					if path != "" && resourcePaths[path] {
						continue
					}
					return fmt.Errorf("TUN 服务不允许读取外部文件字段 %s.%s；请使用内联内容", context, key)
				}
				if err := check(item, context+"."+key); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range value {
				if err := check(item, context); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := check(config, "config"); err != nil {
		return nil, err
	}
	return config, nil
}
