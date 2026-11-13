// This entry preserves go run .; the canonical command is ./cmd/miniproxy.
package main

import (
	"log"
	"os"

	"local/miniproxy/internal/cli"
	"local/miniproxy/internal/desktop"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, desktop.Run); err != nil {
		log.Fatal(err)
	}
}
