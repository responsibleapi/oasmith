// Package main contains the OASmith CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/meoyawn/oasmith/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
