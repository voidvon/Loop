package main

import (
	"log"
	"os"
	"runtime"

	"local/loop/internal/control"
)

func main() {
	runtime.LockOSThread()
	if err := control.RunDaemon(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
