//go:build darwin && !ios && cgo

package autostart

import "testing"

func TestUnbundledExecutableCannotRegisterLoginItem(t *testing.T) {
	service := Service{}
	if state, err := service.Status(); state != Unavailable || err == nil {
		t.Fatal("test executable was treated as a registerable app", state, err)
	}
	if state, err := service.SetEnabled(true); state != Unavailable || err == nil {
		t.Fatal("test executable was allowed to register the terminal/test runner", state, err)
	}
}
