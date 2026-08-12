// Package cli parses arguments and dispatches OASmith modes.
package cli

import (
	"flag"
	"fmt"

	"github.com/meoyawn/oasmith/internal/goemit"
	"github.com/meoyawn/oasmith/internal/openapi"
	"github.com/meoyawn/oasmith/internal/tsemit"
)

// Options holds parsed OASmith command-line flags.
type Options struct {
	OpenAPI string
	Mode    string
	Lang    string
	Out     string
}

// Run parses args, loads the OpenAPI document, and emits requested outputs.
func Run(args []string) error {
	opts, err := Parse(args)
	if err != nil {
		return err
	}
	doc, err := openapi.ParseFile(opts.OpenAPI)
	if err != nil {
		return err
	}
	switch {
	case opts.Mode == "types" && opts.Lang == "go":
		return goemit.Emit(doc, goemit.Options{OutDir: opts.Out, SourcePath: opts.OpenAPI})
	case opts.Mode == "client" && opts.Lang == "go":
		return goemit.EmitClient(doc, goemit.Options{OutDir: opts.Out, SourcePath: opts.OpenAPI})
	case opts.Mode == "client" && opts.Lang == "typescript":
		return tsemit.Emit(doc, tsemit.Options{OutDir: opts.Out})
	default:
		return fmt.Errorf("unsupported --mode/--lang pair %q/%q; valid pairs are types/go, client/go, and client/typescript", opts.Mode, opts.Lang)
	}
}

// Parse converts command-line arguments into generator options.
func Parse(args []string) (Options, error) {
	var opts Options
	fs := flag.NewFlagSet("oasmith", flag.ContinueOnError)
	fs.StringVar(&opts.OpenAPI, "openapi", "", "OpenAPI YAML or JSON document")
	fs.StringVar(&opts.Mode, "mode", "", "generation mode")
	fs.StringVar(&opts.Lang, "lang", "", "output language")
	fs.StringVar(&opts.Out, "out", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return Options{}, fmt.Errorf("usage: oasmith --openapi <openapidoc> --mode <types|client> --lang <go|typescript> --out <dir>")
	}
	if opts.OpenAPI == "" || opts.Mode == "" || opts.Lang == "" || opts.Out == "" {
		return Options{}, fmt.Errorf("usage: oasmith --openapi <openapidoc> --mode <types|client> --lang <go|typescript> --out <dir>")
	}
	if (opts.Mode == "types" && opts.Lang == "go") ||
		(opts.Mode == "client" && (opts.Lang == "go" || opts.Lang == "typescript")) {
		return opts, nil
	}
	return Options{}, fmt.Errorf("unsupported --mode/--lang pair %q/%q; valid pairs are types/go, client/go, and client/typescript", opts.Mode, opts.Lang)
}
