package goemit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/responsibleapi/oasmith/internal/clientgen"
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
	BodylessStatuses string
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

func goClientTemplateData(doc *openapi.Document, sourcePath string, operations []clientgen.Operation) goClientData {
	e := emitter{}
	data := goClientData{
		PackageName: packageName(doc, sourcePath),
	}
	for _, analyzed := range operations {
		operation := e.operationTemplateData(doc, analyzed)
		data.HasJSONBody = data.HasJSONBody || operation.HasJSONBody
		data.HasMultipartBody = data.HasMultipartBody || operation.MultipartBody != nil
		for _, response := range operation.Responses {
			data.HasSSE = data.HasSSE || response.ReconnectableSSE
		}
		data.Operations = append(data.Operations, operation)
	}
	return data
}

func (e *emitter) operationTemplateData(doc *openapi.Document, analyzed clientgen.Operation) goOperationData {
	route := analyzed.Route
	op := route.Operation
	data := goOperationData{
		ID:            openapi.ExportName(op.OperationID),
		Method:        route.Method,
		Path:          route.Path,
		ParamsType:    openapi.ExportName(op.OperationID) + "Params",
		ResponsesType: openapi.ExportName(op.OperationID) + "Response",
		Accept:        analyzed.Accept,
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
	if analyzed.RequestBody.Multipart != nil {
		data.MultipartBody = e.multipartBodyTemplateData(analyzed.RequestBody.Multipart)
		data.HasRequestBody = true
		data.HasParams = true
	} else if body := analyzed.RequestBody.JSON; body != nil {
		paramType := e.goType(body.Schema)
		required := body.Required
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
	} else if body := analyzed.RequestBody.Raw; body != nil {
		data.HasRawBody = true
		data.RequiredRawBody = body.Required
		data.RawBodyMediaType = body.MediaType
		data.HasRequestBody = true
	}
	data.HasParams = len(data.Params) > 0 || data.HasRawBody
	if data.MultipartBody != nil {
		data.HasParams = true
	}
	data.Responses, data.BodylessStatuses = e.operationResponses(analyzed.Responses)
	for _, response := range data.Responses {
		data.ReconnectableSSE = data.ReconnectableSSE || response.ReconnectableSSE
	}
	return data
}

func (e *emitter) multipartBodyTemplateData(analyzed *clientgen.MultipartBody) *goMultipartBodyData {
	body := &goMultipartBodyData{MediaType: analyzed.MediaType}
	for index, analyzedPart := range analyzed.Parts {
		fieldName := openapi.ExportName(analyzedPart.Title)
		if fieldName == "" {
			fieldName = fmt.Sprintf("Part%d", index+1)
		}
		part := goMultipartPartData{
			FieldName:   fieldName,
			ContentType: analyzedPart.ContentType,
		}
		if analyzedPart.Binary {
			part.Binary = true
			part.Type = "io.Reader"
			part.ContentTypeField = fieldName + "ContentType"
			part.AllowedContentType = part.ContentType
		} else {
			part.JSON = true
			part.Type = e.goType(analyzedPart.Schema)
		}
		body.Parts = append(body.Parts, part)
	}
	return body
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

func (e *emitter) operationResponses(analyzed []clientgen.Response) ([]goResponseData, string) {
	responses := make([]goResponseData, 0, len(analyzed))
	var bodylessStatuses []string
	for _, response := range analyzed {
		responseData := goResponseData{
			Status:           response.Status,
			FieldName:        fmt.Sprintf("Status%d", response.Status),
			Type:             e.goType(response.Schema),
			Body:             response.HasBody(),
			JSON:             response.JSON(),
			Text:             response.Text(),
			SSE:              response.SSE,
			ReconnectableSSE: response.SSE,
		}
		if !responseData.Body {
			bodylessStatuses = append(bodylessStatuses, strconv.Itoa(response.Status))
		}
		responses = append(responses, responseData)
	}
	return responses, strings.Join(bodylessStatuses, ", ")
}
