package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type diagnosticListResponse struct {
	Items  []store.DiagnosticLog `json:"items"`
	Limit  int                   `json:"limit"`
	Offset int                   `json:"offset"`
}
type diagnosticSource struct {
	ID               string `json:"id"`
	TenantID         string `json:"tenantId"`
	Name             string `json:"name"`
	ConnectionStatus string `json:"connectionStatus"`
}
type clusterLogCollectRequest struct {
	Component string    `json:"component"`
	Since     time.Time `json:"since"`
	TailLines int64     `json:"tailLines"`
}
type clusterLogSearchRequest struct {
	Component string    `json:"component"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
}
type clusterLogCollectResponse struct {
	RequestID string                   `json:"requestId"`
	Status    string                   `json:"status"`
	Count     int                      `json:"count"`
	Truncated bool                     `json:"truncated"`
	Coverage  store.ClusterLogCoverage `json:"coverage"`
	Message   string                   `json:"message"`
}
type clusterLogCachedResponse struct {
	Status           string                   `json:"status"`
	Collected        bool                     `json:"collected"`
	CoverageComplete bool                     `json:"coverageComplete"`
	Coverage         store.ClusterLogCoverage `json:"coverage"`
}
type clusterLogSearchResponse struct {
	Status           string                   `json:"status"`
	Collected        bool                     `json:"collected"`
	Count            int                      `json:"count"`
	Truncated        bool                     `json:"truncated"`
	CoverageComplete bool                     `json:"coverageComplete"`
	Coverage         store.ClusterLogCoverage `json:"coverage"`
	Message          string                   `json:"message,omitempty"`
}

func applyDiagnosticPayloadContract(pattern string, operation map[string]any) bool {
	c := payloadContract{}
	switch pattern {
	case "GET /api/v1/diagnostic-logs":
		c.Response = reflect.TypeFor[diagnosticListResponse]()
	case "GET /api/v1/diagnostic-log-sources":
		c.Response = reflect.TypeFor[listResponse[diagnosticSource]]()
	case "GET /api/v1/diagnostic-logs/export":
		operation["x-hypercdr-payload-contract"] = true
		responses := operation["responses"].(map[string]any)
		delete(responses, "2XX")
		responses["200"] = map[string]any{"description": "Diagnostic log text attachment; timezone defaults to UTC", "content": map[string]any{"text/plain": map[string]any{"schema": map[string]any{"type": "string"}}}}
	case "POST /api/v1/clusters/{id}/logs/collect":
		c.Request = reflect.TypeFor[clusterLogCollectRequest]()
		c.Response = reflect.TypeFor[clusterLogCollectResponse]()
		c.Required = []string{"component"}
	case "POST /api/v1/clusters/{id}/logs/search":
		c.Request = reflect.TypeFor[clusterLogSearchRequest]()
		c.Response = reflect.TypeFor[clusterLogSearchResponse]()
		c.Required = []string{"component"}
	default:
		return false
	}
	if c.Response != nil {
		applyPayloadShape(operation, c, 200)
	}
	operation["description"] = "Ordinary users are restricted to their tenant; System Administrator may query cross-tenant diagnostic data. Cluster requests require a visible cluster; offline collection returns 409, send failure 502, timeout 504."
	if c.Request != nil {
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		schema["properties"].(map[string]any)["component"].(map[string]any)["enum"] = []string{"comm-agent", "velero", "node-agent"}
	}
	if strings.HasSuffix(pattern, "/search") {
		operation["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = map[string]any{"anyOf": []any{wireSchema(c.Response), wireSchema(reflect.TypeFor[clusterLogCachedResponse]())}}
	}
	if strings.Contains(pattern, "/diagnostic-logs") {
		for _, name := range []string{"tenantId", "scope", "source", "level", "component", "clusterId", "taskId", "q", "from", "to", "limit", "offset"} {
			operation["parameters"] = append(operation["parameters"].([]any), map[string]any{"name": name, "in": "query", "schema": map[string]any{"type": "string"}})
		}
	}
	responses := operation["responses"].(map[string]any)
	for _, code := range []int{400, 401, 403, 404, 409, 500, 502, 503, 504} {
		responses[strconv.Itoa(code)] = map[string]any{"description": "Invalid filter/input, denied identity/scope, missing cluster, offline agent, persistence/send failure, restart or timeout", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiError"}}}}
	}
	if pattern == "GET /api/v1/diagnostic-logs/export" {
		operation["parameters"] = append(operation["parameters"].([]any), map[string]any{"name": "timezone", "in": "query", "schema": map[string]any{"type": "string"}, "description": "IANA timezone; invalid value uses UTC"})
	}
	return true
}
