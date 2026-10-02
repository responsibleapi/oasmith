// Package dartemit emits typed Dart models and an injectable HTTP client.
package dartemit

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/responsibleapi/oasmith/internal/clientgen"
	"github.com/responsibleapi/oasmith/internal/openapi"
)

// Options configures Dart output emission.
type Options struct{ OutDir string }

type emitter struct {
	doc         *openapi.Document
	names       map[*openapi.Schema]string
	definitions map[string]*openapi.Schema
	implemented map[string][]string
	err         error
}

// Emit writes Dart models and, in client mode, operation methods for doc.
func Emit(doc *openapi.Document, opts Options, client bool) error {
	e := &emitter{doc: doc, names: map[*openapi.Schema]string{}, definitions: map[string]*openapi.Schema{}, implemented: map[string][]string{}}
	for _, name := range doc.SchemaNames() {
		if name == "string" || name == "String" {
			continue
		}
		e.register(name, doc.Components.Schemas[name])
	}
	operations, err := e.discoverOperations(client)
	if err != nil {
		return err
	}
	if err := e.discoverAll(); err != nil {
		return err
	}
	models := e.modelsSource()
	if e.err != nil {
		return e.err
	}
	return e.writeFiles(opts.OutDir, models, operations, client)
}

func (e *emitter) discoverOperations(client bool) ([]clientgen.Operation, error) {
	if !client {
		return nil, nil
	}
	operations, err := clientgen.Analyze(e.doc)
	if err != nil {
		return nil, fmt.Errorf("analyze dart client operations: %w", err)
	}
	for _, op := range operations {
		if op.RequestBody.JSON != nil {
			e.dartType(op.RequestBody.JSON.Schema, typeName(op.Route.Operation.OperationID)+"Body")
		}
		for _, param := range op.Route.Operation.Parameters {
			e.dartType(param.Schema, typeName(op.Route.Operation.OperationID)+typeName(param.Name))
		}
		for _, response := range op.Responses {
			if response.Schema != nil {
				e.dartType(response.Schema, typeName(op.Route.Operation.OperationID)+"Response")
			}
		}
	}
	return operations, e.err
}

func (e *emitter) discoverAll() error {
	// Type discovery also registers inline object and union definitions.
	for i := range 1000 {
		before := len(e.definitions)
		names := e.sortedNames()
		for _, name := range names {
			e.discover(name, e.definitions[name])
		}
		if e.err != nil {
			return e.err
		}
		if len(e.definitions) == before {
			return nil
		}
		if i == 999 {
			return fmt.Errorf("dart schema discovery did not converge")
		}
	}
	return nil
}

func (e *emitter) writeFiles(outDir, models string, operations []clientgen.Operation, client bool) error {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("create dart output directory %q: %w", outDir, err)
	}
	modelsPath := filepath.Join(outDir, "models.dart")
	if err := os.WriteFile(modelsPath, []byte(models), 0o600); err != nil {
		return fmt.Errorf("write dart models: %w", err)
	}
	if client {
		api := e.apiSource(operations)
		if e.err != nil {
			return e.err
		}
		apiPath := filepath.Join(outDir, "api.dart")
		if err := os.WriteFile(apiPath, []byte(api), 0o600); err != nil {
			return fmt.Errorf("write dart client: %w", err)
		}
	}
	return nil
}

func (e *emitter) sortedNames() []string {
	names := make([]string, 0, len(e.definitions))
	for name := range e.definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e *emitter) register(name string, schema *openapi.Schema) string {
	if schema == nil {
		return "Object"
	}
	if existing := e.names[schema]; existing != "" {
		return existing
	}
	name = typeName(name)
	if name == "" {
		e.err = fmt.Errorf("dart schema has empty type name")
		return "Object"
	}
	if prior := e.definitions[name]; prior != nil && prior != schema {
		index := 2
		for e.definitions[fmt.Sprintf("%s%d", name, index)] != nil {
			index++
		}
		name = fmt.Sprintf("%s%d", name, index)
	}
	e.names[schema] = name
	e.definitions[name] = schema
	return name
}

func (e *emitter) resolve(schema *openapi.Schema) *openapi.Schema {
	if schema == nil {
		return nil
	}
	if schema.Ref == "" {
		return schema
	}
	ref := openapi.RefName(schema.Ref)
	resolved := e.doc.Components.Schemas[ref]
	if resolved == nil {
		e.err = fmt.Errorf("dart unresolved schema reference %q", schema.Ref)
	}
	return resolved
}

func (e *emitter) dartType(schema *openapi.Schema, hint string) string {
	if schema == nil {
		e.err = fmt.Errorf("dart missing schema for %s", hint)
		return "Object?"
	}
	if schema.Ref != "" {
		name := openapi.RefName(schema.Ref)
		if e.doc.Components.Schemas[name] == nil {
			e.err = fmt.Errorf("dart unresolved schema reference %q", schema.Ref)
		}
		if name == "string" || name == "String" {
			return "String"
		}
		return typeName(name)
	}
	if len(schema.AllOf) > 0 || len(schema.AnyOf) > 0 {
		e.err = fmt.Errorf("dart schema %s uses unsupported allOf/anyOf composition", hint)
		return "Object?"
	}
	nullable := schema.Type.Has("null")
	var base string
	switch {
	case len(schema.OneOf) > 0 || len(schema.Enum) > 0 || (schema.IsObject() && len(schema.Properties) > 0):
		base = e.register(hint, schema)
	case schema.Type.Has("array"):
		if schema.Items == nil {
			e.err = fmt.Errorf("dart array %s has no items", hint)
			return "List<Object?>"
		}
		base = "List<" + e.dartType(schema.Items, hint+"Item") + ">"
	case schema.Type.Has("string"):
		base = "String"
	case schema.Type.Has("integer"):
		base = "int"
	case schema.Type.Has("number"):
		base = "double"
	case schema.Type.Has("boolean"):
		base = "bool"
	case schema.Type.Has("object"):
		base = "Map<String, Object?>"
	case schema.Type.Has("null"):
		return "Object?"
	default:
		e.err = fmt.Errorf("dart unsupported schema for %s: type %v", hint, schema.Type)
		return "Object?"
	}
	if nullable {
		return base + "?"
	}
	return base
}

func (e *emitter) discover(name string, schema *openapi.Schema) {
	if schema == nil {
		e.err = fmt.Errorf("dart schema %s is nil", name)
		return
	}
	if schema.AdditionalProperties != nil {
		if _, ok := schema.AdditionalProperties.(bool); !ok {
			e.err = fmt.Errorf("dart schema %s has unsupported typed additionalProperties", name)
			return
		}
	}
	if schema.IsOneOf() {
		for index, item := range schema.OneOf {
			variant := e.dartType(item, name+fmt.Sprintf("Variant%d", index+1))
			if item.Ref != "" {
				variant = typeName(openapi.RefName(item.Ref))
			}
			if variant == name {
				e.err = fmt.Errorf("dart union %s references itself", name)
				return
			}
			if !strings.Contains(variant, "<") && !strings.HasSuffix(variant, "?") {
				e.implemented[variant] = appendUnique(e.implemented[variant], name)
			}
		}
	}
	for _, key := range schema.SortedPropertyNames() {
		e.dartType(schema.Properties[key], name+typeName(key))
	}
	if schema.Items != nil {
		e.dartType(schema.Items, name+"Item")
	}
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func typeName(raw string) string {
	var parts []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			parts = append(parts, string(current))
			current = nil
		}
	}
	for _, ch := range raw {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			current = append(current, ch)
		} else {
			flush()
		}
	}
	flush()
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

var dartKeywords = map[string]bool{"assert": true, "break": true, "case": true, "catch": true, "class": true, "const": true, "continue": true, "default": true, "do": true, "else": true, "enum": true, "extends": true, "false": true, "final": true, "finally": true, "for": true, "if": true, "in": true, "is": true, "new": true, "null": true, "return": true, "super": true, "switch": true, "this": true, "throw": true, "true": true, "try": true, "var": true, "void": true, "while": true, "with": true, "yield": true}

func fieldName(raw string) string {
	name := typeName(raw)
	if name == "" {
		return "value"
	}
	// Preserve initialisms in public schema names only; fields are idiomatic lower camel.
	name = strings.ToLower(name[:1]) + name[1:]
	if dartKeywords[name] {
		return name + "Value"
	}
	return name
}

func quote(value string) string { return strconv.Quote(value) }
