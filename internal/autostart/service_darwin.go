//go:build darwin && !ios && cgo

package autostart

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework ServiceManagement
#include <stdlib.h>
int loopLoginItemStatus(char **message);
int loopLoginItemSetEnabled(int enabled, char **message);
void loopLoginItemOpenSettings(void);
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (Service) Status() (Status, error) {
	var message *C.char
	state := C.loopLoginItemStatus(&message)
	return loginItemResult(state, message)
}

func (Service) SetEnabled(enabled bool) (Status, error) {
	var message *C.char
	var requested C.int
	if enabled {
		requested = 1
	}
	state := C.loopLoginItemSetEnabled(requested, &message)
	return loginItemResult(state, message)
}

func (Service) OpenSettings() { C.loopLoginItemOpenSettings() }

func loginItemResult(state C.int, message *C.char) (Status, error) {
	var err error
	if message != nil {
		err = errors.New(C.GoString(message))
		C.free(unsafe.Pointer(message))
	}
	switch state {
	case 0, 3:
		// A freshly installed main app can report notFound before its first
		// registration. It is off, but must still allow an enable attempt.
		return Disabled, err
	case 1:
		return Enabled, err
	case 2:
		return RequiresApproval, err
	default:
		if err == nil {
			err = errors.New("macOS 未找到此应用的登录项，请重新打开 Loop.app 后重试")
		}
		return Unavailable, err
	}
}
