package goemit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/responsibleapi/oasmith/internal/openapi"
)

type goClientData struct {
	PackageName      string
	HasJSONBody      bool
	HasMultipartBody bool
	HasSSE           bool
	Operations       []goOperationData
}

type goOperationData struct {
	ID               string
	Method           string
	Path             string
	HasParams        bool
	HasQueryParams   bool
	ParamsType       string
	Params           []goOperationParamData
	ResponsesType    string
	Responses        []goResponseData
	Accept           string
	HasJSONBody      bool
	RequiredJSONBody bool
	HasRequestBody   bool
	HasRawBody       bool
	RequiredRawBody  bool
	RawBodyMediaType string
	MultipartBody    *goMultipartBodyData
	ReconnectableSSE bool
}

type goMultipartBodyData struct {
	MediaType string
	Parts     []goMultipartPartData
}

type goMultipartPartData struct {
	FieldName          string
	Type               string
	JSON               bool
	Binary             bool
	ContentType        string
	ContentTypeField   string
	AllowedContentType string
}

type goOperationParamData struct {
	FieldName string
	WireName  string
	Type      string
	Const     string
	HasConst  bool
	Required  bool
	Path      bool
	Query     bool
	Header    bool
	Body      bool
	Slice     bool
	NonEmpty  bool
}

type goResponseData struct {
	Status           int
	FieldName        string
	Type             string
	Body             bool
	JSON             bool
	Text             bool
	SSE              bool
	ReconnectableSSE bool
}

func goClientTemplateData(doc *openapi.Document, sourcePath string) goClientData {
	e := emitter{}
	data := goClientData{
		PackageName: packageName(doc, sourcePath),
	}
	for _, route := range doc.Operations() {
		operation := e.operationTemplateData(doc, route)
		data.HasJSONBody = data.HasJSONBody || operation.HasJSONBody
		data.HasMultipartBody = data.HasMultipartBody || operation.MultipartBody != nil
		for _, response := range operation.Responses {
			data.HasSSE = data.HasSSE || response.ReconnectableSSE
		}
		data.Operations = append(data.Operations, operation)
	}
	return data
}

func (e *emitter) operationTemplateData(doc *openapi.Document, route openapi.OperationRoute) goOperationData {
	op := route.Operation
	data := goOperationData{
		ID:            openapi.ExportName(op.OperationID),
		Method:        route.Method,
		Path:          route.Path,
		ParamsType:    openapi.ExportName(op.OperationID) + "Params",
		ResponsesType: openapi.ExportName(op.OperationID) + "Response",
		Accept:        operationAccept(doc, route.Method, op),
	}
	for _, param := range op.Parameters {
		paramType := e.goType(param.Schema)
		constant, hasConstant := "", false
		if param.Schema != nil {
			constant, hasConstant = param.Schema.Const.(string)
		}
		if !param.Required {
			paramType = optionalType(paramType)
		}
		data.Params = append(data.Params, goOperationParamData{
			FieldName: openapi.ExportName(param.Name),
			WireName:  param.Name,
			Type:      paramType,
			Const:     constant,
			HasConst:  hasConstant,
			Required:  param.Required,
			Path:      param.In == "path",
			Query:     param.In == "query",
			Header:    param.In == "header",
			Slice:     param.Schema != nil && param.Schema.IsArray(),
			NonEmpty:  param.Required && schemaIsString(doc, param.Schema),
		})
		data.HasQueryParams = data.HasQueryParams || param.In == "query"
	}
	if multipartBody, ok := sequentialMultipartBody(e, op); ok {
		data.MultipartBody = multipartBody
		data.HasRequestBody = true
		data.HasParams = true
	} else if schema := op.JSONRequestSchema(); schema != nil {
		paramType := e.goType(schema)
		required := op.RequestBody != nil && op.RequestBody.Required
		if !required {
			paramType = optionalType(paramType)
		}
		data.Params = append(data.Params, goOperationParamData{
			FieldName: "Body",
			WireName:  "body",
			Type:      paramType,
			Required:  required,
			Body:      true,
		})
		data.HasJSONBody = true
		data.RequiredJSONBody = required
		data.HasRequestBody = true
	} else if mediaType, ok := op.RawRequestBodyMediaType(); ok {
		data.HasRawBody = true
		data.RequiredRawBody = op.RequestBody != nil && op.RequestBody.Required
		data.RawBodyMediaType = mediaType
		data.HasRequestBody = true
	}
	data.HasParams = len(data.Params) > 0 || data.HasRawBody
	if data.MultipartBody != nil {
		data.HasParams = true
	}
	data.Responses = e.operationResponses(doc, route.Method, op)
	for _, response := range data.Responses {
		data.ReconnectableSSE = data.ReconnectableSSE || response.ReconnectableSSE
	}
	return data
}

func sequentialMultipartBody(e *emitter, operation *openapi.Operation) (*goMultipartBodyData, bool) {
	if operation.RequestBody == nil {
		return nil, false
	}
	mediaTypes := make([]string, 0, len(operation.RequestBody.Content))
	for mediaType := range operation.RequestBody.Content {
		mediaTypes = append(mediaTypes, mediaType)
	}
	sort.Strings(mediaTypes)
	for _, mediaType := range mediaTypes {
		media := operation.RequestBody.Content[mediaType]
		if !strings.HasPrefix(mediaType, "multipart/") || media.Schema == nil ||
			!media.Schema.Type.Has("array") || len(media.Schema.PrefixItems) != 2 ||
			len(media.PrefixEncoding) != 2 {
			continue
		}
		minimum, maximum := 0, 0
		if media.Schema.MinItems != nil {
			minimum = *media.Schema.MinItems
		}
		if media.Schema.MaxItems != nil {
			maximum = *media.Schema.MaxItems
		}
		if minimum != 2 || maximum != 2 {
			continue
		}
		body := &goMultipartBodyData{MediaType: mediaType}
		for index, schema := range media.Schema.PrefixItems {
			fieldName := openapi.ExportName(schema.Title)
			if fieldName == "" {
				fieldName = fmt.Sprintf("Part%d", index+1)
			}
			part := goMultipartPartData{
				FieldName:   fieldName,
				ContentType: strings.TrimSpace(media.PrefixEncoding[index].ContentType),
			}
			if schema.Type.Has("string") && schema.Format == "binary" {
				part.Binary = true
				part.Type = "io.Reader"
				part.ContentTypeField = fieldName + "ContentType"
				part.AllowedContentType = part.ContentType
			} else {
				part.JSON = true
				part.Type = e.goType(schema)
			}
			body.Parts = append(body.Parts, part)
		}
		return body, true
	}
	return nil, false
}

func schemaIsString(doc *openapi.Document, schema *openapi.Schema) bool {
	if schema == nil {
		return false
	}
	if schema.Ref != "" && doc != nil {
		return schemaIsString(doc, doc.Components.Schemas[openapi.RefName(schema.Ref)])
	}
	return schema.Type.Has("string")
}

func (e *emitter) operationResponses(doc *openapi.Document, method string, operation *openapi.Operation) []goResponseData {
	statuses := make([]string, 0, len(operation.Responses))
	for status := range operation.Responses {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	responses := make([]goResponseData, 0, len(statuses))
	for _, status := range statuses {
		statusCode, ok := parseStatus(status)
		if !ok {
			continue
		}
		response := doc.ResolveResponse(operation.Responses[status])
		responseData := goResponseData{
			Status:    statusCode,
			FieldName: fmt.Sprintf("Status%d", statusCode),
		}
		mime, media, ok := responseMedia(response)
		if ok {
			schema := media.Schema
			if mime == "text/event-stream" && method == "GET" {
				schema = media.ItemSchema
				responseData.SSE = true
				responseData.ReconnectableSSE = true
			}
			responseData.Type = e.goType(schema)
			responseData.Body = schema != nil
			responseData.JSON = strings.Contains(mime, "json")
			responseData.Text = strings.HasPrefix(mime, "text/") || mime == "application/rss+xml"
		}
		responses = append(responses, responseData)
	}
	return responses
}

func responseMedia(response openapi.Response) (string, openapi.MediaType, bool) {
	for _, mime := range []string{"text/event-stream", "application/json", "application/rss+xml", "text/yaml", "text/plain"} {
		media, ok := response.Content[mime]
		if ok {
			return mime, media, true
		}
	}
	var mimes []string
	for mime := range response.Content {
		mimes = append(mimes, mime)
	}
	sort.Strings(mimes)
	if len(mimes) == 0 {
		return "", openapi.MediaType{}, false
	}
	mime := mimes[0]
	return mime, response.Content[mime], true
}

func operationAccept(doc *openapi.Document, method string, operation *openapi.Operation) string {
	if doc.OperationHasSSEResponseMethod(method, operation) {
		return "text/event-stream"
	}
	for _, response := range operation.Responses {
		resolved := doc.ResolveResponse(response)
		if _, ok := resolved.Content["application/json"]; ok {
			return "application/json"
		}
	}
	return "*/*"
}

func parseStatus(status string) (int, bool) {
	if len(status) != 3 {
		return 0, false
	}
	value := 0
	for _, digit := range status {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		value = value*10 + int(digit-'0')
	}
	return value, true
}
