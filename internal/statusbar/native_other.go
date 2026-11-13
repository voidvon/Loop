//go:build !darwin || ios || !cgo

package statusbar

import (
	"local/loop/internal/app"
)

func Run(_ State, _ <-chan State, stop <-chan struct{}, _ func(app.Operation)) error {
	<-stop
	return nil
}
