// Package tsemit emits TypeScript API client code from OpenAPI documents.
package tsemit

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/responsibleapi/oasmith/internal/clientgen"
	"github.com/responsibleapi/oasmith/internal/emit"
	"github.com/responsibleapi/oasmith/internal/openapi"
)

// Options configures TypeScript client emission.
type Options struct {
	OutDir string
}

//go:embed templates/*.gotmpl oxfmt.json
var templateFS embed.FS

// Emit writes TypeScript type and API files for doc.
func Emit(doc *openapi.Document, opts Options) error {
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return fmt.Errorf("create typescript output directory %q: %w", opts.OutDir, err)
	}
	models, err := modelsSource(doc)
	if err != nil {
		return err
	}
	api, err := apiSource(doc)
	if err != nil {
		return err
	}
	files := map[string]string{
		filepath.Join(opts.OutDir, "api.ts"):   api,
		filepath.Join(opts.OutDir, "types.ts"): models,
	}
	for path, source := range files {
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := formatTypescript(files); err != nil {
		return err
	}
	return nil
}

func formatTypescript(files map[string]string) error {
	nubx, _ := exec.LookPath("nubx")
	if nubx == "" {
		return nil
	}
	// Generated clients commonly live under an ignored directory. Oxfmt 0.62
	// rejects explicitly passed files when its default ignore rules exclude them.
	// Format temporary copies outside the generated tree, then copy the result
	// back to keep the generated output formatted without changing ignore rules.
	formatDir, err := os.MkdirTemp(".", ".oasmith-oxfmt-")
	if err != nil {
		return fmt.Errorf("create oxfmt directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(formatDir) }()
	config, err := templateFS.ReadFile("oxfmt.json")
	if err != nil {
		return fmt.Errorf("read embedded oxfmt config: %w", err)
	}
	configPath := filepath.Join(formatDir, "oxfmt.json")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		return fmt.Errorf("write temporary oxfmt config: %w", err)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve temporary oxfmt config: %w", err)
	}
	tempPaths := make(map[string]string, len(files))
	var paths []string
	for i, path := range sortedKeys(files) {
		tempPath := filepath.Join(formatDir, fmt.Sprintf("%d-%s", i, filepath.Base(path)))
		if err := os.WriteFile(tempPath, []byte(files[path]), 0o644); err != nil {
			return fmt.Errorf("write temporary typescript output %q: %w", path, err)
		}
		tempPaths[path] = tempPath
		absPath, err := filepath.Abs(tempPath)
		if err != nil {
			return fmt.Errorf("resolve typescript output path %q: %w", path, err)
		}
		paths = append(paths, absPath)
	}
	sort.Strings(paths)
	args := append([]string{"-y", "oxfmt@0.62.0", "--config", absConfigPath, "--write"}, paths...)
	cmd := exec.Command(nubx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("format typescript output with oxfmt: %w\n%s", err, string(output))
	}
	for path, tempPath := range tempPaths {
		formatted, err := os.ReadFile(tempPath)
		if err != nil {
			return fmt.Errorf("read formatted typescript output %q: %w", path, err)
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return fmt.Errorf("write formatted typescript output %q: %w", path, err)
		}
	}
	return nil
}

func sortedKeys(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func modelsSource(doc *openapi.Document) (string, error) {
	return executeTemplate("types.ts", modelsTemplateData(doc))
}

type modelsData struct {
	UsesDuration bool
	Schemas      []modelData
}

type modelData struct {
	Name       string
	OneOf      bool
	Object     bool
	Enum       bool
	OneOfRefs  []string
	Properties []modelPropertyData
	EnumValues []string
	Type       string
}

type modelPropertyData struct {
	Name     string
	Optional string
	Type     string
}

func modelsTemplateData(doc *openapi.Document) modelsData {
	data := modelsData{UsesDuration: doc.UsesDuration()}
	for _, name := range doc.SchemaNames() {
		if name == "string" {
			continue
		}
		schema := doc.Components.Schemas[name]
		if payloadSchema := doc.SSEPayloadSchema(name); payloadSchema != nil {
			schema = payloadSchema
		}
		model := modelData{Name: name}
		switch {
		case schema.IsOneOf():
			model.OneOf = true
			for _, item := range schema.OneOf {
				model.OneOfRefs = append(model.OneOfRefs, openapi.RefName(item.Ref))
			}
		case schema.IsObject():
			model.Object = true
			required := schema.RequiredSet()
			for _, propName := range schema.SortedPropertyNames() {
				model.Properties = append(model.Properties, modelPropertyData{
					Name:     propName,
					Optional: optional(required[propName]),
					Type:     tsType(schema.Properties[propName]),
				})
			}
		case len(schema.Enum) > 0:
			model.Enum = true
			for _, value := range schema.Enum {
				model.EnumValues = append(model.EnumValues, quote(value))
			}
		default:
			model.Type = tsType(schema)
		}
		data.Schemas = append(data.Schemas, model)
	}
	return data
}

func apiSource(doc *openapi.Document) (string, error) {
	operations, err := clientgen.Analyze(doc)
	if err != nil {
		return "", fmt.Errorf("analyze client operations: %w", err)
	}
	data := apiData{
		Imports: modelImports(doc, operations),
	}
	for _, analyzed := range operations {
		params := operationParams(analyzed)
		if len(params) > 0 {
			data.RequestInterfaces = append(data.RequestInterfaces, requestInterfaceData{
				Name:   requestInterfaceName(analyzed.Route.Operation.OperationID),
				Params: params,
			})
		}
		operation := operationTemplateData(analyzed)
		if operation.HasSSE {
			data.HasSSE = true
		}
		if operation.MultipartBody != nil {
			data.HasMultipartBody = true
		}
		data.Operations = append(data.Operations, operation)
	}
	return executeTemplate("api.ts", data)
}

type apiData struct {
	Imports           []string
	HasSSE            bool
	HasMultipartBody  bool
	RequestInterfaces []requestInterfaceData
	Operations        []operationData
}

type requestInterfaceData struct {
	Name   string
	Params []opParam
}

type operationData struct {
	ID                      string
	Method                  string
	Params                  []opParam
	RequiredParams          []opParam
	Responses               []opResponse
	SuccessResponses        []opResponse
	SuccessType             string
	ResultType              string
	RequestObjectSignature  string
	RawRequestSignature     string
	RequestObjectArgument   string
	PositionalSignature     string
	PositionalRequestObject string
	PathExpression          string
	JSONBody                *tsRequestBodyData
	RawBody                 *tsRawRequestBodyData
	MultipartBody           *tsMultipartBodyData
	QueryParams             []opParam
	HasSSE                  bool
}

type tsRequestBodyData struct {
	Name     string
	Required bool
}

type tsRawRequestBodyData struct {
	Name      string
	Required  bool
	MediaType string
}

type tsMultipartBodyData struct {
	MediaType string
	Parts     []tsMultipartPartData
}

type tsMultipartPartData struct {
	Name               string
	Type               string
	JSON               bool
	Binary             bool
	ContentType        string
	AllowedContentType string
}

func operationTemplateData(analyzed clientgen.Operation) operationData {
	route := analyzed.Route
	op := route.Operation
	params := operationParams(analyzed)
	responses := operationResponses(analyzed.Responses)
	data := operationData{
		ID:                      openapi.LowerCamel(op.OperationID),
		Method:                  route.Method,
		Params:                  params,
		Responses:               responses,
		SuccessType:             operationSuccessType(responses),
		ResultType:              operationResultType(responses),
		RequestObjectSignature:  requestObjectSignature(params, requestInterfaceName(op.OperationID)),
		RawRequestSignature:     rawRequestSignature(params, requestInterfaceName(op.OperationID)),
		RequestObjectArgument:   requestObjectArgument(params),
		PositionalSignature:     positionalSignature(params),
		PositionalRequestObject: positionalRequestObject(params),
		PathExpression:          pathExpression(route.Path, params),
		HasSSE:                  analyzed.Accept == "text/event-stream",
	}
	for _, param := range params {
		if param.Required {
			data.RequiredParams = append(data.RequiredParams, param)
		}
		if param.Kind == "query" {
			data.QueryParams = append(data.QueryParams, param)
		}
	}
	for _, response := range responses {
		if isSuccessStatus(response.Status) {
			data.SuccessResponses = append(data.SuccessResponses, response)
		}
	}
	if body := bodyParam(params); body != nil {
		switch body.Kind {
		case "body":
			data.JSONBody = &tsRequestBodyData{Name: body.Name, Required: body.Required}
		case "rawBody":
			data.RawBody = &tsRawRequestBodyData{
				Name:      body.Name,
				Required:  body.Required,
				MediaType: analyzed.RequestBody.Raw.MediaType,
			}
		}
	}
	if analyzed.RequestBody.Multipart != nil {
		data.MultipartBody = multipartBodyTemplateData(analyzed.RequestBody.Multipart)
	}
	return data
}

type opParam struct {
	Name     string
	WireName string
	Type     string
	Required bool
	Optional string
	Kind     string
	Slice    bool
}

type opResponse struct {
	Status int
	Type   string
	Body   bool
	SSE    bool
	JSON   bool
	Text   bool
}

func modelImports(doc *openapi.Document, operations []clientgen.Operation) []string {
	seen := map[string]bool{}
	for _, operation := range operations {
		for _, param := range operationParams(operation) {
			collectModelNames(doc, param.Type, seen)
		}
		for _, response := range operationResponses(operation.Responses) {
			collectModelNames(doc, response.Type, seen)
		}
	}
	var names []string
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collectModelNames(doc *openapi.Document, tsType string, seen map[string]bool) {
	for _, name := range tsTypeIdentifiers(tsType) {
		if name == "DurationString" {
			seen[name] = true
			continue
		}
		if name == "string" {
			continue
		}
		if _, ok := doc.Components.Schemas[name]; ok {
			seen[name] = true
		}
	}
}

func tsTypeIdentifiers(tsType string) []string {
	var names []string
	for index := 0; index < len(tsType); {
		if !isTSIdentifierStart(tsType[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(tsType) && isTSIdentifierPart(tsType[index]) {
			index++
		}
		names = append(names, tsType[start:index])
	}
	return names
}

func isTSIdentifierStart(char byte) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func isTSIdentifierPart(char byte) bool {
	return isTSIdentifierStart(char) || char >= '0' && char <= '9'
}

func executeTemplate(name string, data any) (string, error) {
	raw, err := emit.ExecuteTemplate("typescript", templateFS, name, map[string]any{
		"join": strings.Join,
	}, data)
	return string(raw), err
}

func operationParams(analyzed clientgen.Operation) []opParam {
	operation := analyzed.Route.Operation
	var params []opParam
	for _, param := range operation.Parameters {
		params = append(params, opParam{
			Name:     openapi.LowerCamel(param.Name),
			WireName: param.Name,
			Type:     tsType(param.Schema),
			Required: param.Required,
			Optional: optional(param.Required),
			Kind:     param.In,
			Slice:    param.Schema != nil && param.Schema.IsArray(),
		})
	}
	if multipartBody := analyzed.RequestBody.Multipart; multipartBody != nil {
		for index, part := range multipartBody.Parts {
			name := openapi.LowerCamel(part.Title)
			if name == "" {
				name = fmt.Sprintf("part%d", index+1)
			}
			params = append(params, opParam{
				Name:     name,
				WireName: name,
				Type:     multipartPartType(part),
				Required: true,
				Kind:     "multipart",
			})
		}
	} else if body := analyzed.RequestBody.JSON; body != nil {
		params = append(params, opParam{
			Name:     "body",
			WireName: "body",
			Type:     tsType(body.Schema),
			Required: body.Required,
			Optional: optional(body.Required),
			Kind:     "body",
		})
	} else if body := analyzed.RequestBody.Raw; body != nil {
		params = append(params, opParam{
			Name:     "body",
			WireName: "body",
			Type:     "BodyInit",
			Required: body.Required,
			Optional: optional(body.Required),
			Kind:     "rawBody",
		})
	}
	return params
}

func multipartBodyTemplateData(analyzed *clientgen.MultipartBody) *tsMultipartBodyData {
	body := &tsMultipartBodyData{MediaType: analyzed.MediaType}
	for index, analyzedPart := range analyzed.Parts {
		name := openapi.LowerCamel(analyzedPart.Title)
		if name == "" {
			name = fmt.Sprintf("part%d", index+1)
		}
		part := tsMultipartPartData{
			Name:        name,
			Type:        multipartPartType(analyzedPart),
			ContentType: analyzedPart.ContentType,
			Binary:      analyzedPart.Binary,
			JSON:        !analyzedPart.Binary,
		}
		if analyzedPart.Binary {
			part.AllowedContentType = part.ContentType
		}
		body.Parts = append(body.Parts, part)
	}
	return body
}

func multipartPartType(part clientgen.MultipartPart) string {
	if part.Binary {
		return "Blob"
	}
	return tsType(part.Schema)
}

func operationResponses(analyzed []clientgen.Response) []opResponse {
	responses := make([]opResponse, 0, len(analyzed))
	for _, response := range analyzed {
		responses = append(responses, opResponse{
			Status: response.Status,
			Type:   tsType(response.Schema),
			Body:   response.HasBody(),
			SSE:    response.SSE,
			JSON:   response.JSON(),
			Text:   response.Text(),
		})
	}
	return responses
}

func operationSuccessType(responses []opResponse) string {
	var types []string
	for _, response := range responses {
		if !isSuccessStatus(response.Status) {
			continue
		}
		if response.SSE {
			return response.Type
		}
		if response.Body {
			types = append(types, response.Type)
		} else {
			types = append(types, "void")
		}
	}
	if len(types) == 0 {
		return "void"
	}
	return strings.Join(uniqueStrings(types), " | ")
}

func operationResultType(responses []opResponse) string {
	var variants []string
	for _, response := range responses {
		status := fmt.Sprintf("status: %d", response.Status)
		switch {
		case response.SSE:
			variants = append(variants, fmt.Sprintf("{ %s; body: AsyncIterable<%s>; raw: Response }", status, response.Type))
		case response.Body:
			variants = append(variants, fmt.Sprintf("{ %s; body: %s; raw: Response }", status, response.Type))
		default:
			variants = append(variants, fmt.Sprintf("{ %s; raw: Response }", status))
		}
	}
	if len(variants) == 0 {
		return "never"
	}
	return strings.Join(variants, " | ")
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status < 300
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

func tsType(schema *openapi.Schema) string {
	if schema == nil {
		return "unknown"
	}
	if schema.Ref != "" {
		refName := openapi.RefName(schema.Ref)
		if refName == "string" {
			return "string"
		}
		return refName
	}
	if schema.Const != nil {
		return quote(fmt.Sprint(schema.Const))
	}
	if len(schema.Enum) > 0 {
		var values []string
		for _, value := range schema.Enum {
			values = append(values, quote(value))
		}
		return strings.Join(values, " | ")
	}
	if schema.IsArray() {
		return tsReadonlyArrayType(tsType(schema.Items))
	}
	if schema.IsDuration() {
		return "DurationString"
	}
	switch schema.Type.First() {
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "object":
		return "{ [key: string]: unknown }"
	default:
		return "string"
	}
}

func tsReadonlyArrayType(itemType string) string {
	if strings.Contains(itemType, " | ") || strings.HasPrefix(itemType, "readonly ") {
		itemType = "(" + itemType + ")"
	}
	return "readonly " + itemType + "[]"
}

func requestInterfaceName(operationID string) string {
	return openapi.ExportName(operationID) + "Request"
}

func requestObjectSignature(params []opParam, iface string) string {
	if len(params) == 0 {
		return ""
	}
	return "requestParameters: " + iface
}

func rawRequestSignature(params []opParam, iface string) string {
	if len(params) == 0 {
		return ""
	}
	return "requestParameters: " + iface + ", "
}

func requestObjectArgument(params []opParam) string {
	if len(params) == 0 {
		return ""
	}
	return "requestParameters"
}

func positionalSignature(params []opParam) string {
	lastRequiredIndex := -1
	for index, param := range params {
		if param.Required {
			lastRequiredIndex = index
		}
	}
	var parts []string
	for index, param := range params {
		optionalMarker := optional(param.Required)
		paramType := param.Type
		if !param.Required && index < lastRequiredIndex {
			optionalMarker = ""
			paramType += " | undefined"
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", param.Name, optionalMarker, paramType))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ") + ", "
}

func positionalRequestObject(params []opParam) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for _, param := range params {
		parts = append(parts, param.Name)
	}
	return "{ " + strings.Join(parts, ", ") + " }, "
}

func pathExpression(path string, params []opParam) string {
	expr := path
	for _, param := range params {
		if param.Kind != "path" {
			continue
		}
		expr = strings.ReplaceAll(expr, "{"+param.WireName+"}", "${encodeURIComponent(requestParameters['"+param.Name+"'])}")
	}
	if strings.Contains(expr, "${") {
		return "`" + expr + "`"
	}
	return quote(expr)
}

func bodyParam(params []opParam) *opParam {
	for _, param := range params {
		if param.Kind == "body" || param.Kind == "rawBody" {
			return &param
		}
	}
	return nil
}

func optional(required bool) string {
	if required {
		return ""
	}
	return "?"
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}
