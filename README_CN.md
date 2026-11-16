# Loop

基于 Go 与 Fyne 构建的 macOS 代理客户端，使用 sing-box 内核。

[English](README.md) | 简体中文

[下载](https://github.com/voidvon/Loop/releases/latest) · [快速开始](#快速开始) · [文档](#文档) · [版本记录](https://github.com/voidvon/Loop/releases)

## 简介

Loop 将代理连接、节点、订阅、策略组、分流规则与诊断工具集中在一个桌面应用中。支持系统代理和 TUN 模式，可通过菜单栏管理连接，关闭窗口后仍可保持代理运行。

请自行提供代理节点或订阅。发布包已包含 sing-box 内核与规则库快照，节点和订阅列表初始为空。

## 功能

- **连接模式**：自动分流、全局代理、全局直连，支持系统代理与 TUN。
- **节点与订阅**：添加 VLESS 分享链接或导入 Clash YAML 订阅，节点与分流规则独立管理。
- **策略组**：手动选择节点、组合策略组、按名称筛选节点，并测试节点延迟。
- **规则与配置**：管理本地规则和远程规则集，支持 JSON 高亮、搜索、格式化与校验。
- **桌面体验**：浅色、深色及跟随系统外观，macOS 毛玻璃背景、菜单栏控制与登录自启。
- **独立后台**：关闭窗口后保持连接，内置命令行工具并支持 JSON 输出。
- **诊断工具**：网址分流测试、节点延迟测试与批量 HTTP 测速。

## 下载

前往 [GitHub Releases](https://github.com/voidvon/Loop/releases/latest) 下载。

| 平台 | 架构 | 系统要求 | 安装包 |
| --- | --- | --- | --- |
| macOS | Apple Silicon（`arm64`，M 系列芯片） | macOS 15 或更高版本 | `Loop-v<版本>-macos-arm64.zip` |

当前发布包仅适用于 Apple Silicon，暂未提供 Intel、universal、Windows、Linux 或 Android 安装包。

Release 附带 `SHA256SUMS.txt` 与对应版本的 sing-box 源码包，依赖许可保存在应用包内。

> 当前构建使用 ad-hoc 签名，尚未经 Apple 公证。若系统阻止启动，请先尝试打开一次，再到「系统设置 → 隐私与安全性」允许打开。

## 快速开始

应用界面目前为简体中文，README 提供中英文版本。

1. 下载并解压 ZIP，将 `Loop.app` 拖入「应用程序」。
2. 打开 Loop，进入「节点」，点击右上角 **+**，添加 `vless://` 分享链接或 Clash YAML 订阅地址。
3. 进入「策略」，在初始默认策略组 `Available` 中选择要使用的节点。
4. 返回「首页」，选择「自动分流」「全局代理」或「全局直连」。
5. 开启代理连接。默认使用系统代理；如需 TUN，请开启「TUN 模式」，首次使用会请求管理员授权以安装辅助服务。
6. 按需在「更多」中设置登录自启、菜单栏、代理端口与诊断工具。

关闭窗口后，后台与当前连接继续运行。按 **⌘Q** 或在菜单栏选择「退出 Loop」，可停止应用和代理连接。

## 日常使用

### 分流模式与代理端口

| 模式 | 默认代理入口与 TUN 流量 |
| --- | --- |
| 自动分流 | 使用已保存的分流规则与 DNS 配置 |
| 全局代理 | 使用默认策略组；局域网域名与私有 IP 直连 |
| 全局直连 | 直接连接 |

Loop 提供两个本地 HTTP/SOCKS5 入口，可在「更多 → 代理端口」修改。

| 入口 | 默认地址 | 行为 |
| --- | --- | --- |
| 默认代理 | `127.0.0.1:2080` | 跟随首页模式，系统代理使用此入口 |
| 全局代理 | `127.0.0.1:2081` | 始终使用默认策略组，可单独关闭 |

全局代理入口在「全局直连」模式下仍使用策略组。若希望直连模式不依赖代理节点，请关闭该入口。

### 节点、策略与规则

在「节点」页管理节点，在「策略」页选择当前使用的节点。点击节点页中的卡片不会切换连接节点。策略组采用手动选择，暂不支持自动故障转移。

新增单节点支持 VLESS 分享链接。Clash YAML 订阅支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、AnyTLS、SOCKS5、HTTP 的常用字段；暂不支持插件节点、代理链和 Base64 URI 订阅列表。订阅通过直连获取。

在「规则」页管理本地规则、远程 JSON/`.srs` 规则集和 Final 兜底策略；高级配置可在「配置」页直接编辑 JSON。配置草稿未保存时，需要先保存或放弃草稿，再到其他页面修改配置。影响当前连接的改动可能触发重连。

### 配置与数据

安装版的数据保存在 `~/Library/Application Support/loop/`：

| 路径 | 内容 |
| --- | --- |
| `profiles/config.json` | 主配置：端口、DNS、策略组与分流规则 |
| `profiles/nodes.json` | 本地节点与凭据 |
| `profiles/subscriptions.json` | 订阅地址与节点快照 |
| `profiles/policy-selections.json` | 策略组选中项 |
| `profiles/settings.json` | 分流模式与 TUN 偏好 |
| `runtime/config.json` | 自动生成的 sing-box 运行配置 |
| `sing-box.log` | 内核日志 |

升级应用会保留已有用户配置。请在应用中或用户数据目录修改配置，不要直接修改已签名的应用包。节点、订阅和运行配置可能包含凭据。

### 命令行

应用内置 `loopctl`，可用于终端与脚本：

```sh
"/Applications/Loop.app/Contents/MacOS/loopctl" status --json
"/Applications/Loop.app/Contents/MacOS/loopctl" policy list --json
"/Applications/Loop.app/Contents/MacOS/loopctl" mode rule --json
"/Applications/Loop.app/Contents/MacOS/loopctl" disconnect --json
```

模式参数为 `direct`、`rule`、`global`。后台管理、策略切换与独立实例的完整命令见[详细指南](docs/user-guide.zh-CN.md#独立后台与命令行)。

## 开发

需要 macOS、Go 1.27.1（与 `go.mod` 一致）、Xcode Command Line Tools，以及 sing-box 1.14.4 或更高版本。

```sh
git clone https://github.com/voidvon/Loop.git
cd Loop
brew install sing-box

# 仅首次 checkout：创建本地文件，再填写自己的节点信息。
cp profiles/nodes.example.json profiles/nodes.json
cp profiles/subscriptions.example.json profiles/subscriptions.json
cp profiles/selection.example.json profiles/selection.json
make run
```

| 命令 | 用途 |
| --- | --- |
| `make prepare` | 重建固定版本并应用补丁的 Fyne 依赖 |
| `make run` | 构建并启动桌面应用 |
| `make build` | 将 `loop`、`loopd`、`loopctl`、`loop-tun` 构建到 `bin/` |
| `make test` | 运行竞态测试，包含 Fyne painter 检查 |
| `make test-headless` | 运行支持无界面环境的包测试 |
| `make vet` | 运行 Go 静态检查 |
| `make package` | 为本机架构生成 macOS 应用与 ZIP |

Make 入口会自动准备 Fyne。直接使用 Go 命令或 IDE 前，请先执行 `make prepare`。仓库只保存 Fyne 补丁和固定上游信息，`third_party/fyne/` 为自动生成目录，不纳入 Git。

### 打包与版本

```sh
make package
open dist/Loop.app
bash scripts/version.sh current
bash scripts/version.sh next
```

打包包含内核、公共主配置、规则库快照与依赖许可。节点、订阅与节点选择初始为空，不带入本地凭据和偏好。可通过 `LOOP_CORE` 指定 sing-box 可执行文件，通过 `LOOP_CORE_LICENSE` 指定内核许可文件。

[`VERSION`](VERSION) 是应用包的统一版本来源，Release 标签添加 `v` 前缀。修订号与次版本均在 0–20 之间：

```text
0.1.0 → … → 0.1.20 → 0.2.0
0.20.20 → 1.0.0
1.0.20 → 1.1.0
```

`version.sh next` 只输出下一版本，不会修改文件。发布时先更新 `VERSION`、提交并构建，再发布对应标签和 Release。

## 平台说明

- 目前仅实现 macOS 的系统代理与 TUN 后端。使用 TUN 前，请停止其他 VPN/TUN 客户端。
- TUN 首次使用会安装提权辅助服务，后续连接复用该服务；内核或服务更新时可能需要再次授权。
- 登录自启只打开应用，不会自动连接代理。
- 连接期间切换物理网络后，目前需要断开并重新连接。
- 不接管 PAC、自动代理发现与认证系统代理。强制结束后台或系统异常中断可能需要恢复代理设置；日常退出请使用「退出 Loop」。

## 文档

| 文档 | 内容 |
| --- | --- |
| [详细使用与维护指南](docs/user-guide.zh-CN.md) | 配置编辑、节点、策略、规则、TUN、诊断、命令行与旧版迁移 |
| [架构说明](docs/architecture.md) | 进程职责、模块边界与生命周期 |
| [UI 内存说明](docs/ui-memory.md) | 字体缓存、测量记录与实现细节 |
| [Fyne 补丁说明](third_party/fyne-patches/README.md) | 固定依赖与补丁维护方式 |
| [版本记录](https://github.com/voidvon/Loop/releases) | 安装包、发布说明、校验值与内核源码 |

欢迎提交 Issue 与 Pull Request。报告问题时请提供 macOS 版本、CPU 架构、复现步骤及相关日志，分享前移除节点凭据与订阅地址。

## 致谢

项目使用 [sing-box](https://github.com/SagerNet/sing-box)、[Fyne](https://github.com/fyne-io/fyne) 与 [go-fyne-pretty-view](https://github.com/ideaconnect/go-fyne-pretty-view)。内置国内规则快照来自 [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat)。
