package main

import (
	"fmt"
	"os"

	"local/loop/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
