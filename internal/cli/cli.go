// Package cli parses arguments and dispatches OASmith modes.
package cli

import (
	"flag"
	"fmt"

	"github.com/responsibleapi/oasmith/internal/dartemit"
	"github.com/responsibleapi/oasmith/internal/goemit"
	"github.com/responsibleapi/oasmith/internal/openapi"
	"github.com/responsibleapi/oasmith/internal/rustemit"
	"github.com/responsibleapi/oasmith/internal/tsemit"
)

// Options holds parsed OASmith command-line flags.
type Options struct {
	OpenAPI string
	Mode    string
	Lang    string
	Out     string
	Strict  bool
}

const (
	typesMode  = "types"
	clientMode = "client"
	usage      = "usage: oasmith --openapi <openapidoc> --mode <types|client> --lang <go|typescrip" +
		"t|rust|dart> --out <dir>"
)

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
	switch opts.Lang {
	case "dart":
		return dartemit.Emit(
			doc,
			dartemit.Options{OutDir: opts.Out, Strict: opts.Strict},
			opts.Mode == clientMode,
		)
	case "rust":
		return rustemit.Emit(
			doc,
			rustemit.Options{OutDir: opts.Out, StrictJSON: opts.Strict},
			opts.Mode == clientMode,
		)
	case "go":
		if opts.Mode == typesMode {
			return goemit.Emit(
				doc,
				goemit.Options{OutDir: opts.Out, SourcePath: opts.OpenAPI, Strict: opts.Strict},
			)
		}
		return goemit.EmitClient(
			doc,
			goemit.Options{OutDir: opts.Out, SourcePath: opts.OpenAPI, Strict: opts.Strict},
		)
	case "typescript":
		return tsemit.Emit(
			doc,
			tsemit.Options{
				OutDir:    opts.Out,
				Strict:    opts.Strict,
				TypesOnly: opts.Mode == typesMode,
			},
		)
	default:
		return opts.unsupported()
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
	fs.BoolVar(
		&opts.Strict,
		"strict",
		false,
		"reject unknown JSON object fields where additionalProperties is false (for configuration)",
	)
	if err := fs.Parse(args); err != nil {
		return Options{}, fmt.Errorf("%s: %w", usage, err)
	}
	if err := opts.validate(); err != nil {
		return Options{}, err
	}
	return opts, nil
}

func (opts Options) validate() error {
	if opts.OpenAPI == "" || opts.Mode == "" || opts.Lang == "" || opts.Out == "" {
		return fmt.Errorf(
			"incomplete generation options (mode=%q, lang=%q); %s",
			opts.Mode,
			opts.Lang,
			usage,
		)
	}
	if opts.Mode != typesMode && opts.Mode != clientMode {
		return opts.unsupported()
	}
	switch opts.Lang {
	case "rust", "dart", "go", "typescript":
		return nil
	default:
		return opts.unsupported()
	}
}

func (opts Options) unsupported() error {
	return fmt.Errorf(
		"unsupported --mode/--lang pair %q/%q; valid modes are types and client; "+
			"valid languages are go, typescript, rust, and dart",
		opts.Mode,
		opts.Lang,
	)
}
