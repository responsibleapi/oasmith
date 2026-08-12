package cli_test

import (
	"strings"
	"testing"

	"github.com/meoyawn/oascribe/internal/cli"
)

func TestParseRequiresFlags(t *testing.T) {
	t.Parallel()

	_, err := cli.Parse([]string{"--openapi", "doc.yaml", "--mode", "types", "--lang", "go"})
	if err == nil {
		t.Fatal("Parse without --out succeeded")
	}
	if !strings.Contains(err.Error(), "usage: oascribe") {
		t.Fatalf("Parse error = %q, want usage", err.Error())
	}
}

func TestParseRejectsInvalidModeLangPair(t *testing.T) {
	t.Parallel()

	_, err := cli.Parse([]string{
		"--openapi", "doc.yaml",
		"--mode", "types",
		"--lang", "typescript",
		"--out", "out",
	})
	if err == nil {
		t.Fatal("Parse invalid mode/lang succeeded")
	}
	if !strings.Contains(err.Error(), "valid pairs are types/go, client/go, and client/typescript") {
		t.Fatalf("Parse error = %q, want valid pair message", err.Error())
	}
}

func TestParseAcceptsGoClient(t *testing.T) {
	t.Parallel()

	_, err := cli.Parse([]string{
		"--openapi", "doc.yaml",
		"--mode", "client",
		"--lang", "go",
		"--out", "out",
	})
	if err != nil {
		t.Fatalf("Parse client/go: %v", err)
	}
}
