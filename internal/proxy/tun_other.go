//go:build !darwin

package proxy

import (
	"context"
	"errors"
	"io"
)

type tunSession struct{ done chan struct{} }

func TUNAvailable() bool { return false }
func prepareTUN(string, string) (*tunSession, error) {
	return nil, errors.New("当前仅支持 macOS TUN")
}
func (*tunSession) connect(context.Context, []byte, io.Writer) error {
	return errors.New("当前仅支持 macOS TUN")
}
func (*tunSession) stop()            {}
func (*tunSession) cleanup() error   { return nil }
func (*tunSession) exitError() error { return nil }
