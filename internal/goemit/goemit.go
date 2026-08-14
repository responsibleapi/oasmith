// Package goemit emits Go models and HTTP clients from OpenAPI documents.
package goemit

import (
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/responsibleapi/oasmith/internal/emit"
	"github.com/responsibleapi/oasmith/internal/openapi"
)

// Options configures Go model emission.
type Options struct {
	OutDir     string
	SourcePath string
}

type emitter struct{}

//go:embed templates/*.gotmpl
var templateFS embed.FS

// Emit writes Go model files for the supplied OpenAPI document.
func Emit(doc *openapi.Document, opts Options) error {
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return fmt.Errorf("create go output directory %q: %w", opts.OutDir, err)
	}
	raw, err := executeTemplate("models.go", goModelsTemplateData(doc, opts.SourcePath))
	if err != nil {
		return err
	}
	path := filepath.Join(opts.OutDir, "models.go")
	source, err := format.Source(raw)
	if err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	if err := os.WriteFile(path, source, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// EmitClient writes Go models and an HTTP client for the supplied OpenAPI document.
func EmitClient(doc *openapi.Document, opts Options) error {
	if err := Emit(doc, opts); err != nil {
		return err
	}
	raw, err := executeTemplate("client.go", goClientTemplateData(doc, opts.SourcePath))
	if err != nil {
		return err
	}
	path := filepath.Join(opts.OutDir, "client.go")
	source, err := format.Source(raw)
	if err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	if err := os.WriteFile(path, source, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func packageName(doc *openapi.Document, sourcePath string) string {
	base := filepath.Base(sourcePath)
	switch {
	case strings.Contains(base, "private"):
		return "privateapi"
	case strings.Contains(base, "public"):
		return "publicapi"
	case strings.Contains(base, "worker"):
		return "workerconfig"
	case strings.Contains(base, "config"):
		return "config"
	case strings.Contains(strings.ToLower(doc.Info.Title), "worker"):
		return "workerconfig"
	default:
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		name := strings.ToLower(openapi.ExportName(stem))
		if name == "" {
			return "generated"
		}
		return name
	}
}

type goModelsData struct {
	PackageName          string
	Imports              []string
	Schemas              []goSchemaData
	NeedsDurationHelpers bool
}

type goSchemaData struct {
	Alias  *goAliasData
	Enum   *goEnumData
	Object *goObjectData
	OneOf  *goOneOfData
}

type goAliasData struct {
	TypeName   string
	GoType     string
	HasConst   bool
	ConstName  string
	ConstValue string
}

type goEnumData struct {
	TypeName string
	Values   []goEnumValueData
}

type goEnumValueData struct {
	ConstName string
	Value     string
}

type goObjectData struct {
	TypeName string
	Fields   []goFieldData
}

type goFieldData struct {
	Name     string
	Type     string
	JSONName string
	JSONOmit string
}

type goOneOfData struct {
	TypeName              string
	Refs                  []goOneOfRefData
	HasDiscriminator      bool
	DiscriminatorProperty string
	Mappings              []goOneOfMappingData
}

type goOneOfRefData struct {
	FieldName string
	TypeName  string
}

type goOneOfMappingData struct {
	Value     string
	FieldName string
}

func goModelsTemplateData(doc *openapi.Document, sourcePath string) goModelsData {
	e := emitter{}
	data := goModelsData{
		PackageName:          packageName(doc, sourcePath),
		Imports:              goImports(doc),
		NeedsDurationHelpers: needsDurationHelpers(doc),
	}
	for _, name := range doc.SchemaNames() {
		schema := doc.Components.Schemas[name]
		if payloadSchema := doc.SSEPayloadSchema(name); payloadSchema != nil {
			schema = payloadSchema
		}
		data.Schemas = append(data.Schemas, e.schemaTemplateData(name, schema))
	}
	return data
}

func (e *emitter) schemaTemplateData(name string, schema *openapi.Schema) goSchemaData {
	switch {
	case schema.IsOneOf():
		return goSchemaData{OneOf: e.oneOfTemplateData(name, schema)}
	case schema.IsObject():
		return goSchemaData{Object: e.objectTemplateData(name, schema)}
	case len(schema.Enum) > 0:
		return goSchemaData{Enum: e.enumTemplateData(name, schema)}
	default:
		return goSchemaData{Alias: e.aliasTemplateData(name, schema)}
	}
}

func (e *emitter) aliasTemplateData(name string, schema *openapi.Schema) *goAliasData {
	data := goAliasData{
		TypeName: openapi.ExportName(name),
		GoType:   e.goType(schema),
	}
	if schema.Const != nil {
		data.HasConst = true
		data.ConstName = openapi.ConstName(fmt.Sprint(schema.Const))
		data.ConstValue = fmt.Sprint(schema.Const)
	}
	return &data
}

func (*emitter) enumTemplateData(name string, schema *openapi.Schema) *goEnumData {
	typeName := openapi.ExportName(name)
	data := goEnumData{TypeName: typeName}
	for _, value := range schema.Enum {
		data.Values = append(data.Values, goEnumValueData{
			ConstName: typeName + enumValueName(value),
			Value:     value,
		})
	}
	return &data
}

func enumValueName(value string) string {
	name := openapi.ConstName(value)
	if name == "" {
		return "Value"
	}
	return openapi.ExportName(strings.ToLower(name))
}

func (e *emitter) objectTemplateData(name string, schema *openapi.Schema) *goObjectData {
	typeName := openapi.ExportName(name)
	required := schema.RequiredSet()
	keys := schema.SortedPropertyNames()
	data := goObjectData{
		TypeName: typeName,
	}
	for _, propName := range keys {
		prop := schema.Properties[propName]
		goType := e.goType(prop)
		if !required[propName] {
			goType = optionalType(goType)
		}
		data.Fields = append(data.Fields, goFieldData{
			Name:     openapi.ExportName(propName),
			Type:     goType,
			JSONName: propName,
			JSONOmit: jsonOmitEmpty(required[propName]),
		})
	}
	return &data
}

func (*emitter) oneOfTemplateData(name string, schema *openapi.Schema) *goOneOfData {
	data := goOneOfData{TypeName: openapi.ExportName(name)}
	for _, item := range schema.OneOf {
		if item.Ref == "" {
			continue
		}
		refName := openapi.ExportName(openapi.RefName(item.Ref))
		data.Refs = append(data.Refs, goOneOfRefData{
			FieldName: refName,
			TypeName:  refName,
		})
	}
	if schema.Discriminator == nil {
		return &data
	}
	data.HasDiscriminator = true
	data.DiscriminatorProperty = schema.Discriminator.PropertyName
	for _, mapping := range sortedMapping(schema.Discriminator.Mapping) {
		refName := openapi.RefName(schema.Discriminator.Mapping[mapping])
		data.Mappings = append(data.Mappings, goOneOfMappingData{
			Value:     mapping,
			FieldName: openapi.ExportName(refName),
		})
	}
	return &data
}

func executeTemplate(name string, data any) ([]byte, error) {
	return emit.ExecuteTemplate("go", templateFS, name, nil, data)
}

func (e *emitter) goType(schema *openapi.Schema) string {
	if schema == nil {
		return "any"
	}
	if schema.Ref != "" {
		return openapi.ExportName(openapi.RefName(schema.Ref))
	}
	if schema.IsArray() {
		return "[]" + e.goType(schema.Items)
	}
	switch schema.Format {
	case "int64":
		return "int64"
	case "int32", "uint32":
		return "int32"
	case "duration":
		return "ISODuration"
	}
	switch schema.Type.First() {
	case "boolean":
		return "bool"
	case "integer":
		return "int32"
	case "number":
		return "float64"
	case "object":
		return "map[string]any"
	default:
		return "string"
	}
}

func optionalType(goType string) string {
	if strings.HasPrefix(goType, "*") {
		return goType
	}
	return "*" + goType
}

func jsonOmitEmpty(required bool) string {
	if required {
		return ""
	}
	return ",omitempty"
}

func needsJSONHelpers(doc *openapi.Document) bool {
	if needsDurationHelpers(doc) {
		return true
	}
	for _, schema := range doc.Components.Schemas {
		if schema.IsOneOf() {
			return true
		}
	}
	return false
}

func goImports(doc *openapi.Document) []string {
	imports := map[string]bool{}
	if needsJSONHelpers(doc) {
		imports["encoding/json"] = true
		imports["fmt"] = true
	}
	if needsDurationHelpers(doc) {
		imports["fmt"] = true
		imports["strconv"] = true
		imports["time"] = true
	}
	paths := make([]string, 0, len(imports))
	for path := range imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func needsDurationHelpers(doc *openapi.Document) bool {
	return doc.ComponentSchemasUseDuration()
}

func sortedMapping(mapping map[string]string) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
