package httpserver

import (
	"reflect"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

type restorePointDeleteRequest struct {
	RestorePointIDs []string `json:"restorePointIds"`
	RestorePointID  string   `json:"restorePointId"`
}
type restorePointDeleteResponse struct {
	Tasks   []store.Task `json:"tasks"`
	Task    *store.Task  `json:"task,omitempty"`
	Warning string       `json:"warning,omitempty"`
}
type restorePointContentsResponse struct {
	RestorePointID   string                           `json:"restorePointId"`
	VeleroBackupName string                           `json:"veleroBackupName"`
	ClusterID        string                           `json:"clusterId"`
	Resources        []protocol.BackupResourceSummary `json:"resources"`
	Truncated        bool                             `json:"truncated"`
	IndexedAt        time.Time                        `json:"indexedAt"`
	Source           string                           `json:"source"`
}

func restorePointPayloadContract(pattern string) (payloadContract, int, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/restore-points":               {Response: reflect.TypeFor[listResponse[store.RestorePoint]]()},
		"GET /api/v1/restore-points/{id}/contents": {Response: reflect.TypeFor[restorePointContentsResponse]()},
		"POST /api/v1/restore-points/delete":       {Request: reflect.TypeFor[restorePointDeleteRequest](), Response: reflect.TypeFor[restorePointDeleteResponse]()},
	}
	c, ok := contracts[pattern]
	status := 200
	if pattern == "POST /api/v1/restore-points/delete" {
		status = 202
	}
	return c, status, ok
}
func applyRestorePointPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := restorePointPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	switch pattern {
	case "GET /api/v1/restore-points":
		parameters := operation["parameters"].([]any)
		for _, name := range []string{"clusterId", "appId", "protectionPlanId", "view", "pageSize", "page"} {
			schema := map[string]any{"type": "string"}
			if name == "pageSize" || name == "page" {
				schema = map[string]any{"type": "integer", "description": "Positive pagination value; pageSize is capped at 500"}
			}
			parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": false, "schema": schema})
		}
		operation["parameters"] = parameters
	case "POST /api/v1/restore-points/delete":
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		schema["anyOf"] = []any{
			map[string]any{"required": []string{"restorePointId"}, "properties": map[string]any{"restorePointId": map[string]any{"type": "string", "pattern": `\S`}}},
			map[string]any{"required": []string{"restorePointIds"}, "properties": map[string]any{"restorePointIds": map[string]any{"type": "array", "contains": map[string]any{"type": "string", "pattern": `\S`}}}},
		}
		operation["description"] = "Validates all restore points before creating any deletion tasks. Foreign-tenant points are indistinguishable from missing points. Accepted tasks remain queued when the source agent is offline."
	case "GET /api/v1/restore-points/{id}/contents":
		response := operation["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		properties := response["properties"].(map[string]any)
		properties["source"].(map[string]any)["enum"] = []string{"index", "indexed_now"}
		properties["resources"] = map[string]any{"anyOf": []any{properties["resources"], map[string]any{"type": "null"}}, "description": "Cached indices return an array; legacy agent reports can return null for an empty catalog."}
	}
	return true
}
