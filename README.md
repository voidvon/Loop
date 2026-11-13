# Loop

Fyne / Go macOS 代理客户端，界面采用左侧导航、右侧内容布局，包含首页、节点、策略、规则、配置、更多六个入口。macOS 窗口按钮仅占左侧顶部，右侧标题与操作栏从窗口顶端开始，各页面采用紧凑的单行标题。首页控制代理开关；节点页管理节点和订阅；策略页统一选择节点与策略组；规则页管理 Final、本地规则和订阅规则；配置页直接编辑统一主配置；更多页采用左侧设置分类、右侧配置区域，提供开机自启和菜单栏设置。启动先校验配置和请求连通性，再设置系统代理或启动 TUN；关闭窗口退出 UI 并保留后台连接；`⌘Q` 或菜单栏“退出 Loop”停止后台并恢复代理设置。

界面支持跟随系统、浅色和深色三种外观，可在侧边栏底部选择，重启后保留偏好。卡片、列表、弹窗、控件和配置编辑器同步配色。首页顶部切换分流模式，卡片中管理连接、默认策略和 TUN；界面优先使用 Fyne 标准 Button、RadioGroup、Check、Card、List 和 PopUpMenu，通过统一主题设置配色和系统字体；控件尺寸、圆角和阴影沿用框架标准，无 WebView。

macOS 界面从本机加载苹方简体（PingFang SC）常规与半粗字重，字体不打入应用包；配置编辑器继续使用等宽字体。

macOS 菜单栏图标通过 G / R / D 表示全局代理、自动分流、全局直连，未连接时透明度为 50%，连接后为 100%，图标随系统菜单栏配色。原生菜单可开关代理、展开所有策略组并切换节点、显示主窗口或退出。菜单栏由独立后台持有，UI 退出后仍可切换节点并恢复连接。更多 → 菜单栏提供模式字母、断开时半透明、策略列表三个显示开关，修改立即生效并在重启后保留；界面偏好独立于主配置，修改不会重启代理。

更多 → 开机自启提供「登录时启动 Loop」开关，通过 macOS [SMAppService.mainAppService](https://developer.apple.com/documentation/servicemanagement/smappservice/mainapp?language=objc) 注册主应用登录项；进入 macOS 用户会话后打开应用，代理连接仍由首页控制。请在打包后的 Loop.app 中设置，建议先放入「应用程序」。状态以系统登录项为准，每次打开该页面以及从系统设置返回时重新读取；等待系统允许时明确显示「尚未生效」并提供登录项设置入口，启用失败会恢复实际状态，等待允许的注册也可关闭。不写入节点、规则或主配置，关闭自启不会退出当前应用。

macOS 窗口从创建时就不带系统标题栏，避免启动时先显示再隐藏标题栏的闪现；随后恢复标准窗口的缩放、调整大小与最小化能力。左上角由 Fyne 绘制红、黄、绿三个控制按钮：关闭 UI 并保留后台连接、最小化到 Dock、放大／还原窗口。顶部空白区域可以拖动，双击可以放大／还原；窗口边缘仍可调整大小。关闭按钮沿用配置草稿确认。窗口操作通过 Fyne 公开的原生窗口接口和独立的 Cocoa 适配层完成；其他平台保留原生标题栏。

## 项目结构

入口使用 `cmd/loop`，桌面界面、应用服务、配置领域和系统代理后端分别放在 `internal/desktop`、`internal/app`、`internal/profile` 和 `internal/proxy`。TUN 授权辅助程序保留独立入口 `cmd/loop-tun`。具体边界与维护约定见 [架构说明](docs/architecture.md)。

日常开发使用 `make run`、`make build`、`make test`、`make vet` 和 `make package`，这些入口会自动准备依赖。首次拉取后若直接使用 `go run ./cmd/loop`、`go test` 或 IDE，请先执行一次 `make prepare`；根目录 `go run .` 继续兼容。

仓库只保存 [Fyne 补丁](third_party/fyne-patches/README.md)及上游版本/校验值。`third_party/fyne/` 是从 Go 模块缓存重建的生成目录，已加入 Git 忽略；首次准备可能下载固定版本，后续可复用缓存。构建产物、临时诊断文件、真实节点与订阅配置也不纳入版本控制。

## 配置编辑

配置页使用原生 Fyne [go-fyne-pretty-view](https://github.com/ideaconnect/go-fyne-pretty-view) v2.7.0，提供 JSON 语法高亮、行号、折叠、搜索、撤销/重做与格式化，无 WebView。

编辑器仅在首次进入配置页时加载，文件未变化时保留光标与撤销历史。UI 使用本地 Fyne 字体缓存补丁，避免切换页面和外观时重复保留大型字库；系统苹方、粗体、Emoji 和深浅色均保留。实现与测量记录见 [UI 内存](docs/ui-memory.md)。

配置页直接编辑 `profiles/config.json`，包含本地 mixed 入口、日志、缓存、DNS、策略组、分流规则和规则库。没有配置下拉或只读运行预览。
节点和订阅继续在“节点”页管理；TUN 和分流模式在首页管理。

升级时自动把原有 `base.json` 与 `routing.json` 合并为 `config.json`，保留原文件作为迁移备份；后续只读取主配置，不再读取或同步这两个旧文件。已有主配置时绝不被应用内置默认值覆盖，即使主配置有错误也不会静默回退到旧文件。

点击“格式化”统一 JSON 缩进；无效 JSON 会显示错误行、列，并在执行格式化/校验/保存时跳到错误处。只接受严格 JSON，不支持注释、尾逗号或重复字段。文本上限 2 MiB。输入时保持你的排版，不会边输入边重排。

“校验”执行 JSON、客户端约束和 `sing-box check`，不修改文件，也不建立代理连接。编辑主配置时，使用仅用于校验的占位出口检查完整的自动分流策略，即使当前是全局直连或全局代理，也不会遗漏自动分流规则的错误；该出口不会启动或持久化。

“保存”在通过相同校验后，将原文件备份到同目录的 `.bak`（保留最近一次），再原子替换源文件，文件与备份权限为 0600。校验失败或检测到文件已被外部修改时保留原文件。重新载入可以丢弃当前草稿、读取磁盘内容；如有草稿会先提示。

连接期间允许编辑草稿，停止连接后可校验、保存；保存后下次启动生效。切换侧边栏或模式不会丢失草稿。未保存内容只保留在 UI 内存中，关闭窗口或正常退出会提示。强制结束 UI 无法保留草稿，但不会结束独立后台。

使用 `-config` 调试模式时，此页直接编辑指定的完整 JSON 文件，保存前同样执行内核检查和备份；此模式绕过分离配置管理。

## 策略组

节点页只管理本地节点和订阅，所有节点选择集中在“策略”页。左侧列出策略组，右侧显示该组可选的节点与子策略，点击后保存并显示勾选。

- `Available`：固定的默认策略，包含全部本地与订阅节点。Final、全局代理、代理 DNS 与规则库下载默认通过它；沿用升级前所选节点。
- `YouTube`、`Telegram`：默认选择 Available，也可以选国家组或具体节点。内置域名规则与 Telegram IP 规则会指向对应应用组；这里按目的域名/IP 分流，并非按进程识别 App。Telegram IP 段来自 [官方列表](https://core.telegram.org/resources/cidr.txt)，是随版本提供的快照。
- `HK`、`TW`、`SG`、`JP`、`KR`、`US`：根据节点名称的地区文字、代码、旗帜筛选，可编辑正则表达式。没有匹配节点时显示为空，不会自动直连。

以上均为手动选择组，不会自动切换或故障转移。可以新增策略组、编辑节点筛选和组引用；新增应用组后，在规则页添加对应规则。Available 固定保留。删除其他组时，会同时从其他组的可选列表中移除该组；如果其他组当前选用了它，或规则、Final、DNS 等仍依赖它，则提示具体依赖，先切换选择或修改对应引用后再删除。删除不会移除节点或订阅，也不会自动更换流量出口。定义中的节点筛选会随订阅节点更新自动展开，订阅自带策略不会覆盖本地策略。

右上角依次为测速、编辑、删除图标。测速批量测量当前组节点的延迟，并在行末显示 `ms`、超时或失败；子策略测量其当前选中的实际节点，重复节点只测一次。再次点击测速位置的停止图标可取消。测速通过节点请求 `https://www.gstatic.com/generate_204`，要求返回 HTTP 204；耗时包含连接节点、TLS 握手和响应头，并非下载带宽或 ICMP ping。默认并发 4、每节点超时 10 秒，结果仅保留在本次运行内存中。独立测速内核不修改系统代理、TUN、主配置或策略选择，已有连接继续运行；测速期间暂时锁定其他代理与配置操作。

策略组定义写进同一份主配置的 `policy_groups`；选中项保存在本地选择文件。运行时编译成 sing-box [selector](https://sing-box.sagernet.org/configuration/outbound/selector/)，客户端字段不传给内核。连接中选择节点或子策略会停止并重新连接，失败时恢复原选择并尝试恢复连接；TUN 重连可能再次请求管理员授权。策略组定义在停止连接后修改，配置草稿未保存时禁止跨页写入。

升级旧主配置时保留 `config.json.pre-policies.bak`，把原 `proxy` 引用迁移为 Available，并在已有规则后追加应用规则，保留原规则的优先级。所选节点消失时会提示重新选择；订阅刷新若会移除策略组选中节点则拒绝本次刷新，先在策略页切换再更新。

## 规则管理

“规则”页顶部只保留标题、数量与操作图标，左侧分为 **Final 兜底 / 本地规则 / 订阅规则**，右侧采用自适应规则卡片。使用标准 Fyne 组件，支持深浅色与系统字体；点击卡片的 `…` 或右键打开管理菜单。所有修改直接写入同一份 `profiles/config.json`。

- **Final**：选择未命中前面规则的流量默认直连或走策略组（默认 Available），点击“保存”应用。
- **本地规则**：支持完整域名、域名后缀、域名关键词、IPv4 / IPv6 地址或 CIDR、私有 IP；处理方式可选直连、策略组或拦截。点击右上角 `+` 添加，卡片菜单提供编辑、删除、上移 / 下移。一条规则可填写多个值，卡片展示首个值与其余数量，编辑时可查看全部。
- **订阅规则**：支持 [sing-box 远程规则集](https://sing-box.sagernet.org/configuration/rule-set/)的 JSON 源格式与 `.srs` 二进制格式。添加时填写名称、链接、格式、更新间隔和处理策略；内核负责下载、缓存及定时更新。此处订阅的是规则集，不是节点订阅，也不是整份 sing-box 配置。卡片显示规则集定义，不把文件内的条目数量冒充已加载规则数。
- 新域名规则置顶；新 IP 规则默认插在无条件解析步骤之后，缺少时补上。新增订阅可选“仅域名 · 优先匹配”或“包含 IP · 解析后匹配”。卡片编号是主配置中的真实匹配顺序，分组展示不改变优先级；订阅的单一简单路由引用也可在菜单中上移 / 下移。
- 编辑订阅保留下载客户端等已有设置；更换 URL / 格式时移除旧的初始快照路径。订阅名称固定以保护引用。删除订阅会同时移除其简单路由引用；若 DNS、复杂条件或多个规则集共用的条件仍依赖它，则阻止删除并提示先处理引用。多标签定义、复杂引用的处理策略仍在配置页管理。
- 逻辑规则、解析动作和非远程规则集引用继续在本地规则中原样展示，不会被常规表单简化。高级条件在配置页编辑。

每次规则修改都先执行同一套 JSON 与内核校验，通过后备份并保存；不会修改 DNS、节点或节点订阅。DNS 规则仍在配置页独立调整。连接中可新增、编辑、删除、排序规则以及修改 Final；自动分流模式下会检查实际策略选择，再自动停止并重新连接以应用新规则，过程中已有连接会短暂中断，TUN 可能再次请求管理员授权。新规则启动失败时恢复原配置并尝试重新连接原配置，若恢复连接也失败则明确显示未连接与错误。校验失败不停止原连接；全局直连 / 全局代理期间仅保存，当前连接保持，切回自动分流后生效。未连接时保存不会自动启动代理。配置页有未保存草稿时禁止规则页写入，请先保存或放弃草稿，避免相互覆盖。

## 首页分流模式

- **全局直连**：进入本软件的流量全部直连，DNS 使用系统解析；不需要选择节点，也不会启动代理节点的联网测试。内核仍运行，本地代理入口仍可使用。
- **自动分流**：使用主配置 `config.json` 中保存的规则与 DNS；默认国内直连、其他代理。
- **全局代理**：局域网域名和私有 IP 直连，其他流量走 Available；其他域名通过 Available 访问 Cloudflare DoH。

首次使用默认为自动分流。模式单独保存在 `settings.json`，不覆盖规则、节点、订阅和当前节点选择。停止时切换只保存设置；连接中切换会自动停止、应用模式并重连，已有连接会中断。如果新模式启动失败，会恢复原模式设置并尝试重新连接。完整配置 `-config` 调试模式不提供此切换。

全局模式是生成配置时的临时覆盖，切回自动分流即可恢复主配置中的规则。

这些模式决定**进入软件的流量**如何分流，与系统代理 / TUN 的接管方式独立。开启 TUN 后也可以使用“国内直连、其他代理”。

## TUN 模式（macOS）

首页新增 **TUN 模式**，默认关闭，只能在停止连接后修改，保存于 `settings.json`，不改动节点、订阅和自动分流策略。

- **关闭**：使用原有 HTTP / HTTPS / SOCKS 系统代理，启动不需要 TUN 授权。
- **开启**：启动时由 macOS 弹出管理员授权，独立 Go 辅助进程启动包内内核，创建双栈 TUN 并接管 TCP / UDP。Fyne 界面仍以普通用户身份运行；TUN 不修改 HTTP / HTTPS / SOCKS 系统代理。
- 每次 TUN 启动、分流模式切换导致的重连，都可能再次请求授权。取消授权或启动失败会停止本次连接。
- 已有 VPN / TUN 接管默认路由时拒绝启动，请先关闭其他 VPN，避免覆盖其路由。普通系统代理软件也建议停止，避免浏览器仍主动连接到它。
- 正常停止、后台退出或后台崩溃断开控制连接时，辅助进程向内核发送退出信号。UI 的退出或崩溃不会断开后台与辅助进程的控制连接。临时 DNS 随系统会话销毁，内核关闭 TUN 并释放路由。辅助进程随连接退出，没有安装常驻 root 服务。

TUN 配置启用出站网卡自动检测以避免回环，DNS 请求交给同一套分流策略，并开启反向域名映射。为避免系统 DNS 已指向 TUN 时循环解析，生成配置中的 `local` DNS 临时替换为经 `direct` 出口访问的 `223.5.5.5`；保存的策略不变。自定义显式 DNS 上游保留。应用自身使用 DoH / DoT 的流量仍按普通连接处理，不会被还原成明文 DNS。

DNS 通过 macOS Dynamic Store 的临时独立服务接入，不覆盖 Wi-Fi / 以太网保存的 DNS。TUN 内核使用 root 私有临时工作目录，停机后删除，不向用户配置目录写入 root 缓存；规则库每次先读取随配置提供的快照，连接期间可更新，更新缓存不跨 TUN 会话保留。TUN 入站只注入运行配置，不写回主配置。

开发运行 TUN 时先构建辅助程序，再显式指定（打包应用会自动查找）：

```sh
go build -o bin/loop-tun ./cmd/loop-tun
make build
./bin/loop -tun-helper "$PWD/bin/loop-tun"
```

`-render-config` 可导出 TUN 配置供 `sing-box check` 检查；`-check-connection` 刻意拒绝启动 TUN，整机接管需要在界面执行授权。当前只实现 macOS，尚未完成其他平台的 TUN 后端。新版本已通过三种分流模式 × 两种接管方式的内核配置检查，以及辅助进程断连清理测试；管理员授权后的实际 TUN 联网仍需在关闭其他 VPN 的环境下验证。

## 分流规则与节点独立

```text
profiles/
  config.json           唯一主配置：本地入站、日志、缓存、DNS、分流规则与规则库
  nodes.json            手动节点的连接参数（私密，不提交）
  subscriptions.json    每个订阅的独立节点快照（私密，不提交）
  selection.json        Available 的来源 ID + 节点 ID（兼容 CLI，不提交）
  policy-selections.json 其他策略组的独立选择（不提交）
  settings.json         分流模式与 TUN 开关，首次切换后创建（不提交）
  rule-sets/            首次启动使用的国内规则库快照
```

每次启动都读取上述文件，在用户数据目录生成 `runtime/config.json`，交给 sing-box。运行配置是生成物，不要手动编辑；它也含有节点凭据。

分流规则引用策略组名称（例如 `Available`、`YouTube`、`Telegram`）或 `direct`（直连），不能绑定具体节点。旧 `proxy` 引用仍兼容并指向 Available。换节点只改策略选择；导入或更新订阅只改对应订阅的节点快照。订阅内的路由、DNS、入站、策略组都不会替换自己的配置。所有相关输入缺失或无效时会报错，不会自动切换直连。

默认规则按顺序执行：

1. 私有 IP、本地网络域名直连。
2. `.cn` 与 `geosite-cn` 国内域名直连。
3. 解析其余域名，再判断私有 IP、`geoip-cn` 国内 IP，匹配则直连。
4. YouTube 域名走 YouTube 策略，Telegram 域名与官方公布的 IP 段走 Telegram 策略；默认均跟随 Available。
5. 其余请求通过 Available 兜底。

国内域名用系统 DNS；其余域名通过当前代理节点访问 Cloudflare DoH。节点服务器域名使用系统 DNS 做引导解析，避免循环依赖。节点自己的 `domain_resolver` 不会覆盖这套 DNS 策略。

国内规则库采用 [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat)，附带本地快照，首次运行不必等下载；sing-box 后续每天经当前代理更新并缓存。归属判断受规则库准确度影响。

### 自定义规则

编辑 `profiles/config.json`，在 `route.rules` 最前面添加自己的覆盖规则，例如：

```json
{ "domain_suffix": ["example.com"], "action": "route", "outbound": "direct" }
```

强制代理则把 `outbound` 改为 `Available` 或相应策略组名称；拦截用 `{"domain_suffix":["ads.example"],"action":"reject"}`。规则从上往下匹配；不要填写节点名或订阅名。若也要改变某域名的 DNS 解析路径，需要在 `dns.rules` 的最前面添加对应规则，引用 `dns-direct` 或 `dns-proxy`。

也可直接在配置页编辑同一内容；停止连接后保存，下次启动生效。策略使用 sing-box JSON，当前没有 Loon 文本规则转换器。

## 从 MiniProxy 升级

应用现名为 **Loop**，应用包为 `Loop.app`，入口命令为 `loop`、`loopd`、`loopctl` 和 `loop-tun`；Go 模块名为 `local/loop`。默认数据目录为 `~/Library/Application Support/loop/`。

打包应用首次使用默认配置时，会从旧 `MiniProxy` 数据目录复制节点、订阅、主配置、规则库、模式、策略选择和菜单栏偏好；旧文件保留。已有 Loop 配置不会被覆盖，自定义 `--profiles` / `--state-dir` 目录不自动导入旧数据。旧后台的运行配置、代理恢复记录、锁和日志不迁移，避免把旧进程的网络状态带到新后台。原有主题和界面偏好也会迁移到新应用 ID。

请先通过旧版菜单栏退出 MiniProxy，再打开 Loop。登录自启绑定 macOS 应用标识，改名后请在 Loop 的“更多 → 开机自启”重新开启，并关闭旧版的登录项。

## 独立后台与命令行

现在 UI 与后台是两个独立进程。点击 `×` 会销毁 UI 进程并释放其界面资源；后台、代理连接和 macOS 菜单栏继续运行。菜单栏“显示主窗口”会重新启动 UI 并附加到当前连接；不会重启代理。`⌘Q` 或菜单栏“退出 Loop”停止整个应用。

`loopd` 和 `loopctl` 都不依赖 Fyne。后台仅监听当前用户私有的 Unix Socket，无 TCP 端口；CLI 的 `--json` 返回 `ok / error / status / result`，失败退出码非零，适合脚本和 AI 直接调用。状态包含策略组、节点的稳定 key 和连接状态，不包含节点凭据或订阅链接。

```sh
make build
# 仅启动后台，不打开 UI，也不会自动连接
./bin/loopctl daemon start
./bin/loopctl status --json
./bin/loopctl connect --json
./bin/loopctl mode rule --json  # direct / rule / global
./bin/loopctl policy list --json
# 从 policy list 的 members[].key 取得节点键或子策略名称
./bin/loopctl policy select Available '<节点 key>' --json
./bin/loopctl disconnect --json
./bin/loopctl ui                # 打开或激活 UI
./bin/loopctl daemon stop --json
```

打包后使用应用内自带的 CLI：

```sh
"/Applications/Loop.app/Contents/MacOS/loopctl" status --json
```

AI 可直接执行这些命令，无需打开窗口或另建 MCP 服务。策略名、节点 key 中有空格时使用 shell 引号；应读取状态中的 `busy` 和命令错误，等待正在执行的操作完成后再重试。

纯命令行环境可使用 `daemon start --no-tray`，不创建菜单栏；此参数只影响新启动的后台。也可在前台直接运行 `./bin/loopd --no-tray --profiles /绝对路径/profiles --state-dir /绝对路径/state`，日志写到终端，SIGINT / SIGTERM 停止并恢复代理。

`--state-dir` 用于隔离后台及日志；独立实例同时指定独立的 `--profiles`，并避免端口或系统代理/TUN 接管冲突。所有控制命令使用相同状态目录。UI 默认启动后台和菜单栏；首次运行迁移原菜单栏偏好到状态目录 `desktop.json`。后台重启后旧 UI 会拒绝继续控制，关闭重开即可附加新实例。

CLI `daemon stop` 直接停止后台，已打开的 UI 会保留草稿并显示后台不可用；关闭重开会确保后台重新启动。登录自启仍打开主应用，再启动后台，不会自动连接代理。

## 开发运行

需要 Go、Xcode Command Line Tools、sing-box 1.14.4+。

```sh
brew install sing-box
# 新 checkout：从示例创建这三个文件，再填入节点。
# cp profiles/nodes.example.json profiles/nodes.json
# cp profiles/subscriptions.example.json profiles/subscriptions.json
# cp profiles/selection.example.json profiles/selection.json
make run
```

本地已经迁移了提供的 Megabox 节点。原来的 `config.json` 保留作为备份；默认启动使用 `profiles/config.json` 主配置，并独立管理节点与订阅。`go run ./cmd/loop -config config.json` 是绕过独立分流的完整配置调试模式。

### 管理节点

“节点”页仅用于管理，不提供连接节点切换、搜索或底部操作按钮。右上角 `+` 直接打开添加弹窗，提供“单个节点”和“订阅链接”两个页签。单节点目前仅接受一个 `vless://` 链接，名称可选，留空使用链接名称或服务器地址。连接期间可以浏览，停止后才能修改。

节点页左栏固定显示“本地节点”，下面按订阅列出来源及节点数量；右栏显示选中来源的节点列表。右上角 `+` 可添加本地节点或订阅。右键本地节点可编辑或删除，右键左栏订阅可更新、编辑或删除该订阅；订阅内的节点通过订阅统一管理。浏览来源或点击节点不会改变当前连接节点。暂不提供节点组合、筛选组或代理链。

切换连接节点可使用命令行：

```sh
go run ./cmd/loop -list-nodes
go run ./cmd/loop -select-source manual -select-node megabox-40
```

旧的 `-select-source / -select-node` 参数只允许后台未运行时使用。在线选择请使用下方的 `policy select`，后台会自动重连并在失败时恢复原选择。

### 导入、更新与切换订阅

在“节点”页点击右上角 `+`，切换到“订阅链接”，填写名称和 URL，点击“获取并添加”。订阅使用 **Clash YAML**，获取时使用 `clash.meta` User-Agent，只读取 `proxies`，转换为 sing-box 节点后保存到 `subscriptions.json`；订阅 URL、类型、更新时间与节点快照一起保存，供以后刷新使用。请求、解析或内核检查失败时保留表单与旧数据。订阅拉取采用直连，超时 30 秒，最大 8 MB。

支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、AnyTLS、SOCKS5、HTTP 的常用字段，以及 TLS/Reality、WebSocket/gRPC/HTTPUpgrade。暂不支持插件、链式节点、Base64 URI 订阅列表和其他传输方式；遇到不支持的节点会提示并拒绝本次导入。每个节点以订阅里的名称作为 ID，重复名称会报错。

新增单节点入口仅接受 VLESS 分享链接。右键编辑已有本地节点仍保留原有分享链接、手动表单和高级 JSON 编辑能力，支持原有协议。保存时调用内核检查配置，这不等同于每个节点均已通过联网测试；添加成功后只切换节点页展示来源，使用哪个节点仍在策略页选择。

命令行仍接受**本地 sing-box JSON 导出文件**，从 `outbounds` 提取节点，以原始 `tag` 作为 ID：

```sh
go run ./cmd/loop -import-subscription /path/to/provider.json \
  -subscription-id airport-a -subscription-name '机场 A'
go run ./cmd/loop -list-nodes
go run ./cmd/loop -select-source airport-a -select-node '香港 01'
```

界面右键订阅选择“更新订阅”或命令行用同一个 `subscription-id` 再次导入，即更新其节点快照；自己的规则、DNS、手动节点和当前选择都保留。如果新订阅删除了当前所选节点，更新会被拒绝并保留旧快照。此时先切到手动节点或其他来源，再更新和重新选择。

### 配置位置

- 开发运行默认使用工作目录下的 `profiles/`。
- `.app` 首次运行将内置配置复制到 `~/Library/Application Support/loop/profiles/`，以后使用这份可编辑副本。重新打包或升级应用只补齐缺失文件，不覆盖自己的规则和节点。不要直接修改签名应用包内部。
- `-profiles /绝对路径` 可以指定另一套独立配置目录。
- 命令行修改已安装应用的配置，可运行 `dist/Loop.app/Contents/MacOS/loopctl -list-nodes` 等；或者给 `go run ./cmd/loop` 加 `-profiles "$HOME/Library/Application Support/loop/profiles"`。
- 内核日志：`~/Library/Application Support/loop/sing-box.log`。
- 运行配置：`~/Library/Application Support/loop/runtime/config.json`，权限 0600。

### 验证

```sh
go test -race ./...
go vet ./...
go run ./cmd/loop -check-connection
# 如需检查或手工运行生成配置，请写入自己的临时路径：
go run ./cmd/loop -render-config /tmp/loop-config.json
sing-box check -c /tmp/loop-config.json
```

节点连接检查不会修改系统代理。测试覆盖切换节点/订阅后的规则与 DNS 不变、订阅刷新不会注入策略、选中节点丢失时拒绝更新、应用升级保留自定义配置，以及系统代理恢复。

## 打包

```sh
bash scripts/package-macos.sh
open dist/Loop.app
```

生成自带内核、主配置、节点订阅数据和规则库的 `.app`、本机 CPU 架构的 ZIP，用户无需安装 Go、Homebrew 或 sing-box。当前是同架构 macOS 15+ 的本地 ad-hoc 签名构建，不是 universal 包。

包内含当前节点凭据，适合自己的本地使用；分享前换成适合分发的配置。对外分发另需 Developer ID 签名、公证，以及按 sing-box GPL-3.0-or-later 许可提供对应源码与第三方许可说明。

## 使用边界

- 默认是 `127.0.0.1:2080` 系统代理模式；TUN 需在首页单独开启和授权。登录自启在「更多 → 开机自启」设置，默认不自动注册。
- 验证独立运行时退出 Karing/Loon 等 VPN，避免代理叠加。
- 连接期间切换物理网络后，需要停止再启动。
- 不修改 PAC、自动发现、认证代理；检测到这些配置时提示先处理。系统代理已有绕过列表保留，匹配地址可能不经过 sing-box。
- 系统代理模式强制结束后台/断电可能留下系统代理；下一次启动连接会先根据 `proxy-state.json` 恢复，也可在系统设置手动关闭。系统代理模式强制结束后台还可能留下内核子进程，需要结束它再重试。TUN 模式能处理后台控制连接断开；同时强制终止授权辅助进程和内核、系统异常等情况尚不保证自动清理，不应与正常停止等同。

## Android

当前只完成 macOS 后端。界面、配置合成和 `Backend` 接口可复用，但安卓整机代理仍需额外集成 libbox、VpnService、VPN 授权、前台服务和 TUN 管理。
