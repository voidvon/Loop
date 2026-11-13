package main

import (
	"log"
	"os"

	"local/loop/internal/cli"
	"local/loop/internal/desktop"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, desktop.Run); err != nil {
		log.Fatal(err)
	}
}
