// Package cli handles command arguments and non-GUI commands. It does not import Fyne.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"local/miniproxy/internal/app"
	"local/miniproxy/internal/profile"
)

// Run launches the desktop only when no non-GUI command was requested.
// The launcher is supplied by the entry point so this package stays headless.
func Run(args []string, output io.Writer, launch func(app.Options) error) error {
	flags := flag.NewFlagSet("miniproxy", flag.ContinueOnError)
	flags.SetOutput(output)
	config := flags.String("config", "", "调试用完整 sing-box 配置；绕过独立节点管理")
	profiles := flags.String("profiles", "", "配置目录（包含主配置 config.json、nodes.json 等）")
	listNodes := flags.Bool("list-nodes", false, "列出所有手动节点和订阅节点后退出")
	selectSource := flags.String("select-source", "", "选择节点来源 ID：manual 或订阅 ID，配合 -select-node")
	selectNode := flags.String("select-node", "", "选择节点 ID，配合 -select-source")
	importSubscription := flags.String("import-subscription", "", "导入本地 sing-box JSON 订阅文件，仅提取节点")
	subscriptionID := flags.String("subscription-id", "", "导入订阅的固定 ID")
	subscriptionName := flags.String("subscription-name", "", "导入订阅的显示名称")
	render := flags.String("render-config", "", "生成完整 sing-box 配置到指定文件后退出")
	core := flags.String("core", "", "sing-box 内核路径（默认优先使用应用包内内核）")
	helper := flags.String("tun-helper", "", "开发用 TUN 辅助程序路径")
	check := flags.Bool("check-connection", false, "检查配置及节点连接后退出，不修改系统代理")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不支持位置参数，请使用 -help 查看用法")
	}
	if *config != "" && (*profiles != "" || *listNodes || *selectSource != "" || *selectNode != "" || *importSubscription != "" || *render != "") {
		return errors.New("-config 不能与分离配置的管理命令同时使用")
	}
	if (*selectSource == "") != (*selectNode == "") {
		return errors.New("请同时提供 -select-source 和 -select-node")
	}
	commands := 0
	for _, selected := range []bool{*listNodes, *selectSource != "", *importSubscription != "", *render != "", *check} {
		if selected {
			commands++
		}
	}
	if commands > 1 {
		return errors.New("一次只能执行一个管理命令")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	stateRoot, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	options := app.Options{Executable: executable, StateDir: filepath.Join(stateRoot, "MiniProxy"), CorePath: *core, HelperPath: *helper}
	if *config != "" {
		options.ConfigPath, err = filepath.Abs(*config)
	} else {
		options.Store.Dir, err = app.ProfileDirectory(executable, options.StateDir, *profiles)
	}
	if err != nil {
		return err
	}
	store := options.Store
	switch {
	case *importSubscription != "":
		count, err := store.ImportSubscription(*subscriptionID, *subscriptionName, *importSubscription)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "已导入订阅 %s，共 %d 个节点；分流规则和当前选择未修改\n", *subscriptionID, count)
	case *selectSource != "":
		if err := store.Select(profile.Selection{Source: *selectSource, Node: *selectNode}); err != nil {
			return err
		}
		fmt.Fprintf(output, "已选择 %s / %s；下次启动连接生效，分流规则未修改\n", *selectSource, *selectNode)
	case *listNodes:
		sources, err := store.Sources()
		if err != nil {
			return err
		}
		selection, err := store.Selection()
		if err != nil {
			return err
		}
		for _, source := range sources {
			for _, node := range source.Nodes {
				marker := " "
				if source.ID == selection.Source && node.ID == selection.Node {
					marker = "*"
				}
				fmt.Fprintf(output, "%s %s\t%s\t%s / %s\n", marker, source.ID, node.ID, source.Name, node.Name)
			}
		}
	case *render != "":
		if err := store.WriteRuntime(*render); err != nil {
			return err
		}
		fmt.Fprintf(output, "已生成 %s\n", *render)
	case *check:
		service := app.NewService(options)
		if err := service.CoreError(); err != nil {
			return err
		}
		if err := app.CheckProfileConnection(service.CorePath, options.ConfigPath, store); err != nil {
			return err
		}
		fmt.Fprintln(output, "配置与代理连接检查通过")
	default:
		if launch == nil {
			return errors.New("没有桌面启动器")
		}
		return launch(options)
	}
	return nil
}
