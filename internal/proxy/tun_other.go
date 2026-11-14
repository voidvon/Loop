//go:build !darwin

package proxy

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

type tunSession struct{}

func TUNAvailable() bool { return false }
func prepareTUN(string, string) (*tunSession, *exec.Cmd, error) {
	return nil, nil, errors.New("当前仅支持 macOS TUN")
}
func (*tunSession) connect(context.Context, <-chan struct{}, string, []byte, io.Writer) error {
	return errors.New("当前仅支持 macOS TUN")
}
func (*tunSession) stop()                 {}
func (*tunSession) cleanup()              {}
func (*tunSession) exitError() error      { return nil }
func (*tunSession) helperConnected() bool { return false }
