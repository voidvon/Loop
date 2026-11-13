//go:build !darwin || ios || !cgo

package autostart

import "errors"

func (Service) Status() (Status, error) {
	return Unavailable, errors.New("当前运行环境不支持 macOS 登录自启")
}

func (s Service) SetEnabled(bool) (Status, error) { return s.Status() }
func (Service) OpenSettings()                     {}
