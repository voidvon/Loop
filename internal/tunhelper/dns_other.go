//go:build !darwin

package tunhelper

import "errors"

func startDNS(map[string]any) (func(), <-chan struct{}, error) {
	return nil, nil, errors.New("当前仅支持 macOS TUN DNS")
}
