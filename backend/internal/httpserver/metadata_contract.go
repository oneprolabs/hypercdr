package httpserver

import "reflect"

func applyMetadataPayloadContract(pattern string, operation map[string]any) bool {
	switch pattern {
	case "GET /api/v1/product-info":
		applyPayloadShape(operation, payloadContract{Response: reflect.TypeFor[ProductInfo]()}, 200)
		operation["description"] = "Public edition metadata. Capabilities and license are edition-owned extension values; consumers must preserve unknown extension keys."
	case "GET /api/v1/schema":
		operation["x-hypercdr-payload-contract"] = true
		responses := operation["responses"].(map[string]any)
		delete(responses, "2XX")
		responses["200"] = map[string]any{"description": "OpenAPI 3.1 document generated from mounted routes, wire DTOs and authentication policy", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{
			"type": "object", "required": []string{"openapi", "info", "paths", "components"}, "properties": map[string]any{
				"openapi": map[string]any{"type": "string", "const": "3.1.0"},
				"info":    map[string]any{"type": "object", "required": []string{"title", "version", "description"}, "properties": map[string]any{"title": map[string]any{"type": "string"}, "version": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}},
				"paths": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object", "required": []string{"operationId", "parameters", "security", "responses"}, "properties": map[string]any{
					"operationId": map[string]any{"type": "string"}, "parameters": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "security": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "responses": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "object", "required": []string{"description"}}},
				}}}},
				"components": map[string]any{"type": "object", "required": []string{"securitySchemes", "schemas"}, "properties": map[string]any{"securitySchemes": map[string]any{"type": "object"}, "schemas": map[string]any{"type": "object"}}},
			},
		}}}}
	default:
		return false
	}
	return true
}
