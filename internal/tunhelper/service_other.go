//go:build !darwin

package tunhelper

import (
	"context"
	"errors"
	"net"
)

func DialService(context.Context, string) (*net.UnixConn, error) {
	return nil, errors.New("TUN 服务仅支持 macOS")
}

func RunServiceClient(context.Context, string, string, string, string) error {
	return errors.New("TUN 服务仅支持 macOS")
}
func InstallService(string, int) error { return errors.New("TUN 服务仅支持 macOS") }
func EnsureService(context.Context, string, string) error {
	return errors.New("TUN 服务仅支持 macOS")
}
func RunInstalledService(context.Context) error { return errors.New("TUN 服务仅支持 macOS") }
func StatusService(context.Context) (ServiceStatus, error) {
	return ServiceStatus{}, errors.New("TUN 服务仅支持 macOS")
}
func UninstallService(context.Context, string) error { return errors.New("TUN 服务仅支持 macOS") }
func UninstallServiceRoot() error                    { return errors.New("TUN 服务仅支持 macOS") }
