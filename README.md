# Loop

A macOS proxy client built with Go and Fyne, powered by sing-box.

English | [简体中文](README_CN.md)

[Download](https://github.com/voidvon/Loop/releases/latest) · [Getting Started](#getting-started) · [Documentation](#documentation) · [Release History](https://github.com/voidvon/Loop/releases)

## Overview

Loop brings proxy connections, nodes, subscriptions, policy groups, routing rules, and diagnostics into one desktop app. Use the system proxy or TUN mode, manage connections from the menu bar, and keep the proxy running after closing the window.

Bring your own proxy nodes or subscription. Release packages include the sing-box core and routing rule snapshots, with empty node and subscription lists.

## Features

- **Connection modes** — rule-based routing, global proxy, and direct mode, with system proxy or TUN support.
- **Nodes and subscriptions** — add VLESS links or import Clash YAML subscriptions; keep nodes separate from routing rules.
- **Policy groups** — select nodes, combine groups, filter by name, and measure node latency.
- **Routing and configuration** — manage local rules and remote rule sets, or edit JSON with highlighting, search, formatting, and validation.
- **Desktop controls** — light, dark, and system appearance; macOS translucent backgrounds; menu bar controls and launch at login.
- **Independent background service** — close the window while connections continue; control the service with the bundled CLI and JSON output.
- **Diagnostics** — inspect URL routing, test node latency, and run batch HTTP benchmarks.

## Downloads

Download the app from [GitHub Releases](https://github.com/voidvon/Loop/releases/latest).

| Platform | Architecture | Requirement | Package |
| --- | --- | --- | --- |
| macOS | Apple Silicon (`arm64`, M-series) | macOS 15 or later | `Loop-v<version>-macos-arm64.zip` |

The current release targets Apple Silicon only. Intel, universal, Windows, Linux, and Android packages are not available.

Releases include `SHA256SUMS.txt` and the corresponding sing-box source archive. Dependency licenses are included inside the app bundle.

> Current builds use ad-hoc signing and are not notarized by Apple. If macOS blocks the app, attempt to open it once, then allow it in **System Settings → Privacy & Security**.

## Getting Started

The app interface currently uses Simplified Chinese. The labels below match the app.

1. Download the ZIP, extract it, and move `Loop.app` to **Applications**.
2. Open Loop and go to **Nodes (节点)**. Click **+** to add a `vless://` link or a Clash YAML subscription URL.
3. Open **Policies (策略)** and select a node in `Available`, the initial default policy group.
4. On **Home (首页)**, choose **Rule-based (自动分流)**, **Global Proxy (全局代理)**, or **Direct (全局直连)**.
5. Enable the proxy connection. System proxy mode is the default. To use TUN, enable **TUN Mode (TUN 模式)**; the first use requests administrator authorization to install its helper service.
6. Optionally configure **More (更多)** for launch at login, menu bar preferences, proxy ports, and diagnostics.

Closing the window keeps the background service and current connection running. Use **⌘Q** or **Quit Loop (退出 Loop)** in the menu bar to stop the app and connection.

## Everyday Use

### Routing modes and ports

| Mode | Default proxy port and TUN traffic |
| --- | --- |
| Rule-based | Follow the saved routing rules and DNS configuration |
| Global Proxy | Use the default policy group; local domains and private IPs stay direct |
| Direct | Connect directly |

Loop provides two local HTTP/SOCKS5 endpoints. Change their ports under **More → Proxy Ports (更多 → 代理端口)**.

| Endpoint | Default address | Behavior |
| --- | --- | --- |
| Default proxy | `127.0.0.1:2080` | Follows the mode selected on Home; used by the system proxy |
| Global proxy | `127.0.0.1:2081` | Always uses the default policy group; can be disabled separately |

The global endpoint continues to use its policy group in Direct mode. Disable it if you want Direct mode to work without a proxy node.

### Nodes, policies, and rules

Manage nodes on **Nodes (节点)** and choose the active node on **Policies (策略)**. Selecting a node card on the Nodes page does not change the active connection. Groups use manual selection; automatic failover is not implemented.

The single-node add flow accepts VLESS links. Clash YAML subscriptions support common fields for VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS, SOCKS5, and HTTP. Plugin-based nodes, proxy chains, and Base64 URI subscription lists are not supported. Subscription fetches use a direct connection.

Use **Rules (规则)** to manage local rules, remote JSON/`.srs` rule sets, and the final fallback policy. Use **Config (配置)** for advanced JSON editing. Unsaved configuration drafts must be saved or discarded before making changes on other pages. Changes that affect a live connection can trigger a reconnect.

### Configuration and data

Installed apps store data in `~/Library/Application Support/loop/`:

| Path | Contents |
| --- | --- |
| `profiles/config.json` | Main configuration: ports, DNS, policies, and routing rules |
| `profiles/nodes.json` | Local nodes and credentials |
| `profiles/subscriptions.json` | Subscription URLs and node snapshots |
| `profiles/policy-selections.json` | Selected policy members |
| `profiles/settings.json` | Routing mode and TUN preference |
| `runtime/config.json` | Generated sing-box configuration |
| `sing-box.log` | Core log |

App updates preserve existing user configuration. Edit the user data copy or use the app; do not modify files inside the signed bundle. Node files, subscription files, and generated runtime configuration may contain credentials.

### Command line

The app includes `loopctl` for scripts and terminal use:

```sh
"/Applications/Loop.app/Contents/MacOS/loopctl" status --json
"/Applications/Loop.app/Contents/MacOS/loopctl" policy list --json
"/Applications/Loop.app/Contents/MacOS/loopctl" mode rule --json
"/Applications/Loop.app/Contents/MacOS/loopctl" disconnect --json
```

Use `direct`, `rule`, or `global` for the mode. See the [detailed guide (Chinese)](docs/user-guide.zh-CN.md#独立后台与命令行) for background service commands, policy selection, and isolated instances.

## Development

Requirements: macOS, Go 1.27.1 (as specified in `go.mod`), Xcode Command Line Tools, and sing-box 1.14.4 or later.

```sh
git clone https://github.com/voidvon/Loop.git
cd Loop
brew install sing-box

# First checkout only: create local files, then fill in your node details.
cp profiles/nodes.example.json profiles/nodes.json
cp profiles/subscriptions.example.json profiles/subscriptions.json
cp profiles/selection.example.json profiles/selection.json
make run
```

| Command | Purpose |
| --- | --- |
| `make prepare` | Reconstruct the pinned, patched Fyne dependency |
| `make run` | Build the app and launch the UI |
| `make build` | Build `loop`, `loopd`, `loopctl`, and `loop-tun` into `bin/` |
| `make test` | Run race tests, including the Fyne painter checks |
| `make test-headless` | Run the supported headless package tests |
| `make vet` | Run Go static analysis |
| `make package` | Create the macOS app and ZIP for the host architecture |

Make targets prepare Fyne automatically. Run `make prepare` before using Go commands or an IDE directly. The repository tracks the Fyne patch and pinned upstream metadata; `third_party/fyne/` is generated and ignored by Git.

### Packaging and versions

```sh
make package
open dist/Loop.app
bash scripts/version.sh current
bash scripts/version.sh next
```

Packaging bundles the core, shared configuration, rule snapshots, and dependency notices. It starts with empty nodes, subscriptions, and node selection, excluding local credentials and preferences. Set `LOOP_CORE` to use a specific sing-box executable and `LOOP_CORE_LICENSE` to supply its license file.

[`VERSION`](VERSION) is the version source for the app bundle; release tags use the `v` prefix. Patch and minor numbers range from 0 to 20:

```text
0.1.0 → … → 0.1.20 → 0.2.0
0.20.20 → 1.0.0
1.0.20 → 1.1.0
```

`version.sh next` prints the next version without changing files. Update `VERSION`, commit, and build before publishing the matching tag and release.

## Platform Notes

- Only the macOS proxy and TUN backends are implemented. Stop other VPN/TUN clients before using TUN.
- TUN installs a privileged helper on first use. Subsequent connections reuse it; a core or service update may require authorization again.
- Launch at login opens the app without automatically connecting the proxy.
- Switching physical networks while connected currently requires disconnecting and reconnecting.
- PAC, automatic proxy discovery, and authenticated system proxies are not managed. Force-quitting the background service or a system interruption may require restoring proxy settings; use Quit Loop for normal shutdown.

## Documentation

| Document | Contents |
| --- | --- |
| [Detailed guide (Chinese)](docs/user-guide.zh-CN.md) | Configuration editing, nodes, policies, rules, TUN, diagnostics, CLI, and migration |
| [Architecture (Chinese)](docs/architecture.md) | Process responsibilities, module boundaries, and lifecycle |
| [UI memory notes (Chinese)](docs/ui-memory.md) | Font caching, measurements, and implementation details |
| [Fyne patches (Chinese)](third_party/fyne-patches/README.md) | Pinned dependency and patch maintenance |
| [Release history](https://github.com/voidvon/Loop/releases) | Downloads, release notes, checksums, and core source archives |

Issues and pull requests are welcome. Include your macOS version, CPU architecture, reproduction steps, and relevant logs. Remove node credentials and subscription URLs before sharing logs or configuration.

## Acknowledgments

Built with [sing-box](https://github.com/SagerNet/sing-box), [Fyne](https://github.com/fyne-io/fyne), and [go-fyne-pretty-view](https://github.com/ideaconnect/go-fyne-pretty-view). Bundled China routing snapshots come from [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat).
