package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strconv"
	"strings"
)

type clusterSummary struct {
	ID        string `json:"id"`
	IsDefault bool   `json:"isDefault"`
}
type clusterRemovalResponse struct {
	Status    string `json:"status"`
	ClusterID string `json:"clusterId"`
	Warning   string `json:"warning,omitempty"`
}
type clusterUpgradeRequest struct {
	Repair bool `json:"repair"`
}
type clusterUnregisterRequest struct {
	DeleteVelero     *bool  `json:"deleteVelero"`
	DeleteNamespace  *bool  `json:"deleteNamespace"`
	DeleteBackupData bool   `json:"deleteBackupData"`
	Reason           string `json:"reason"`
}
type clusterUnregisterQueuedResponse struct {
	Task    store.Task `json:"task"`
	Warning string     `json:"warning"`
}
type clusterInventoryRequest struct {
	RequestID                  string `json:"requestId"`
	Scope                      string `json:"scope"`
	Namespace                  string `json:"namespace"`
	IncludeDetails             bool   `json:"includeDetails"`
	Reason                     string `json:"reason"`
	IncludeRecentVeleroObjects bool   `json:"includeRecentVeleroObjects"`
}
type clusterInventorySentResponse struct {
	Status    string `json:"status"`
	MessageID string `json:"messageId"`
	RequestID string `json:"requestId"`
	Scope     string `json:"scope"`
	Namespace string `json:"namespace"`
}
type clusterInventoryFailedResponse struct {
	Status    string `json:"status"`
	MessageID string `json:"messageId"`
	RequestID string `json:"requestId"`
	Warning   string `json:"warning"`
}

type clusterErrorResponse struct {
	Error     string           `json:"error"`
	Message   string           `json:"message,omitempty"`
	RequestID string           `json:"requestId,omitempty"`
	Precheck  *unregisterAudit `json:"precheck,omitempty"`
}

func clusterPayloadContract(pattern string) (payloadContract, int, bool) {
	c := payloadContract{}
	status := 200
	switch pattern {
	case "GET /api/v1/clusters":
		c.Response = reflect.TypeFor[listResponse[store.Cluster]]()
	case "PATCH /api/v1/clusters/{id}":
		c.Request = reflect.TypeFor[store.ClusterUpdateInput]()
		c.Response = reflect.TypeFor[store.Cluster]()
	case "POST /api/v1/clusters/{id}/default":
		c.Response = reflect.TypeFor[store.Cluster]()
	case "DELETE /api/v1/clusters/{id}", "POST /api/v1/clusters/{id}/force-cleanup":
		c.Response = reflect.TypeFor[clusterRemovalResponse]()
	case "GET /api/v1/clusters/{id}/unregister/precheck":
		c.Response = reflect.TypeFor[unregisterAudit]()
	case "POST /api/v1/clusters/{id}/unregister":
		c.Request = reflect.TypeFor[clusterUnregisterRequest]()
		c.Response = reflect.TypeFor[store.Task]()
		status = 202
	case "POST /api/v1/clusters/{id}/agent/upgrade", "POST /api/v1/clusters/{id}/velero/upgrade":
		c.Request = reflect.TypeFor[clusterUpgradeRequest]()
		c.Response = reflect.TypeFor[store.Task]()
		status = 202
	case "POST /api/v1/clusters/{id}/inventory/request":
		c.Request = reflect.TypeFor[clusterInventoryRequest]()
		c.Response = reflect.TypeFor[clusterInventorySentResponse]()
		status = 202
	case "GET /api/v1/clusters/{id}/inventory/requests/{requestId}":
		c.Response = reflect.TypeFor[inventoryRequestStatus]()
	default:
		return c, 0, false
	}
	return c, status, true
}

func applyClusterPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := clusterPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	responses := operation["responses"].(map[string]any)
	for _, code := range []int{400, 401, 403, 404, 409, 500} {
		responses[strconv.Itoa(code)] = map[string]any{"description": "Invalid request, denied identity, missing cluster, precheck/runtime conflict, or persistence failure", "content": map[string]any{"application/json": map[string]any{"schema": wireSchema(reflect.TypeFor[clusterErrorResponse]())}}}
	}
	if pattern == "GET /api/v1/clusters" {
		operation["parameters"] = append(operation["parameters"].([]any), map[string]any{"name": "view", "in": "query", "schema": map[string]any{"type": "string", "enum": []string{"summary"}}, "description": "Omit for full cluster records; summary returns only id and isDefault."})
		responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = map[string]any{"anyOf": []any{wireSchema(c.Response), wireSchema(reflect.TypeFor[listResponse[clusterSummary]]())}}
	}
	if strings.HasSuffix(pattern, "/unregister") {
		operation["requestBody"].(map[string]any)["required"] = false
		responses["202"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = map[string]any{"oneOf": []any{wireSchema(c.Response), wireSchema(reflect.TypeFor[clusterUnregisterQueuedResponse]())}}
	}
	if strings.HasSuffix(pattern, "/upgrade") {
		operation["requestBody"].(map[string]any)["required"] = false
	}
	if strings.HasSuffix(pattern, "/inventory/request") {
		responses["202"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"] = map[string]any{"oneOf": []any{wireSchema(c.Response), wireSchema(reflect.TypeFor[clusterInventoryFailedResponse]())}}
	}
	if strings.HasPrefix(pattern, "DELETE ") {
		operation["parameters"] = append(operation["parameters"].([]any), map[string]any{"name": "force", "in": "query", "schema": map[string]any{"type": "boolean"}, "description": "Requires true. Standard removal uses the unregister workflow."})
	}
	return true
}
