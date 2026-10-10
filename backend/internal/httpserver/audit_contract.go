package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
)

func applyAuditPayloadContract(pattern string, operation map[string]any) bool {
	if pattern != "GET /api/v1/audit-logs" {
		return false
	}
	applyPayloadShape(operation, payloadContract{Response: reflect.TypeFor[listResponse[store.AuditLog]]()}, 200)
	operation["description"] = "Audit records in the authenticated tenant; tenant filtering precedes pagination, including for system administrators. Registration-token events are excluded."
	operation["parameters"] = append(operation["parameters"].([]any),
		map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 100}, "description": "1 to 500; other values use 100."},
		map[string]any{"name": "offset", "in": "query", "schema": map[string]any{"type": "integer", "default": 0}, "description": "Negative values use zero."})
	return true
}
