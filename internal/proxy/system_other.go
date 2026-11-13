//go:build !darwin

package proxy

import (
	"errors"
	"os"
)

func acquireLock(string) (*os.File, error) {
	return nil, errors.New("此版本仅实现 macOS 连接后端")
}

type systemProxy struct{ journal string }

func (*systemProxy) enable(string) error { return errors.New("此版本仅实现 macOS 系统代理") }
func (*systemProxy) restore() error      { return nil }
