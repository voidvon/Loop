# MiniProxy 架构

## 结论与本次调整

Go 没有强制统一的客户端目录模板。原来根目录多个文件组成 `package main` 是有效组织方式，已有的 `internal/profile`、`internal/proxy` 和独立 TUN 辅助程序也合理。随着节点、规则、配置编辑增加，主要问题变成入口包承担太多职责、界面执行内核校验与订阅更新，以及规则表单依赖配置编辑页保存。

本次采用 Go 官方文档介绍的 `cmd` 与 `internal` 组织方式，把入口、桌面呈现和应用操作分开。没有引入通用仓储、依赖注入框架或额外配置格式。参考：[Go 官方模块布局说明](https://go.dev/doc/modules/layout)。

## 目录与职责

```text
cmd/
  miniproxy/main.go       正式客户端入口：连接 CLI 与桌面启动器
  miniproxy-tun/main.go   独立的 macOS 提权辅助程序入口
internal/
  cli/                   参数解析、命令分派；不依赖 Fyne
  app/                   配置校验保存、节点校验、订阅更新、模式/策略切换回滚
                         配置目录定位、首次复制与旧配置迁移
  autostart/             macOS 主应用登录项注册与状态读取；不依赖 Fyne 或代理配置
  desktop/               Fyne 窗口、页面、编辑器、交互状态与异步任务调度
  profile/               配置模型、JSON 持久化、规则修改、节点/订阅解析
                         主配置 + 策略图 + 节点 + 模式编译为 sing-box 运行配置
  proxy/                 sing-box 查找/校验/进程生命周期、连接检查
                         系统代理恢复、TUN 辅助程序客户端、Backend 接口
  tunhelper/             macOS 提权会话、TUN 路由与临时 DNS 生命周期
  testutil/              测试专用的临时配置与虚构节点
profiles/                开发配置与打包时的首次运行种子
scripts/package-macos.sh 打包、内核复制、许可、签名与 ZIP
docs/                    架构与维护说明
main.go                  兼容原有 go run . 的薄入口
Makefile                 常用开发命令
dist/、bin/              构建产物，不作为源代码
```

`profile` 内按职责拆为 `types.go`、`storage.go`、`nodes.go`、`compile.go`，并与现有配置、规则、模式和导入文件协作，避免重新形成单个大文件。

## 依赖方向

```text
cmd/miniproxy ──→ cli ──→ app ──→ profile
       │                   └──→ proxy
       └──→ desktop ──→ app / autostart / profile / proxy

cmd/miniproxy-tun ──→ tunhelper
```

`profile` 不依赖 UI、应用服务或进程管理；`proxy` 不依赖 Fyne 或配置存储。`app` 组合配置和内核操作。CLI 接收桌面启动回调，不导入桌面包。桌面入口创建具体后端，现有 `proxy.Backend` 用于启动、停止与模式切换；平台实现仍由 `proxy` 负责。

`desktop/app.go` 是桌面组装与生命周期入口，负责 Fyne 主线程、窗口关闭和代理清理。界面回调调用 `app.Service` 完成配置校验/保存和节点/订阅操作。数据格式与既有用户配置目录保持兼容。

登录自启通过 `autostart.Service` 的独立平台适配实现：macOS 使用 ServiceManagement 的主应用登录项 API，其他平台返回不可用。`desktop/startup.go` 异步读写系统状态、处理等待允许与错误，并在应用回到前台时刷新；不另存启用偏好，也不参与代理连接生命周期。只有明确切换开关才注册或注销，开发命令和测试进程受应用包检查保护。

页面交互优先组合 Fyne 标准组件：模式用 `RadioGroup`，TUN 用 `Check`，卡片用 `Card`，导航用 `Button` 的 Importance 表达选中状态，菜单用 `PopUpMenu`。统一主题只定制颜色与字体，组件的尺寸、圆角、焦点、禁用状态及阴影由 Fyne 提供；自定义列表包装只补充右键事件。

窗口控制的呈现在 `desktop/window_chrome.go`，仍是 Fyne 控件；macOS 通过桌面驱动公开的 `CreateSplashWindow` 创建初始无装饰窗口，防止首帧出现系统标题栏，再由 Cocoa 同时恢复 titled/full-size-content、关闭、缩放、最小化与调整大小能力，保持标题栏隐藏。`window_darwin.go` 通过公开的 `driver.NativeWindow` 取得当前窗口句柄，`window_darwin.m` 只负责隐藏系统标题栏、最小化、缩放和标题区域拖动。Cocoa 操作调度到系统主队列，事件监听随窗口释放；没有使用反射、私有 Fyne API 或全局活动窗口。非 macOS 平台保留系统窗口装饰。

## 配置与编辑协作

持久源配置只有一份 `profiles/config.json`，包含基础设置、DNS、policy_groups 策略组与路由。节点、订阅、选择和首页模式继续独立保存。编译结果写到用户数据目录的 `runtime/config.json`，属于含凭据的生成物，不回写主配置。

配置页、规则页和策略组编辑共享 `desktop.documentController`。控制器负责当前文档路径、校验/保存任务、取消、可写状态和完成通知；`app.Service` 负责真实校验与持久化。规则页不再调用配置编辑页。编辑器通过草稿查询和保存通知与控制器同步，规则修改会阻止覆盖未保存草稿，仍保留外部文件变更检查、原子替换和最近一次备份。

规则页使用控制器的 `RunRules` 调用 `app.Service.SaveRules`，支持连接中保存。保存和实际策略编译校验成功后，自动分流连接通过 Backend 停止、重连；新配置启动失败时，比较文件基线后原样恢复旧文档，再尝试恢复原连接。校验失败不停止连接，全局模式仅保存不重启。每次启动独立限时 60 秒，防止第一次启动超时耗尽回滚恢复的期限；应用退出取消后不再重连。文档控制器等待后台清理完成，界面同步最终连接状态与保存/回滚文档，整个过程锁定其他操作。回滚保留原 `.bak`，遇到外部文件变更则拒绝覆盖并报错。

`profile/policies.go` 管理策略组定义、动态成员展开、组引用的无环校验与选中项；`policy_compile.go` 编译 selector 图。节点运行标识由来源 ID 与节点 ID 的哈希生成，跨订阅同名节点不会混淆。Available 使用原 selection.json，其他组使用 policy-selections.json；两者不写入分流规则。空的未引用国家组不生成出口，被路由依赖但不可用的组拒绝编译。客户端管理 selector 默认值，并按生成的选择组合隔离内核缓存，避免旧选择覆盖客户端状态。

`profile/policy_delete.go` 在同一份文档草稿中删除策略组并清理其他组的可选引用，通过共享文档控制器一次校验、备份、保存。当前选中该组的父策略与路由/DNS/下载依赖会阻止删除，并提供具体策略名称或字段路径；不自动改变选择或路由。确认后再次检查选择依赖，仍保留文档基线冲突保护。

`app.SwitchStrategy` 用 Backend 做停止、重连与失败回滚；桌面串行化策略选择、模式切换、节点和文档操作。策略页使用标准 List、Button、CheckGroup、Entry 与 Dialog 组成左右布局，节点管理页不读取或改变当前选择。

`app.StrategyProbeTargets` 将当前组成员解析为去重的实际节点快照，不落盘选中项。`proxy.LatencyProber` 为每批测速启动独立、无提权的内核，以随机认证的 loopback mixed 入站和 `auth_user` 路由让并发请求分别经过指定节点；未匹配请求拒绝。临时配置权限 0600，不加载主配置、缓存、系统代理或 TUN。请求使用显式 HTTP 代理访问 HTTP 204 目标，逐项返回耗时或错误，回调可并发执行。取消和退出均终止、回收子进程并删除临时文件。`desktop/strategy_latency.go` 负责内存结果、进度、操作锁和主线程更新，不修改策略选择。

`-config` 是完整 sing-box 配置的调试入口，跳过分离配置目录初始化，直接校验与编辑指定文件。

## 并发与线程约定

- 从主 goroutine 调用 `desktop.Run`；界面字段与共享文档控制器只在 Fyne 主线程访问，后台完成后使用 `fyne.Do` 回调。
- 内核启动/停止、校验和订阅请求放在后台任务中，使用 context 超时与取消。
- 应用服务不拥有后台 goroutine 或界面状态，调用方负责串行执行文件修改。当前桌面通过连接、节点管理和文档任务锁协调操作。
- 连接中规则可通过保存并重连流程落盘；节点、策略组定义与完整配置仍在停止后修改，配置编辑草稿可继续保留。主配置保存与规则回滚通过共享控制器同步编辑器基线。
- 退出时取消任务并清理后端。独立 CLI 与运行中的 GUI 没有跨进程写锁；需要外部编辑时先停止连接与管理操作。文档保存有内容基线冲突检测，但不等于跨进程事务。

## 开发与验证

```sh
make run            # 正式入口；go run . 仍兼容
make build          # 构建客户端与 TUN helper
make test           # 所有测试 + race detector
make test-headless  # 不编译 Fyne；验证无 CGO 的核心包边界
make vet
make package        # 当前 macOS 架构的应用与 ZIP
```

核心集成测试需要 PATH 中的 sing-box，缺少时跳过相关测试。测试使用临时目录和虚构节点，不读取实际节点凭据、不改变系统代理、不启动整机 TUN。配置校验测试会执行 `sing-box check`。

命令行测试覆盖帮助、参数冲突、GUI 分派、节点选择、运行配置导出，以及完整配置模式不初始化 profiles。已有回归继续覆盖配置迁移、模式切换回滚、规则与节点隔离、草稿保护、保存失败保留原文件和代理恢复。

## 平台范围

目前交付目标仍是 macOS。本次分层有利于以后替换后端，但不能直接得到 Android VPN 客户端。Android 仍需实现 libbox/VpnService、授权、前台服务与移动端生命周期；不能复用桌面外部进程与 macOS helper 作为整机代理实现。暂时没有为未实现的平台创建空架构。

继续增加页面时，呈现逻辑留在 `desktop`，可独立测试的用户操作放入 `app`，格式解析与存储规则放入 `profile`，系统与内核进程相关代码放入 `proxy`。当实际出现第二个实现或测试替身需要时再提取接口。
