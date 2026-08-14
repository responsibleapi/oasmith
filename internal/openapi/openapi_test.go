package openapi_test

import (
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/responsibleapi/oasmith/internal/openapi"
)

func TestParseOpenAPI32PrivateFixture(t *testing.T) {
	t.Parallel()

	doc, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "fixtures", "private.yaml"))
	if err != nil {
		t.Fatalf("ParseFile private fixture: %v", err)
	}
	if doc.OpenAPI != "3.2.0" {
		t.Fatalf("OpenAPI version = %q, want 3.2.0", doc.OpenAPI)
	}
	if len(doc.Operations()) == 0 {
		t.Fatal("private fixture parsed no operations")
	}
}

func TestResolveSharedPathParametersWithOperationOverride(t *testing.T) {
	t.Parallel()

	doc, err := openapi.Parse([]byte(`
openapi: 3.1.0
info: {title: parameters, version: "1"}
paths:
  /items:
    parameters:
      - $ref: "#/components/parameters/limit"
    get:
      operationId: listItems
      parameters:
        - name: limit
          in: query
          required: true
          schema: {type: integer}
      responses: {"200": {description: ok}}
components:
  parameters:
    limit:
      name: limit
      in: query
      required: false
      schema: {type: integer}
`))
	if err != nil {
		t.Fatalf("parse shared parameter fixture: %v", err)
	}
	operations := doc.Operations()
	if len(operations) != 1 || len(operations[0].Operation.Parameters) != 1 {
		t.Fatalf("operations = %#v", operations)
	}
	parameter := operations[0].Operation.Parameters[0]
	if parameter.Name != "limit" || !parameter.Required {
		t.Fatalf("merged parameter = %#v, want required operation override", parameter)
	}
}

func TestExportNameProducesValidIdentifiersForExternalSpecs(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"$.xgafv":  "Xgafv",
		"3d-video": "X3dVideo",
		"video.id": "VideoId",
	} {
		if got := openapi.ExportName(input); got != want {
			t.Errorf("ExportName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDetectTextEventStreamOperation(t *testing.T) {
	t.Parallel()

	doc, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "fixtures", "private.yaml"))
	if err != nil {
		t.Fatalf("ParseFile private fixture: %v", err)
	}
	for _, route := range doc.Operations() {
		if route.Operation.OperationID == "episodeProcessingEvents" {
			if !doc.OperationHasSSEResponse(route.Operation) {
				t.Fatal("episodeProcessingEvents was not detected as SSE")
			}
			return
		}
	}
	t.Fatal("episodeProcessingEvents operation not found")
}

func TestSuccessfulSSEOperationsAreGETOnly(t *testing.T) {
	t.Parallel()

	for _, fixture := range []string{"public-client.yaml", "private.yaml"} {
		doc, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "fixtures", fixture))
		if err != nil {
			t.Fatalf("ParseFile %s: %v", fixture, err)
		}
		for _, route := range doc.Operations() {
			for status, response := range route.Operation.Responses {
				statusCode, err := strconv.Atoi(status)
				if err != nil || statusCode < 200 || statusCode >= 300 {
					continue
				}
				resolved := doc.ResolveResponse(response)
				if _, ok := resolved.Content["text/event-stream"]; ok && route.Method != http.MethodGet {
					t.Errorf("%s %s (%s) exposes a successful SSE response on a non-GET operation", route.Method, route.Path, fixture)
				}
			}
		}
	}
}

func TestSSEPayloadSchema(t *testing.T) {
	t.Parallel()

	privateDoc, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "fixtures", "private.yaml"))
	if err != nil {
		t.Fatalf("ParseFile private fixture: %v", err)
	}
	complete := privateDoc.SSEPayloadSchema("EpisodeProcessingCompleteEvent")
	if complete == nil || complete.Properties["type"].Const != "episode.processing.complete" {
		t.Fatalf("single SSE payload schema = %#v", complete)
	}

	publicDoc, err := openapi.ParseFile(filepath.Join("..", "..", "testdata", "fixtures", "public-client.yaml"))
	if err != nil {
		t.Fatalf("ParseFile public client fixture: %v", err)
	}
	event := publicDoc.SSEPayloadSchema("Event")
	if event == nil || event.Discriminator == nil || event.Discriminator.PropertyName != "kind" {
		t.Fatalf("union SSE payload schema = %#v", event)
	}
	if len(event.OneOf) != 2 || event.OneOf[0].Ref != "#/components/schemas/ProgressEvent" ||
		event.OneOf[1].Ref != "#/components/schemas/TerminalEvent" {
		t.Fatalf("union SSE payload variants = %#v", event.OneOf)
	}
}

func TestDetectSSEOnlyFromSuccessfulResponseMediaType(t *testing.T) {
	t.Parallel()

	doc := &openapi.Document{
		Components: openapi.Components{
			Responses: map[string]*openapi.Response{
				"EventStream": {
					Content: map[string]openapi.MediaType{
						"text/event-stream": {ItemSchema: &openapi.Schema{}},
					},
				},
			},
		},
	}
	if !doc.OperationHasSSEResponse(&openapi.Operation{
		Responses: map[string]openapi.Response{
			"200": {Ref: "#/components/responses/EventStream"},
		},
	}) {
		t.Fatal("referenced successful text/event-stream response was not detected")
	}
	if doc.OperationHasSSEResponse(&openapi.Operation{
		Responses: map[string]openapi.Response{
			"400": {Content: map[string]openapi.MediaType{"text/event-stream": {}}},
		},
	}) {
		t.Fatal("error text/event-stream response was detected as an SSE operation")
	}
	if doc.OperationHasSSEResponse(&openapi.Operation{
		Responses: map[string]openapi.Response{
			"200": {Content: map[string]openapi.MediaType{"application/json": {}}},
		},
	}) {
		t.Fatal("JSON success response was detected as an SSE operation")
	}
}

func TestSchemaDurationDetection(t *testing.T) {
	t.Parallel()

	duration := &openapi.Schema{
		Type:   openapi.Type{"string"},
		Format: "duration",
	}
	if !duration.IsDuration() {
		t.Fatal("duration string schema was not detected")
	}

	plainString := &openapi.Schema{
		Type: openapi.Type{"string"},
	}
	if plainString.IsDuration() {
		t.Fatal("plain string schema was detected as duration")
	}
}

func TestSchemaUsesDurationRecursively(t *testing.T) {
	t.Parallel()

	schema := &openapi.Schema{
		Type: openapi.Type{"object"},
		Properties: map[string]*openapi.Schema{
			"values": {
				Type: openapi.Type{"array"},
				Items: &openapi.Schema{
					Type:   openapi.Type{"string"},
					Format: "duration",
				},
			},
		},
	}
	if !schema.UsesDuration() {
		t.Fatal("nested duration schema was not detected")
	}
}

func TestSchemaRequiredSetAndSortedPropertyNames(t *testing.T) {
	t.Parallel()

	schema := &openapi.Schema{
		Required: []string{"b"},
		Properties: map[string]*openapi.Schema{
			"b": {},
			"a": {},
		},
	}
	required := schema.RequiredSet()
	if !required["b"] || required["a"] {
		t.Fatalf("RequiredSet = %#v, want only b required", required)
	}
	names := schema.SortedPropertyNames()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("SortedPropertyNames = %#v, want [a b]", names)
	}
}
