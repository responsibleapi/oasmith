// Package main contains the OASmith CLI entrypoint.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/responsibleapi/oasmith/internal/cli"
)

const invalidInputExitCode = 2

// version is stamped by release builds with -ldflags "-X main.version=<semver>".
var version = ""

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(buildVersion())
		return
	}
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(invalidInputExitCode)
	}
}

// buildVersion prefers the release stamp, then the module version recorded by
// `go install module@version`.
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
