//go:build darwin && !ios && cgo

package statusbar

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void loopStatusRun(char *model);
void loopStatusUpdate(char *model);
void loopStatusStop(void);
*/
import "C"

import (
	"encoding/json"
	"sync"
	"unsafe"

	"local/loop/internal/app"
)

var callbacks struct {
	sync.RWMutex
	action func(app.Operation)
}

//export loopStatusAction
func loopStatusAction(data *C.char) {
	var operation app.Operation
	if json.Unmarshal([]byte(C.GoString(data)), &operation) != nil {
		return
	}
	callbacks.RLock()
	action := callbacks.action
	callbacks.RUnlock()
	if action != nil {
		go action(operation)
	}
}

// Run owns Cocoa's main-thread loop. State updates arrive from the controller;
// no window, font atlas, Fyne renderer, or OpenGL context is created.
func Run(initial State, updates <-chan State, stop <-chan struct{}, action func(app.Operation)) error {
	callbacks.Lock()
	callbacks.action = action
	callbacks.Unlock()
	defer func() { callbacks.Lock(); callbacks.action = nil; callbacks.Unlock() }()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		for {
			select {
			case state := <-updates:
				data := C.CString(string(encode(state)))
				C.loopStatusUpdate(data)
				C.free(unsafe.Pointer(data))
			case <-stop:
				C.loopStatusStop()
				return
			case <-finished:
				return
			}
		}
	}()
	data := C.CString(string(encode(initial)))
	defer C.free(unsafe.Pointer(data))
	C.loopStatusRun(data)
	return nil
}
