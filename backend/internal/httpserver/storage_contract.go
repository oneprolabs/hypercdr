package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"time"
)

type storageDraftTestResponse struct {
	Status     string                  `json:"status"`
	Detail     string                  `json:"detail"`
	Reachable  bool                    `json:"reachable"`
	TestedAt   time.Time               `json:"testedAt"`
	Repository store.StorageRepository `json:"repository"`
	Error      string                  `json:"error,omitempty"`
	Probe      *map[string]any         `json:"probe"`
}
type storageSavedTestResponse struct {
	Status          string                  `json:"status"`
	Detail          string                  `json:"detail"`
	Reachable       bool                    `json:"reachable"`
	TestedAt        time.Time               `json:"testedAt"`
	Repository      store.StorageRepository `json:"repository"`
	Error           string                  `json:"error,omitempty"`
	CheckedType     string                  `json:"checkedType"`
	CheckedBucket   string                  `json:"checkedBucket"`
	CheckedEndpoint string                  `json:"checkedEndpoint"`
}
type storageSyncRequest struct {
	ClusterID string `json:"clusterId"`
}
type storageSyncQueuedResponse struct {
	Task    store.Task `json:"task"`
	Warning string     `json:"warning"`
}

func storagePayloadContract(pattern string) (payloadContract, int, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/storage-repositories":            {Response: reflect.TypeFor[listResponse[store.StorageRepository]]()},
		"POST /api/v1/storage-repositories":           {Request: reflect.TypeFor[store.StorageRepositoryInput](), Response: reflect.TypeFor[store.StorageRepository](), Required: []string{"name"}},
		"PATCH /api/v1/storage-repositories/{id}":     {Request: reflect.TypeFor[store.StorageRepositoryInput](), Response: reflect.TypeFor[store.StorageRepository](), Required: []string{"name"}},
		"DELETE /api/v1/storage-repositories/{id}":    {Response: reflect.TypeFor[deletedResponse]()},
		"POST /api/v1/storage-repositories/test":      {Request: reflect.TypeFor[store.StorageRepositoryInput](), Response: reflect.TypeFor[storageDraftTestResponse](), Required: []string{"bucket"}},
		"POST /api/v1/storage-repositories/{id}/test": {Response: reflect.TypeFor[storageSavedTestResponse]()},
		"POST /api/v1/storage-repositories/{id}/sync": {Request: reflect.TypeFor[storageSyncRequest](), Response: reflect.TypeFor[store.Task](), Required: []string{"clusterId"}},
	}
	c, ok := contracts[pattern]
	status := 200
	if pattern == "POST /api/v1/storage-repositories" || pattern == "POST /api/v1/storage-repositories/{id}/sync" {
		status = 201
	}
	return c, status, ok
}

func applyStoragePayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := storagePayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	if c.Request == reflect.TypeFor[store.StorageRepositoryInput]() {
		properties := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
		for _, name := range []string{"accessKey", "secretKey", "accountName", "accountKey", "serviceAccountKey"} {
			properties[name].(map[string]any)["writeOnly"] = true
		}
	}
	if pattern == "POST /api/v1/storage-repositories/{id}/sync" {
		operation["responses"].(map[string]any)["202"] = map[string]any{"description": "Task queued; agent offline or dispatch deferred", "content": map[string]any{"application/json": map[string]any{"schema": wireSchema(reflect.TypeFor[storageSyncQueuedResponse]())}}}
	}
	return true
}
