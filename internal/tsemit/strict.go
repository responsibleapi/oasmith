package tsemit

import (
	"encoding/json"
	"fmt"

	"github.com/responsibleapi/oasmith/internal/openapi"
)

func jsonShape(schema *openapi.Schema) map[string]any {
	shape := map[string]any{}
	if schema == nil {
		return shape
	}
	if schema.Ref != "" {
		shape["ref"] = openapi.RefName(schema.Ref)
		return shape
	}
	if len(schema.Type) > 0 {
		shape["type"] = schema.Type
	}
	if schema.Const != nil {
		shape["const"] = schema.Const
	}
	if len(schema.Enum) > 0 {
		shape["enum"] = schema.Enum
	}
	if schema.AdditionalProperties != nil {
		shape["additionalProperties"] = schema.AdditionalProperties
	}
	if len(schema.Properties) > 0 {
		properties := map[string]any{}
		for key, property := range schema.Properties {
			properties[key] = jsonShape(property)
		}
		shape["properties"] = properties
	}
	if len(schema.Required) > 0 {
		shape["required"] = schema.Required
	}
	if schema.Items != nil {
		shape["items"] = jsonShape(schema.Items)
	}
	if len(schema.OneOf) > 0 {
		variants := []any{}
		for _, variant := range schema.OneOf {
			variants = append(variants, jsonShape(variant))
		}
		shape["oneOf"] = variants
	}
	if schema.Discriminator != nil {
		mapping := map[string]string{}
		for tag, ref := range schema.Discriminator.Mapping {
			mapping[tag] = openapi.RefName(ref)
		}
		shape["discriminator"] = map[string]any{"propertyName": schema.Discriminator.PropertyName, "mapping": mapping}
	}
	return shape
}

func jsonSchemaSource(schema *openapi.Schema) (string, error) {
	raw, err := json.Marshal(jsonShape(schema))
	if err != nil {
		return "", fmt.Errorf("encode TypeScript JSON schema: %w", err)
	}
	return string(raw), nil
}

func strictJSONSource(doc *openapi.Document) (string, error) {
	schemas := map[string]any{}
	for _, name := range doc.SchemaNames() {
		schema := doc.Components.Schemas[name]
		if payload := doc.SSEPayloadSchema(name); payload != nil {
			schema = payload
		}
		schemas[name] = jsonShape(schema)
	}
	raw, err := json.Marshal(schemas)
	if err != nil {
		return "", fmt.Errorf("encode TypeScript JSON schemas: %w", err)
	}
	return executeTemplate("strict-json.ts", struct {
		SchemasJSON string
		Models      []modelData
	}{string(raw), modelsTemplateData(doc).Schemas})
}
