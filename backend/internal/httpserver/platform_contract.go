package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strconv"
)

type platformVersionResponse struct {
	Version               string `json:"version"`
	GitCommit             string `json:"gitCommit"`
	BuildTime             string `json:"buildTime"`
	DatabaseSchemaVersion string `json:"databaseSchemaVersion"`
	DeployMode            string `json:"deployMode"`
}
type availableReleaseResponse struct {
	Items  []store.PlatformRelease `json:"items"`
	Cached bool                    `json:"cached"`
}

// Camel-case wire names match installed pipelines; encoding/json retains
// case-insensitive decoding of the original exported Go field names.
type platformReleaseRequest struct {
	Version               string                            `json:"version"`
	DatabaseSchemaVersion string                            `json:"databaseSchemaVersion"`
	MinimumAgentVersion   string                            `json:"minimumAgentVersion"`
	ReleaseNotes          string                            `json:"releaseNotes"`
	RollbackSupported     bool                              `json:"rollbackSupported"`
	ComponentManifest     map[string]store.ReleaseComponent `json:"componentManifest"`
}
type platformUpgradeRequest struct {
	ReleaseID string `json:"releaseId"`
}
type platformUpgradeStatusRequest struct {
	Status       string `json:"status"`
	Step         string `json:"step"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	ExecutorID   string `json:"executorId"`
	Progress     int    `json:"progress"`
	MarkStarted  bool   `json:"markStarted"`
	MarkDone     bool   `json:"markDone"`
}
type platformPrecheckItem struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Passed   bool   `json:"passed"`
	Blocking bool   `json:"blocking"`
	Detail   any    `json:"detail,omitempty"`
}
type platformPrecheckResponse struct {
	Passed         bool                   `json:"passed"`
	Checks         []platformPrecheckItem `json:"checks"`
	CurrentVersion string                 `json:"currentVersion"`
}

func platformPayloadContract(pattern string) (payloadContract, int, bool) {
	c := payloadContract{}
	status := 200
	switch pattern {
	case "GET /api/v1/platform/version":
		c.Response = reflect.TypeFor[platformVersionResponse]()
	case "GET /api/v1/platform/available-releases":
		c.Response = reflect.TypeFor[availableReleaseResponse]()
	case "GET /api/v1/platform/releases":
		c.Response = reflect.TypeFor[listResponse[store.PlatformRelease]]()
	case "GET /api/v1/platform/releases/{id}":
		c.Response = reflect.TypeFor[store.PlatformRelease]()
	case "POST /api/v1/platform/releases":
		c.Required = []string{"version", "componentManifest"}
		c.Request = reflect.TypeFor[platformReleaseRequest]()
		c.Response = reflect.TypeFor[store.PlatformRelease]()
		status = 201
	case "GET /api/v1/platform/upgrades":
		c.Response = reflect.TypeFor[listResponse[store.PlatformUpgradeJob]]()
	case "GET /api/v1/platform/upgrades/precheck":
		c.Response = reflect.TypeFor[platformPrecheckResponse]()
	case "POST /api/v1/platform/upgrades":
		c.Request = reflect.TypeFor[platformUpgradeRequest]()
		c.Response = reflect.TypeFor[store.PlatformUpgradeJob]()
		c.Required = []string{"releaseId"}
		status = 202
	case "POST /api/v1/platform/upgrades/{id}/status":
		c.Required = []string{"status"}
		c.Request = reflect.TypeFor[platformUpgradeStatusRequest]()
		c.Response = reflect.TypeFor[store.PlatformUpgradeJob]()
	default:
		return c, 0, false
	}
	return c, status, true
}

func applyPlatformPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := platformPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	operation["description"] = "Installation-wide release/upgrade operation. Releases and upgrades require System Administrator; version and available release catalog permit authenticated users. Pipeline release-token access is limited to the security alternatives on each operation."
	if pattern == "GET /api/v1/platform/upgrades/precheck" {
		operation["parameters"] = append(operation["parameters"].([]any), map[string]any{"name": "releaseId", "in": "query", "schema": map[string]any{"type": "string"}})
	}
	responses := operation["responses"].(map[string]any)
	for _, code := range []int{400, 401, 403, 404, 409, 423, 429, 500, 501, 502} {
		responses[strconv.Itoa(code)] = map[string]any{"description": "Invalid input, denied identity, missing release/job, precheck/conflict, migration freeze, catalog backoff, persistence or upstream failure", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiError"}}}}
	}
	return true
}
