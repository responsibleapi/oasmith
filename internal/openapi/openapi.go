package openapi

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document is the parsed OpenAPI document shape used by the generator.
type Document struct {
	OpenAPI    string              `yaml:"openapi"`
	Info       Info                `yaml:"info"`
	Servers    []Server            `yaml:"servers"`
	Paths      map[string]PathItem `yaml:"paths"`
	Components Components          `yaml:"components"`
}

// Server describes one OpenAPI server URL.
type Server struct {
	URL string `yaml:"url"`
}

// Info describes OpenAPI document metadata.
type Info struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

// Components groups reusable OpenAPI components.
type Components struct {
	Schemas         map[string]*Schema        `yaml:"schemas"`
	Responses       map[string]*Response      `yaml:"responses"`
	Parameters      map[string]*Parameter     `yaml:"parameters"`
	SecuritySchemes map[string]SecurityScheme `yaml:"securitySchemes"`
}

// SecurityScheme describes an OpenAPI security scheme.
type SecurityScheme struct {
	Type   string `yaml:"type"`
	Scheme string `yaml:"scheme"`
}

// PathItem groups operations registered for a path.
type PathItem struct {
	Parameters []Parameter `yaml:"parameters"`
	Get        *Operation  `yaml:"get"`
	Head       *Operation  `yaml:"head"`
	Post       *Operation  `yaml:"post"`
	Put        *Operation  `yaml:"put"`
	Patch      *Operation  `yaml:"patch"`
	Delete     *Operation  `yaml:"delete"`
}

// Operation describes one OpenAPI operation.
type Operation struct {
	OperationID string                `yaml:"operationId"`
	Deprecated  bool                  `yaml:"deprecated"`
	Servers     []Server              `yaml:"servers"`
	Parameters  []Parameter           `yaml:"parameters"`
	RequestBody *RequestBody          `yaml:"requestBody"`
	Responses   map[string]Response   `yaml:"responses"`
	Security    []map[string][]string `yaml:"security"`
}

// Parameter describes an OpenAPI operation parameter.
type Parameter struct {
	Ref      string  `yaml:"$ref"`
	Name     string  `yaml:"name"`
	In       string  `yaml:"in"`
	Required bool    `yaml:"required"`
	Schema   *Schema `yaml:"schema"`
}

// RequestBody describes an OpenAPI request body.
type RequestBody struct {
	Required bool                 `yaml:"required"`
	Content  map[string]MediaType `yaml:"content"`
}

// Response describes an OpenAPI response or response reference.
type Response struct {
	Ref         string               `yaml:"$ref"`
	Description string               `yaml:"description"`
	Content     map[string]MediaType `yaml:"content"`
}

// MediaType describes schema metadata for a response or request media type.
type MediaType struct {
	Schema         *Schema    `yaml:"schema"`
	ItemSchema     *Schema    `yaml:"itemSchema"`
	PrefixEncoding []Encoding `yaml:"prefixEncoding"`
}

// Encoding describes the wire encoding for one multipart position.
type Encoding struct {
	ContentType string `yaml:"contentType"`
}

// Schema describes the OpenAPI schema subset supported by the generator.
type Schema struct {
	Ref              string             `yaml:"$ref"`
	Title            string             `yaml:"title"`
	Type             Type               `yaml:"type"`
	Format           string             `yaml:"format"`
	Description      string             `yaml:"description"`
	Enum             []string           `yaml:"enum"`
	Const            any                `yaml:"const"`
	Properties       map[string]*Schema `yaml:"properties"`
	Required         []string           `yaml:"required"`
	Items            *Schema            `yaml:"items"`
	PrefixItems      []*Schema          `yaml:"prefixItems"`
	MinItems         *int               `yaml:"minItems"`
	MaxItems         *int               `yaml:"maxItems"`
	OneOf            []*Schema          `yaml:"oneOf"`
	Discriminator    *Discriminator     `yaml:"discriminator"`
	ContentMediaType string             `yaml:"contentMediaType"`
	ContentSchema    *Schema            `yaml:"contentSchema"`
}

// Type holds one or more OpenAPI schema type names.
type Type []string

// UnmarshalYAML accepts scalar and array OpenAPI type declarations.
func (t *Type) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*t = []string{value.Value}
	case yaml.SequenceNode:
		for _, item := range value.Content {
			*t = append(*t, item.Value)
		}
	}
	return nil
}

// Has reports whether the type list contains name.
func (t Type) Has(name string) bool {
	return slices.Contains(t, name)
}

// First returns the first type name, if any.
func (t Type) First() string {
	if len(t) == 0 {
		return ""
	}
	return t[0]
}

// Discriminator describes OpenAPI oneOf discriminator metadata.
type Discriminator struct {
	PropertyName string            `yaml:"propertyName"`
	Mapping      map[string]string `yaml:"mapping"`
}

// OperationRoute combines an operation with its path and HTTP method.
type OperationRoute struct {
	Path      string
	Method    string
	Operation *Operation
}

// ParseFile reads and parses an OpenAPI YAML or JSON document.
func ParseFile(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read openapi document %q: %w", path, err)
	}
	return Parse(raw)
}

// Parse parses an OpenAPI YAML or JSON document from raw bytes.
//
// OpenAPI documents may use either YAML or JSON syntax. YAML 1.2 is a
// superset of JSON, so the YAML decoder accepts both representations while
// preserving the same document model and validation behavior.
func Parse(raw []byte) (*Document, error) {
	var doc Document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	if doc.OpenAPI != "3.1.0" && doc.OpenAPI != "3.2.0" {
		return nil, fmt.Errorf("unsupported OpenAPI version %q; expected 3.1.0 or 3.2.0", doc.OpenAPI)
	}
	if doc.Components.Schemas == nil {
		doc.Components.Schemas = map[string]*Schema{}
	}
	if doc.Components.Responses == nil {
		doc.Components.Responses = map[string]*Response{}
	}
	if doc.Components.Parameters == nil {
		doc.Components.Parameters = map[string]*Parameter{}
	}
	if doc.Paths == nil {
		doc.Paths = map[string]PathItem{}
	}
	return &doc, nil
}

// SchemaNames returns schema component names in stable order.
func (doc *Document) SchemaNames() []string {
	names := make([]string, 0, len(doc.Components.Schemas))
	for name := range doc.Components.Schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ComponentSchemasUseDuration reports whether any reusable schema uses ISO 8601 durations.
func (doc *Document) ComponentSchemasUseDuration() bool {
	for _, schema := range doc.Components.Schemas {
		if schema.UsesDuration() {
			return true
		}
	}
	return false
}

// UsesDuration reports whether the document uses ISO 8601 durations.
func (doc *Document) UsesDuration() bool {
	if doc.ComponentSchemasUseDuration() {
		return true
	}
	for _, route := range doc.Operations() {
		for _, param := range route.Operation.Parameters {
			if param.Schema.UsesDuration() {
				return true
			}
		}
		if route.Operation.JSONRequestSchema().UsesDuration() || route.Operation.JSONResponseSchema().UsesDuration() {
			return true
		}
		for _, response := range route.Operation.Responses {
			resolved := doc.ResolveResponse(response)
			for _, mediaType := range resolved.Content {
				if mediaType.Schema.UsesDuration() || mediaType.ItemSchema.UsesDuration() {
					return true
				}
			}
		}
	}
	return false
}

// Operations returns named operations in stable path and method order.
func (doc *Document) Operations() []OperationRoute {
	var routes []OperationRoute
	paths := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		item := doc.Paths[path]
		for _, route := range []OperationRoute{
			{Path: path, Method: "GET", Operation: item.Get},
			{Path: path, Method: "HEAD", Operation: item.Head},
			{Path: path, Method: "POST", Operation: item.Post},
			{Path: path, Method: "PUT", Operation: item.Put},
			{Path: path, Method: "PATCH", Operation: item.Patch},
			{Path: path, Method: "DELETE", Operation: item.Delete},
		} {
			if route.Operation != nil && route.Operation.OperationID != "" {
				operation := *route.Operation
				operation.Parameters = doc.mergeParameters(item.Parameters, operation.Parameters)
				route.Operation = &operation
				routes = append(routes, route)
			}
		}
	}
	return routes
}

// RefName extracts the schema component name from a schema reference.
func RefName(ref string) string {
	const prefix = "#/components/schemas/"
	return strings.TrimPrefix(ref, prefix)
}

// ComponentResponseName extracts the response component name from a response reference.
func ComponentResponseName(ref string) string {
	const prefix = "#/components/responses/"
	return strings.TrimPrefix(ref, prefix)
}

// ComponentParameterName extracts the parameter component name from a reference.
func ComponentParameterName(ref string) string {
	const prefix = "#/components/parameters/"
	return strings.TrimPrefix(ref, prefix)
}

// ResolveResponse returns the concrete response for a response reference.
func (doc *Document) ResolveResponse(response Response) Response {
	if response.Ref == "" {
		return response
	}
	resolved := doc.Components.Responses[ComponentResponseName(response.Ref)]
	if resolved == nil {
		return response
	}
	return *resolved
}

// SSEPayloadSchema returns the decoded JSON data schema represented by a named
// OpenAPI 3.2 server-sent event item schema.
func (doc *Document) SSEPayloadSchema(componentName string) *Schema {
	for _, route := range doc.Operations() {
		for _, response := range route.Operation.Responses {
			resolved := doc.ResolveResponse(response)
			media, ok := resolved.Content["text/event-stream"]
			if !ok || media.ItemSchema == nil ||
				RefName(media.ItemSchema.Ref) != componentName {
				continue
			}
			return doc.ssePayloadSchema(media.ItemSchema)
		}
	}
	return nil
}

func jsonSSEDataSchema(schema *Schema) *Schema {
	data := schema.Properties["data"]
	if data == nil || data.ContentMediaType != "application/json" {
		return nil
	}
	return data.ContentSchema
}

func stringConst(schema *Schema) (string, bool) {
	if schema == nil || !schema.Type.Has("string") {
		return "", false
	}
	value, ok := schema.Const.(string)
	return value, ok
}

// IsRef reports whether schema is a schema reference.
func (schema *Schema) IsRef() bool {
	return schema != nil && schema.Ref != ""
}

// IsObject reports whether schema includes the object type.
func (schema *Schema) IsObject() bool {
	return schema != nil && schema.Type.Has("object")
}

// IsArray reports whether schema includes the array type.
func (schema *Schema) IsArray() bool {
	return schema != nil && schema.Type.Has("array")
}

// IsDuration reports whether schema is an ISO 8601 duration string.
func (schema *Schema) IsDuration() bool {
	return schema != nil && schema.Type.Has("string") && schema.Format == "duration"
}

// IsOneOf reports whether schema is a oneOf union.
func (schema *Schema) IsOneOf() bool {
	return schema != nil && len(schema.OneOf) > 0
}

// RequiredSet returns the schema's required property names as a lookup set.
func (schema *Schema) RequiredSet() map[string]bool {
	required := map[string]bool{}
	if schema == nil {
		return required
	}
	for _, name := range schema.Required {
		required[name] = true
	}
	return required
}

// SortedPropertyNames returns the schema's property names in stable order.
func (schema *Schema) SortedPropertyNames() []string {
	if schema == nil {
		return nil
	}
	keys := make([]string, 0, len(schema.Properties))
	for key := range schema.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// UsesDuration reports whether schema or any nested schema uses ISO 8601 durations.
func (schema *Schema) UsesDuration() bool {
	if schema == nil {
		return false
	}
	if schema.IsDuration() {
		return true
	}
	if schema.Items.UsesDuration() {
		return true
	}
	if schema.ContentSchema.UsesDuration() {
		return true
	}
	for _, prop := range schema.Properties {
		if prop.UsesDuration() {
			return true
		}
	}
	for _, item := range schema.OneOf {
		if item.UsesDuration() {
			return true
		}
	}
	return false
}

// OperationHasSSEResponse reports whether a successful GET response is an SSE
// stream. It remains as the GET-default helper used by callers that do not
// carry route metadata.
func (doc *Document) OperationHasSSEResponse(operation *Operation) bool {
	return doc.OperationHasSSEResponseMethod("GET", operation)
}

// OperationHasSSEResponseMethod reports whether a successful response on the
// supplied HTTP method is an SSE stream. Only GET is replay-safe.
func (doc *Document) OperationHasSSEResponseMethod(method string, operation *Operation) bool {
	if method != "GET" {
		return false
	}
	if operation == nil {
		return false
	}
	for status, response := range operation.Responses {
		if !successResponseStatus(status) {
			continue
		}
		resolved := doc.ResolveResponse(response)
		if _, ok := resolved.Content["text/event-stream"]; ok {
			return true
		}
	}
	return false
}

func (doc *Document) mergeParameters(pathParameters []Parameter, operationParameters []Parameter) []Parameter {
	merged := make([]Parameter, 0, len(pathParameters)+len(operationParameters))
	indices := make(map[string]int, len(pathParameters)+len(operationParameters))
	appendParameter := func(parameter Parameter) {
		resolved, ok := doc.resolveParameter(parameter)
		if !ok || resolved.Name == "" || resolved.In == "" {
			return
		}
		key := resolved.In + "\x00" + resolved.Name
		if index, exists := indices[key]; exists {
			merged[index] = resolved
			return
		}
		indices[key] = len(merged)
		merged = append(merged, resolved)
	}
	for _, parameter := range pathParameters {
		appendParameter(parameter)
	}
	for _, parameter := range operationParameters {
		appendParameter(parameter)
	}
	return merged
}

func (doc *Document) resolveParameter(parameter Parameter) (Parameter, bool) {
	if parameter.Ref == "" {
		return parameter, true
	}
	resolved := doc.Components.Parameters[ComponentParameterName(parameter.Ref)]
	if resolved == nil {
		return Parameter{}, false
	}
	return *resolved, true
}

func (doc *Document) ssePayloadSchema(itemSchema *Schema) *Schema {
	wireSchema := doc.resolveSchema(itemSchema)
	if wireSchema == nil {
		return nil
	}
	if payload := jsonSSEDataSchema(wireSchema); payload != nil {
		return payload
	}
	if len(wireSchema.OneOf) == 0 {
		return nil
	}
	payloads := make([]*Schema, 0, len(wireSchema.OneOf))
	mapping := make(map[string]string, len(wireSchema.OneOf))
	discriminatorProperty := ""
	for _, wireVariant := range wireSchema.OneOf {
		variant := doc.resolveSchema(wireVariant)
		if variant == nil {
			return nil
		}
		eventName, ok := stringConst(variant.Properties["event"])
		payload := jsonSSEDataSchema(variant)
		if !ok || payload == nil || payload.Ref == "" {
			return nil
		}
		property, ok := doc.discriminatorPropertyForValue(payload, eventName)
		if !ok || discriminatorProperty != "" && discriminatorProperty != property {
			return nil
		}
		discriminatorProperty = property
		payloads = append(payloads, payload)
		mapping[eventName] = payload.Ref
	}
	return &Schema{
		OneOf: payloads,
		Discriminator: &Discriminator{
			PropertyName: discriminatorProperty,
			Mapping:      mapping,
		},
	}
}

func (doc *Document) resolveSchema(schema *Schema) *Schema {
	if schema == nil || schema.Ref == "" {
		return schema
	}
	return doc.Components.Schemas[RefName(schema.Ref)]
}

func (doc *Document) discriminatorPropertyForValue(schema *Schema, value string) (string, bool) {
	resolved := doc.resolveSchema(schema)
	if resolved == nil {
		return "", false
	}
	if len(resolved.OneOf) == 0 {
		for _, property := range resolved.SortedPropertyNames() {
			constant, ok := stringConst(resolved.Properties[property])
			if ok && constant == value {
				return property, true
			}
		}
		return "", false
	}
	property := ""
	for _, variant := range resolved.OneOf {
		candidate, ok := doc.discriminatorPropertyForValue(variant, value)
		if !ok || property != "" && property != candidate {
			return "", false
		}
		property = candidate
	}
	return property, property != ""
}

func successResponseStatus(status string) bool {
	return len(status) == 3 && status[0] == '2' &&
		status[1] >= '0' && status[1] <= '9' &&
		status[2] >= '0' && status[2] <= '9'
}

// JSONResponseSchema returns the first successful JSON-like response schema.
func (operation *Operation) JSONResponseSchema() *Schema {
	for _, code := range []string{"200", "201", "202"} {
		response, ok := operation.Responses[code]
		if !ok {
			continue
		}
		if mt, ok := response.Content["application/json"]; ok {
			return mt.Schema
		}
		if mt, ok := response.Content["application/rss+xml"]; ok {
			return mt.Schema
		}
	}
	return nil
}

// JSONRequestSchema returns the JSON request body schema, if present.
func (operation *Operation) JSONRequestSchema() *Schema {
	if operation.RequestBody == nil {
		return nil
	}
	if mt, ok := operation.RequestBody.Content["application/json"]; ok {
		return mt.Schema
	}
	return nil
}

// RawRequestBodyMediaType returns a deterministic non-JSON request media type.
func (operation *Operation) RawRequestBodyMediaType() (string, bool) {
	if operation.RequestBody == nil {
		return "", false
	}
	if _, ok := operation.RequestBody.Content["application/json"]; ok {
		return "", false
	}
	if _, ok := operation.RequestBody.Content["application/octet-stream"]; ok {
		return "application/octet-stream", true
	}
	mediaTypes := make([]string, 0, len(operation.RequestBody.Content))
	for mediaType := range operation.RequestBody.Content {
		mediaTypes = append(mediaTypes, mediaType)
	}
	sort.Strings(mediaTypes)
	if len(mediaTypes) == 0 {
		return "", false
	}
	return mediaTypes[0], true
}
