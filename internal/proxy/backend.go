package proxy

import "context"

// Backend keeps the UI independent of the platform's proxy implementation.
// Android needs its own VpnService/libbox backend; it cannot use Desktop.
type Backend interface {
	Start(context.Context) error
	Stop() error
}
