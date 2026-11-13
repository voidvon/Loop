package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"local/loop/internal/profile"
)

func fieldText(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func mapValue(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func cloneOutbound(value map[string]any) map[string]any {
	data, _ := json.Marshal(value)
	var result map[string]any
	json.Unmarshal(data, &result)
	if result == nil {
		result = map[string]any{}
	}
	return result
}

func (m *nodeManager) editManual(existing *profile.Node) {
	if m.locked {
		return
	}
	node := profile.Node{ID: profile.NewID(), Outbound: map[string]any{"type": "vless", "server_port": 443}}
	if existing != nil {
		node = *existing
		node.Outbound = cloneOutbound(existing.Outbound)
	}
	name, server, port := widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	name.SetText(node.Name)
	server.SetText(fieldText(node.Outbound["server"]))
	port.SetText(fieldText(node.Outbound["server_port"]))
	if port.Text == "" {
		port.SetText("443")
	}
	protocol := widget.NewSelect([]string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic", "anytls", "socks", "http"}, nil)
	protocol.SetSelected(fieldText(node.Outbound["type"]))
	uuid, password, username := widget.NewPasswordEntry(), widget.NewPasswordEntry(), widget.NewEntry()
	uuid.SetText(fieldText(node.Outbound["uuid"]))
	password.SetText(fieldText(node.Outbound["password"]))
	username.SetText(fieldText(node.Outbound["username"]))
	method := widget.NewSelect([]string{"aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305"}, nil)
	method.SetSelected(fieldText(node.Outbound["method"]))
	if method.Selected == "" {
		method.SetSelected("chacha20-ietf-poly1305")
	}
	flow := widget.NewSelect([]string{"无", "xtls-rprx-vision"}, nil)
	flow.SetSelected("无")
	if fieldText(node.Outbound["flow"]) != "" {
		flow.SetSelected(fieldText(node.Outbound["flow"]))
	}
	tlsData := mapValue(node.Outbound["tls"])
	tlsEnabled := widget.NewCheck("启用 TLS", nil)
	tlsEnabled.SetChecked(tlsData["enabled"] == true || existing == nil)
	sni, publicKey, shortID := widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	sni.SetPlaceHolder("证书域名 / SNI")
	sni.SetText(fieldText(tlsData["server_name"]))
	reality := mapValue(tlsData["reality"])
	publicKey.SetText(fieldText(reality["public_key"]))
	shortID.SetText(fieldText(reality["short_id"]))
	fingerprint := widget.NewSelect([]string{"默认", "chrome", "firefox", "safari"}, nil)
	fingerprint.SetSelected("默认")
	if fp := fieldText(mapValue(tlsData["utls"])["fingerprint"]); fp != "" {
		fingerprint.SetSelected(fp)
	}
	transport := mapValue(node.Outbound["transport"])
	network := widget.NewSelect([]string{"tcp", "ws", "grpc", "httpupgrade"}, nil)
	network.SetSelected("tcp")
	if typ := fieldText(transport["type"]); typ != "" {
		network.SetSelected(typ)
	}
	path, host, service := widget.NewEntry(), widget.NewEntry(), widget.NewEntry()
	path.SetText(fieldText(transport["path"]))
	host.SetText(fieldText(mapValue(transport["headers"])["Host"]))
	if fieldText(transport["host"]) != "" {
		host.SetText(fieldText(transport["host"]))
	}
	service.SetText(fieldText(transport["service_name"]))
	fields := container.NewVBox()
	var rebuild func()
	rebuild = func() {
		items := []*widget.FormItem{
			widget.NewFormItem("名称", name), widget.NewFormItem("节点类型", protocol),
			widget.NewFormItem("服务器", server), widget.NewFormItem("端口", port),
		}
		typ := protocol.Selected
		if typ == "vless" || typ == "vmess" || typ == "tuic" {
			items = append(items, widget.NewFormItem("UUID", uuid))
		}
		if typ == "socks" || typ == "http" {
			items = append(items, widget.NewFormItem("用户名", username))
		}
		if typ != "vless" && typ != "vmess" {
			items = append(items, widget.NewFormItem("密码", password))
		}
		if typ == "shadowsocks" {
			items = append(items, widget.NewFormItem("加密方式", method))
		}
		if typ == "vless" {
			items = append(items, widget.NewFormItem("Flow", flow))
		}
		if typ != "shadowsocks" && typ != "socks" {
			items = append(items, widget.NewFormItem("安全连接", tlsEnabled))
			if tlsEnabled.Checked {
				items = append(items, widget.NewFormItem("SNI", sni))
				if typ == "vless" || typ == "vmess" || typ == "trojan" {
					items = append(items, widget.NewFormItem("TLS 指纹", fingerprint))
				}
				if typ == "vless" {
					items = append(items, widget.NewFormItem("Reality 公钥", publicKey), widget.NewFormItem("Short ID", shortID))
				}
			}
		}
		if typ == "vless" || typ == "vmess" || typ == "trojan" {
			items = append(items, widget.NewFormItem("传输方式", network))
			if network.Selected == "ws" || network.Selected == "httpupgrade" {
				items = append(items, widget.NewFormItem("路径", path), widget.NewFormItem("Host", host))
			}
			if network.Selected == "grpc" {
				items = append(items, widget.NewFormItem("Service", service))
			}
		}
		fields.Objects = []fyne.CanvasObject{widget.NewForm(items...)}
		fields.Refresh()
	}
	protocol.OnChanged = func(typ string) {
		if typ == "trojan" || typ == "hysteria2" || typ == "tuic" || typ == "anytls" {
			tlsEnabled.SetChecked(true)
		}
		rebuild()
	}
	tlsEnabled.OnChanged = func(bool) { rebuild() }
	network.OnChanged = func(string) { rebuild() }
	rebuild()

	link := widget.NewPasswordEntry()
	link.SetPlaceHolder("粘贴 vless://、trojan://、ss://、hysteria2:// 链接")
	linkName := widget.NewEntry()
	linkName.SetPlaceHolder("可选，留空使用链接名称")
	if existing != nil {
		linkName.SetText(node.Name)
	}
	linkPage := container.NewVBox(widget.NewLabel("粘贴单个节点的分享链接"), widget.NewForm(widget.NewFormItem("分享链接", link), widget.NewFormItem("名称", linkName)))
	advancedName := widget.NewEntry()
	advancedName.SetText(node.Name)
	advanced := widget.NewMultiLineEntry()
	advanced.TextStyle.Monospace = true
	advanced.SetMinRowsVisible(10)
	data, _ := json.MarshalIndent(node.Outbound, "", "  ")
	advanced.SetText(string(data))
	advancedPage := container.NewBorder(widget.NewForm(widget.NewFormItem("名称", advancedName)), nil, nil, nil, advanced)
	tabs := container.NewAppTabs(container.NewTabItem("分享链接", linkPage), container.NewTabItem("手动填写", container.NewVScroll(fields)), container.NewTabItem("高级 JSON", advancedPage))
	if existing != nil {
		tabs.SelectIndex(1)
	}
	validation := widget.NewLabel("")
	validation.Wrapping = fyne.TextWrapWord
	var editor *dialog.CustomDialog
	save := widget.NewButton("检查并保存", func() {
		if m.locked {
			return
		}
		candidate := profile.Node{ID: node.ID}
		switch tabs.SelectedIndex() {
		case 0:
			parsed, err := profile.ParseNodeLink(link.Text)
			if err != nil {
				validation.SetText(err.Error())
				return
			}
			candidate = parsed
			candidate.ID = node.ID
			if strings.TrimSpace(linkName.Text) != "" {
				candidate.Name = strings.TrimSpace(linkName.Text)
			}
		case 1:
			candidate.Name = strings.TrimSpace(name.Text)
			out := cloneOutbound(node.Outbound)
			if fieldText(out["type"]) != protocol.Selected {
				out = map[string]any{}
			}
			portNumber, err := strconv.Atoi(strings.TrimSpace(port.Text))
			if err != nil || portNumber < 1 || portNumber > 65535 {
				validation.SetText("端口必须为 1–65535")
				return
			}
			typ := protocol.Selected
			out["type"], out["server"], out["server_port"] = typ, strings.TrimSpace(server.Text), portNumber
			delete(out, "server_ports")
			if typ == "vless" || typ == "vmess" || typ == "tuic" {
				out["uuid"] = strings.TrimSpace(uuid.Text)
			}
			if typ != "vless" && typ != "vmess" {
				out["password"] = password.Text
			}
			if typ == "socks" || typ == "http" {
				out["username"] = username.Text
			}
			if typ == "shadowsocks" {
				out["method"] = method.Selected
			}
			if typ == "vmess" && out["security"] == nil {
				out["security"] = "auto"
			}
			if typ == "vless" {
				delete(out, "flow")
				if flow.Selected != "无" {
					out["flow"] = flow.Selected
				}
			}
			if typ != "shadowsocks" && typ != "socks" && tlsEnabled.Checked {
				tls := cloneOutbound(mapValue(out["tls"]))
				tls["enabled"] = true
				tls["server_name"] = strings.TrimSpace(sni.Text)
				delete(tls, "utls")
				if (typ == "vless" || typ == "vmess" || typ == "trojan") && fingerprint.Selected != "默认" {
					tls["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint.Selected}
				}
				if typ == "vless" {
					delete(tls, "reality")
					if strings.TrimSpace(publicKey.Text) != "" {
						tls["reality"] = map[string]any{"enabled": true, "public_key": strings.TrimSpace(publicKey.Text), "short_id": strings.TrimSpace(shortID.Text)}
						if tls["utls"] == nil {
							tls["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
						}
					}
				}
				out["tls"] = tls
			} else {
				delete(out, "tls")
			}
			if typ == "vless" || typ == "vmess" || typ == "trojan" {
				delete(out, "transport")
				switch network.Selected {
				case "ws":
					out["transport"] = map[string]any{"type": "ws", "path": path.Text, "headers": map[string]any{"Host": host.Text}}
				case "grpc":
					out["transport"] = map[string]any{"type": "grpc", "service_name": service.Text}
				case "httpupgrade":
					out["transport"] = map[string]any{"type": "httpupgrade", "path": path.Text, "host": host.Text}
				}
			}
			candidate.Outbound = out
		case 2:
			candidate.Name = strings.TrimSpace(advancedName.Text)
			if err := json.Unmarshal([]byte(advanced.Text), &candidate.Outbound); err != nil {
				validation.SetText("节点 JSON 格式无效")
				return
			}
		}
		if candidate.Name == "" {
			validation.SetText("请填写节点名称")
			return
		}
		if err := profile.ValidateNode(candidate); err != nil {
			validation.SetText(err.Error())
			return
		}
		m.background("正在检查节点配置…", func(ctx context.Context) error {
			return m.service.SaveManual(ctx, candidate)
		}, func() {
			editor.Hide()
			m.sourceIndex = 0
			m.refresh()
			m.selectSource(0)
			m.message.SetText("节点已保存。")
		})
	})
	save.Importance = widget.HighImportance
	title := "添加节点"
	if existing != nil {
		title = "编辑节点"
	}
	editor = dialog.NewCustomWithoutButtons(title, container.NewBorder(nil, container.NewVBox(validation, container.NewHBox(widget.NewButton("取消", func() { editor.Hide() }), save)), nil, nil, tabs), m.window)
	editor.Resize(fyne.NewSize(600, 530))
	editor.Show()
}
