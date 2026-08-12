// Package openapi parses the OpenAPI subset used by the generator.
package openapi

import (
	"strings"
	"unicode"
)

// ExportName converts an OpenAPI name into an exported Go identifier.
func ExportName(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	for i, part := range parts {
		parts[i] = upperFirst(part)
	}
	exported := strings.Join(parts, "")
	if exported != "" && unicode.IsDigit([]rune(exported)[0]) {
		return "X" + exported
	}
	return exported
}

// LowerCamel converts an OpenAPI name into a lower-camel identifier.
func LowerCamel(name string) string {
	exported := ExportName(name)
	if exported == "" {
		return ""
	}
	return strings.ToLower(exported[:1]) + exported[1:]
}

// ConstName converts an enum or const value into an uppercase Go constant name.
func ConstName(value string) string {
	var out []rune
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			out = append(out, unicode.ToUpper(r))
		default:
			out = append(out, '_')
		}
	}
	return strings.Trim(string(out), "_")
}

func upperFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
