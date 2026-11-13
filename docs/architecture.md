# Loop 架构

## 进程与职责

应用分为两个独立进程：`Loop` 是 Fyne UI，`loopd` 是后台运行时。连接时后台再启动 sing-box 子进程；使用 TUN 时还会启动独立的提权辅助程序。因此“两进程”指应用角色，实际进程数会随连接方式增加。

```text
Loop / Fyne UI ──┐
                ├─ Unix Socket / HTTP JSON v1 ─ loopd ─ sing-box
loopctl / CLI ───┘                               │
                                       原生 Cocoa 菜单栏
                                                │
                                       macOS TUN helper
```

- UI 只管理窗口、编辑草稿和交互任务；关闭窗口会退出整个 UI 进程。
- 后台拥有代理生命周期、模式、策略选择、配置写入和菜单栏。macOS 打包时放在独立标识的 `Contents/Helpers/loop-background.app` 中，避免 LaunchServices 把菜单栏后台误认为 UI，导致再次打开应用只激活后台。UI 消失不会停止后台或代理。
- CLI 不依赖 Fyne，可启动后台、查询状态、开关连接、切换模式和策略，并输出 JSON。
- sing-box 仍作为内置可执行文件使用，没有嵌入 Fyne 或更换内核。

## 目录

```text
cmd/
  loop/       桌面入口，也兼容管理参数
  loopd/      后台前台运行入口，可独立部署
  loopctl/    独立命令行入口，无 Fyne 依赖
  loop-tun/   macOS 提权辅助程序
internal/
  desktop/        Fyne 页面、编辑器、窗口与 UI 生命周期
  cli/            参数解析、命令分派与 JSON 输出
  control/        后台控制器、单实例、Unix Socket API、客户端与进程启动
  statusbar/      后台的 Cocoa 菜单栏，无 Fyne / OpenGL 依赖
  app/            共享操作、校验保存、订阅更新、连接切换与回滚
  autostart/      macOS 主应用登录项注册与状态读取
  profile/        配置、节点、订阅、规则、策略图与持久化
  proxy/          内核生命周期、系统代理恢复、TUN 客户端与延迟测试
  tunhelper/      提权会话、TUN 路由与临时 DNS 生命周期
  testutil/       临时配置和虚构节点
profiles/         开发配置与打包时的首次运行种子
scripts/          依赖准备、macOS 打包、内核复制与签名
third_party/
  fyne-patches/  Fyne 固定版本、校验值、补丁与维护说明（纳入 Git）
  fyne/          自动重建的完整 Fyne 源码（Git 忽略）
```

`profile` 不依赖 UI、应用服务或进程管理。`proxy` 不依赖 Fyne 或配置存储。`app` 组合这些能力，并通过 `RemoteOperations` 支持 UI 转发写操作。`control` 负责操作串行化和后台状态；它不导入 `desktop`。`statusbar` 通过操作模型调用后台，无需创建 Fyne 窗口或渲染器。

macOS UI 的系统字体通过可选 `SystemFontResource` 使用 CoreText 测量和绘制：苹方用于常规/加粗文字，Menlo 用于编辑器，Emoji/符号由系统回退。原生字体缓存最多 48 项；文字布局和图像上下文逐次释放。Fyne 仍负责组件、窗口、布局与 OpenGL 纹理，Go 业务层和后台不变。普通输入框、换行和节点名称省略均使用同一套原生测量。

其他平台或未选择原生绘制的字体仍使用 Go 路径：有界缓存共享不可变字库表，仅配色/尺寸不同的主题范围复用映射；文件字体通过 `FontFileResource` 直接读取 TTC 表，回退按需加载。配置编辑器首次进入时构建，文件未变则保留内容、光标和撤销历史。启动稳定后仅回收一次临时内存，没有周期性强制 GC。字体补丁由根模块 `replace` 固定，`make` 构建/测试和打包脚本会自动准备，直接运行 Go 命令前先执行 `make prepare`。仓库只跟踪补丁，完整依赖从固定版本和校验值重建；本地修改会先要求通过 `make update-fyne-patch` 保存，避免被覆盖。升级 Fyne 时需复核，维护步骤见 [Fyne 补丁](../third_party/fyne-patches/README.md)。测量方式与结果见 [UI 内存](ui-memory.md)。

## 生命周期

打开 UI 时先尝试激活已有窗口，否则确保后台存在，并附加到其状态。后台使用单实例文件锁，不会因为第二次打开应用重复启动内核。UI 也有独立实例锁和本地激活接口。

关闭 `×` 会确认未保存草稿、取消 UI 请求、释放窗口并退出 UI 进程；不发送停止连接命令。后台接受的连接和规则应用事务使用后台自己的 context，即使请求方退出也会完成或回滚。延迟测试使用请求 context，关闭 UI 或取消测速会回收独立测试内核。

菜单栏“显示主窗口”激活已有 UI，或重新启动 UI 进程。菜单栏“退出 Loop”优先让已有 UI 确认未保存草稿，再退出后台；没有 UI 时直接停止后台。`⌘Q` 同样执行完整退出。CLI `daemon stop` 直接停止后台，不等待 UI 草稿确认；UI 保留草稿并显示后台不可用，关闭重开可重新附加。信号 SIGTERM / SIGINT 只结束接收它的角色：后台清理代理，UI 仅清理界面。

登录自启继续使用 `SMAppService.mainAppService`，启动主应用后确保后台存在；不意味着自动连接代理。不安装额外常驻 root 服务。

## 本机 API 与并发

后台在当前用户私有的临时目录创建 `control.sock`，目录权限 0700、Socket 权限 0600，无 TCP 监听。目录名由状态目录路径哈希得到，避免 Unix Socket 路径过长。不同状态目录可运行独立实例；独立实例也应使用不同的 profiles、端口和接管设置。

- `GET /v1/status`：协议版本、实例 ID、PID、连接/忙碌状态、模式、策略成员与菜单偏好。不返回节点凭据和订阅 URL。
- `POST /v1/action`：共享的操作模型，返回 `ok / error / status / result`。拒绝未知字段、多个 JSON 对象和超过 8 MiB 的请求。
- 操作串行执行，忙碌时返回 HTTP 409。CLI 使用非零退出码和 `ok:false` 报告失败。
- 客户端附加后绑定后台实例 ID。后台重启后，旧客户端的写入被拒绝，防止旧窗口修改新实例。
- 配置保存保留内容基线检查、原子替换、0600 文件权限和最近一次备份。
- 旧 CLI 文件修改参数持有与后台相同的锁，只允许离线执行；在线选择使用 `policy select`。

UI 每秒读取后台快照，刷新主线程状态并保留编辑草稿。UI 中的连接、配置、规则、节点和订阅写操作均经后台执行；读文件仍使用现有 Store。用户手工外部编辑不受后台互斥锁管理，保存时的基线检查仍会检测冲突。

规则、模式和策略在线切换复用 `app` 的停止、启动、失败回滚流程。规则校验失败保留原连接；自动分流在线保存成功后重连；全局模式仅保存，切回自动分流生效。后台自行持有事务 context，UI 的退出不会破坏恢复连接。

应用身份、默认状态目录和旧版迁移集中在 `app/identity.go`。安装版首次使用默认目录时复制 MiniProxy 的持久配置，先完整写入临时目录再发布，保留旧源文件与新目录已有数据；主题偏好在 Fyne 读取新 ID 前导入。代理恢复记录、运行配置和锁不跨应用迁移，登录项由用户在新应用中重新注册。

主配置仍为 `profiles/config.json`，节点和订阅独立保存。菜单栏偏好由状态目录的 `desktop.json` 保存，第一次附加 UI 时迁移旧 Fyne 偏好；主题由 UI 自己保存。运行配置 `runtime/config.json` 是含凭据的生成物，不回写主配置。

## 构建与验证

```sh
make run            # 构建全部角色后打开 UI
make build          # UI、daemon、CLI、TUN helper
make test           # 全量回归 + race detector
make test-headless  # 无 CGO 的后台/核心包测试，不编译 Fyne
make vet
make package        # 当前 macOS 架构的完整应用和 ZIP
```

控制器和 Socket 测试使用虚构节点与替身后端，不修改系统代理或 TUN；配置检查集成测试使用 PATH 中的 sing-box，缺少时跳过。覆盖客户端退出后事务继续、重复连接幂等、多个客户端同步、写操作互斥、单实例/权限、旧实例拒绝、退出清理、规则回滚、菜单模型、CLI JSON 与 UI 偏好同步。

目前交付目标为 macOS。Unix Socket 控制层可用于其他 Unix 平台；Windows 需要补充 IPC/进程适配。Android 仍需要 VpnService/libbox、授权与前台服务，不能直接沿用桌面子进程和 macOS helper。
