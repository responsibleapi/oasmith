// Package clientgen analyzes the language-neutral HTTP contract used by client emitters.
package clientgen

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/responsibleapi/oasmith/internal/openapi"
)

// Operation is the normalized client contract for one OpenAPI operation.
type Operation struct {
	Route       openapi.OperationRoute
	RequestBody RequestBody
	Responses   []Response
	Accept      string
}

// RequestBody is exactly one supported request body shape.
type RequestBody struct {
	JSON      *JSONBody
	Raw       *RawBody
	Multipart *MultipartBody
}

// JSONBody describes a JSON request body.
type JSONBody struct {
	Schema   *openapi.Schema
	Required bool
}

// RawBody describes a request body passed through without encoding.
type RawBody struct {
	MediaType string
	Required  bool
}

// MultipartBody describes the fixed-length sequential multipart shape supported by clients.
type MultipartBody struct {
	MediaType string
	Required  bool
	Parts     []MultipartPart
}

// MultipartPart describes one positional multipart part.
type MultipartPart struct {
	Title       string
	Schema      *openapi.Schema
	ContentType string
	Binary      bool
}

// Response describes one concrete response status and its selected representation.
type Response struct {
	Status    int
	MediaType string
	Schema    *openapi.Schema
	SSE       bool
}

// HasBody reports whether the response has a supported typed body.
func (response Response) HasBody() bool {
	return response.Schema != nil
}

// JSON reports whether the response body is JSON encoded.
func (response Response) JSON() bool {
	return response.HasBody() && strings.Contains(response.MediaType, "json")
}

// Text reports whether the response body is text encoded.
func (response Response) Text() bool {
	return response.HasBody() &&
		(strings.HasPrefix(response.MediaType, "text/") ||
			response.MediaType == "application/xml" || strings.HasSuffix(response.MediaType, "+xml"))
}

// Analyze normalizes and validates every operation used by generated clients.
func Analyze(doc *openapi.Document) ([]Operation, error) {
	routes := doc.Operations()
	operations := make([]Operation, 0, len(routes))
	for _, route := range routes {
		requestBody, err := analyzeRequestBody(route.Operation)
		if err != nil {
			return nil, err
		}
		responses := analyzeResponses(doc, route)
		operations = append(operations, Operation{
			Route:       route,
			RequestBody: requestBody,
			Responses:   responses,
			Accept:      operationAccept(responses),
		})
	}
	return operations, nil
}

func analyzeRequestBody(operation *openapi.Operation) (RequestBody, error) {
	if operation.RequestBody == nil {
		return RequestBody{}, nil
	}
	if media, ok := operation.RequestBody.Content["application/json"]; ok {
		if media.Schema == nil {
			return RequestBody{}, fmt.Errorf(
				"operation %s has unsupported request body: application/json schema is missing",
				operation.OperationID,
			)
		}
		return RequestBody{JSON: &JSONBody{
			Schema:   media.Schema,
			Required: operation.RequestBody.Required,
		}}, nil
	}
	if multipart, ok := sequentialMultipartBody(operation.RequestBody); ok {
		return RequestBody{Multipart: multipart}, nil
	}
	if mediaType, ok := operation.RawRequestBodyMediaType(); ok && !strings.HasPrefix(mediaType, "multipart/") {
		return RequestBody{Raw: &RawBody{
			MediaType: mediaType,
			Required:  operation.RequestBody.Required,
		}}, nil
	}
	return RequestBody{}, fmt.Errorf("operation %s has unsupported request body", operation.OperationID)
}

func sequentialMultipartBody(requestBody *openapi.RequestBody) (*MultipartBody, bool) {
	mediaTypes := make([]string, 0, len(requestBody.Content))
	for mediaType := range requestBody.Content {
		mediaTypes = append(mediaTypes, mediaType)
	}
	sort.Strings(mediaTypes)
	for _, mediaType := range mediaTypes {
		media := requestBody.Content[mediaType]
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
		body := &MultipartBody{
			MediaType: mediaType,
			Required:  requestBody.Required,
		}
		for index, schema := range media.Schema.PrefixItems {
			body.Parts = append(body.Parts, MultipartPart{
				Title:       schema.Title,
				Schema:      schema,
				ContentType: strings.TrimSpace(media.PrefixEncoding[index].ContentType),
				Binary:      schema.Type.Has("string") && schema.Format == "binary",
			})
		}
		return body, true
	}
	return nil, false
}

func analyzeResponses(doc *openapi.Document, route openapi.OperationRoute) []Response {
	statuses := make([]string, 0, len(route.Operation.Responses))
	for status := range route.Operation.Responses {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	responses := make([]Response, 0, len(statuses))
	for _, status := range statuses {
		statusCode, ok := parseStatus(status)
		if !ok {
			continue
		}
		resolved := doc.ResolveResponse(route.Operation.Responses[status])
		mediaType, media, ok := responseMedia(resolved)
		response := Response{Status: statusCode}
		if ok {
			response.MediaType = mediaType
			response.Schema = media.Schema
			if mediaType == "text/event-stream" && route.Method == http.MethodGet && isSuccessStatus(statusCode) {
				response.Schema = media.ItemSchema
				response.SSE = response.Schema != nil
			}
		}
		responses = append(responses, response)
	}
	return responses
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status < 300
}

func responseMedia(response openapi.Response) (string, openapi.MediaType, bool) {
	for _, mediaType := range []string{
		"text/event-stream",
		"application/json",
		"application/rss+xml",
		"text/yaml",
		"text/plain",
	} {
		media, ok := response.Content[mediaType]
		if ok {
			return mediaType, media, true
		}
	}
	mediaTypes := make([]string, 0, len(response.Content))
	for mediaType := range response.Content {
		mediaTypes = append(mediaTypes, mediaType)
	}
	sort.Strings(mediaTypes)
	if len(mediaTypes) == 0 {
		return "", openapi.MediaType{}, false
	}
	mediaType := mediaTypes[0]
	return mediaType, response.Content[mediaType], true
}

func operationAccept(responses []Response) string {
	for _, response := range responses {
		if response.SSE {
			return "text/event-stream"
		}
	}
	for _, response := range responses {
		if response.MediaType == "application/json" {
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
