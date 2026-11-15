// Package main contains the OASmith CLI entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/responsibleapi/oasmith/internal/cli"
)

const invalidInputExitCode = 2

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(invalidInputExitCode)
	}
}
