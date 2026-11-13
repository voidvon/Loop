package profile

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

func decodeBase64(value string) (string, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if data, err := encoding.DecodeString(value); err == nil {
			return string(data), nil
		}
	}
	return "", errors.New("分享链接中的 Base64 内容无效")
}

// ParseNodeLink accepts common individual links. Errors never echo credentials.
func ParseNodeLink(raw string) (Node, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "ss://") {
		return parseShadowsocks(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return Node{}, errors.New("节点分享链接格式无效")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return Node{}, errors.New("分享链接缺少有效端口")
	}
	q := u.Query()
	out := map[string]any{"type": u.Scheme, "server": u.Hostname(), "server_port": port}
	tlsEnabled := q.Get("security") == "tls" || q.Get("security") == "reality"
	switch u.Scheme {
	case "vless":
		if encryption := q.Get("encryption"); encryption != "" && encryption != "none" {
			return Node{}, errors.New("暂不支持该 VLESS 加密方式")
		}
		out["uuid"] = u.User.Username()
		if q.Get("flow") != "" {
			out["flow"] = q.Get("flow")
		}
	case "trojan", "hysteria2", "hy2", "anytls":
		if u.Scheme == "hy2" {
			out["type"] = "hysteria2"
		}
		password := u.User.Username()
		if pass, ok := u.User.Password(); ok {
			password += ":" + pass
		}
		out["password"] = password
		tlsEnabled = true
		if q.Get("obfs") != "" {
			out["obfs"] = map[string]any{"type": q.Get("obfs"), "password": q.Get("obfs-password")}
		}
	default:
		return Node{}, errors.New("分享链接支持 VLESS、Trojan、Shadowsocks、Hysteria2 和 AnyTLS；其他协议可手动添加")
	}
	if tlsEnabled {
		sni := q.Get("sni")
		if sni == "" {
			sni = q.Get("peer")
		}
		tls := map[string]any{"enabled": true}
		if sni != "" {
			tls["server_name"] = sni
		}
		if q.Get("insecure") == "1" || q.Get("allowInsecure") == "1" {
			tls["insecure"] = true
		}
		if q.Get("alpn") != "" {
			tls["alpn"] = strings.Split(q.Get("alpn"), ",")
		}
		fingerprint := q.Get("fp")
		if q.Get("security") == "reality" {
			if q.Get("pbk") == "" {
				return Node{}, errors.New("Reality 链接缺少公钥")
			}
			tls["reality"] = map[string]any{"enabled": true, "public_key": q.Get("pbk"), "short_id": q.Get("sid")}
			if fingerprint == "" {
				fingerprint = "chrome"
			}
		}
		if fingerprint != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
		}
		out["tls"] = tls
	}
	entry := map[string]any{"network": q.Get("type")}
	entry["ws-opts"] = map[string]any{"path": q.Get("path"), "headers": map[string]any{"Host": q.Get("host")}}
	entry["grpc-opts"] = map[string]any{"grpc-service-name": q.Get("serviceName")}
	entry["http-upgrade-opts"] = map[string]any{"path": q.Get("path"), "host": q.Get("host")}
	if err := clashTransport(entry, out); err != nil {
		return Node{}, err
	}
	name := u.Fragment
	if name == "" {
		name = u.Hostname()
	}
	node := Node{ID: NewID(), Name: name, Outbound: out}
	return node, validateNode(node)
}

func parseShadowsocks(raw string) (Node, error) {
	// SIP002 uses base64(method:password)@host:port; older links encode all.
	if !strings.Contains(strings.SplitN(strings.TrimPrefix(raw, "ss://"), "#", 2)[0], "@") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "ss://"), "#", 2)
		decoded, err := decodeBase64(parts[0])
		if err != nil {
			return Node{}, err
		}
		userinfo, host, ok := strings.Cut(decoded, "@")
		if !ok {
			return Node{}, errors.New("Shadowsocks 分享链接无效")
		}
		raw = "ss://" + base64.RawURLEncoding.EncodeToString([]byte(userinfo)) + "@" + host
		if len(parts) == 2 {
			raw += "#" + parts[1]
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil || u.Hostname() == "" {
		return Node{}, errors.New("Shadowsocks 分享链接无效")
	}
	if u.Query().Get("plugin") != "" {
		return Node{}, errors.New("暂不支持带插件的 Shadowsocks 链接")
	}
	method, password := u.User.Username(), ""
	if pass, ok := u.User.Password(); ok {
		password = pass
	} else {
		decoded, err := decodeBase64(method)
		if err != nil {
			return Node{}, err
		}
		var ok bool
		method, password, ok = strings.Cut(decoded, ":")
		if !ok {
			return Node{}, errors.New("Shadowsocks 链接缺少加密方式或密码")
		}
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return Node{}, errors.New("Shadowsocks 链接端口无效")
	}
	name := u.Fragment
	if name == "" {
		name = u.Hostname()
	}
	node := Node{ID: NewID(), Name: name, Outbound: map[string]any{"type": "shadowsocks", "server": u.Hostname(), "server_port": port, "method": method, "password": password}}
	return node, validateNode(node)
}
