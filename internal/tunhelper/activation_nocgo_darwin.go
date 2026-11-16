//go:build darwin && !cgo

package tunhelper

import (
	"errors"
	"net"
)

const serviceSocketActivationAvailable = false

func activatedServiceListener() (net.Listener, error) {
	return nil, errors.New("TUN 按需服务需要启用 CGO 构建辅助程序")
}
