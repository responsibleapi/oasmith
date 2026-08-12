// Package emit provides shared helpers for code emitters.
package emit

import (
	"bytes"
	"fmt"
	"io/fs"
	"text/template"
)

// ExecuteTemplate parses embedded generator templates and executes name.
func ExecuteTemplate(kind string, templateFS fs.FS, name string, funcs map[string]any, data any) ([]byte, error) {
	tpl := template.New("")
	if len(funcs) > 0 {
		tpl = tpl.Funcs(template.FuncMap(funcs))
	}
	tpl, err := tpl.ParseFS(templateFS, "templates/*.gotmpl")
	if err != nil {
		return nil, fmt.Errorf("parse %s templates: %w", kind, err)
	}
	var b bytes.Buffer
	if err := tpl.ExecuteTemplate(&b, name, data); err != nil {
		return nil, fmt.Errorf("execute %s template %q: %w", kind, name, err)
	}
	return b.Bytes(), nil
}
