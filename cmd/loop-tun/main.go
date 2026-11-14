package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"local/loop/internal/tunhelper"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	socket := flag.String("socket", "", "client socket")
	token := flag.String("token", "", "session token")
	client := flag.Bool("client", false, "连接已安装的 TUN 服务，首次安装时授权一次")
	service := flag.Bool("service", false, "由 launchd 启动授权服务")
	install := flag.Bool("install-service-root", false, "安装授权服务（需要 root）")
	setup := flag.Bool("install-service", false, "安装或更新 TUN 服务（首次请求系统授权）")
	uninstall := flag.Bool("uninstall-service", false, "移除已安装的 TUN 服务")
	uninstallRoot := flag.Bool("uninstall-service-root", false, "移除授权服务（需要 root）")
	status := flag.Bool("service-status", false, "显示 TUN 服务状态")
	core := flag.String("core", "", "打包内核路径")
	uid := flag.Int("uid", -1, "服务所属用户 UID")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	count := 0
	for _, selected := range []bool{*client, *service, *install, *setup, *uninstall, *uninstallRoot, *status} {
		if selected {
			count++
		}
	}
	if count > 1 {
		return fmt.Errorf("一次只能选择一个 TUN 服务操作")
	}
	if *service {
		return tunhelper.RunInstalledService(ctx)
	}
	if *install {
		return tunhelper.InstallService(*core, *uid)
	}
	if *setup {
		helper, err := os.Executable()
		if err != nil {
			return err
		}
		if *core == "" {
			*core = filepath.Join(filepath.Dir(helper), "sing-box")
		}
		return tunhelper.EnsureService(ctx, helper, *core)
	}
	if *uninstallRoot {
		return tunhelper.UninstallServiceRoot()
	}
	if *status {
		state, err := tunhelper.StatusService(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(state)
	}
	if *uninstall {
		helper, err := os.Executable()
		if err != nil {
			return err
		}
		return tunhelper.UninstallService(ctx, helper)
	}
	if *client {
		if os.Geteuid() == 0 || *socket == "" || len(*token) != 64 || *core == "" {
			return fmt.Errorf("TUN 客户端参数无效")
		}
		helper, err := os.Executable()
		if err != nil {
			return err
		}
		return tunhelper.RunServiceClient(ctx, helper, *core, *socket, *token)
	}
	if os.Geteuid() != 0 || *socket == "" || len(*token) != 64 {
		return fmt.Errorf("TUN 辅助进程必须由 Loop 授权启动")
	}
	conn, err := net.DialTimeout("unix", *socket, 5*time.Second)
	if err != nil {
		return err
	}
	return tunhelper.Serve(ctx, conn, *token)
}
