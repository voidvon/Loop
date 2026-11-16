//go:build darwin && cgo

package tunhelper

/*
#include <launch.h>
#include <stdlib.h>
#include <unistd.h>

static int loop_activate_socket(int *fd) {
	int *fds = NULL;
	size_t count = 0;
	int result = launch_activate_socket("Control", &fds, &count);
	if (result != 0) return result;
	*fd = count == 1 ? fds[0] : -1;
	if (count != 1) {
		for (size_t i = 0; i < count; i++) close(fds[i]);
	}
	free(fds);
	return 0;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
)

const serviceSocketActivationAvailable = true

func activatedServiceListener() (net.Listener, error) {
	var fd C.int
	if code := C.loop_activate_socket(&fd); code != 0 {
		return nil, fmt.Errorf("获取 launchd TUN Socket 失败：%w", syscall.Errno(code))
	}
	if fd < 0 {
		return nil, errors.New("launchd TUN Socket 数量异常")
	}
	file := os.NewFile(uintptr(fd), "launchd-tun-control")
	defer file.Close()
	listener, err := net.FileListener(file)
	if err != nil {
		return nil, err
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		listener.Close()
		return nil, errors.New("launchd TUN Socket 必须是 Unix Socket")
	}
	// launchd owns the path and keeps listening while this process is absent.
	unixListener.SetUnlinkOnClose(false)
	return unixListener, nil
}
