package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"local/loop/internal/tunhelper"
)

func main() {
	socket := flag.String("socket", "", "client socket")
	token := flag.String("token", "", "session token")
	flag.Parse()
	if os.Geteuid() != 0 || *socket == "" || len(*token) != 64 {
		fmt.Fprintln(os.Stderr, "TUN 辅助进程必须由 Loop 授权启动")
		os.Exit(1)
	}
	conn, err := net.DialTimeout("unix", *socket, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := tunhelper.Serve(ctx, conn, *token); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
