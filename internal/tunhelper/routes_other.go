//go:build !darwin

package tunhelper

import "errors"

func CheckRouteConflict() error { return errors.New("当前仅支持 macOS TUN") }
