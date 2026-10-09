package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"time"
)

type protectionPlanResponse struct {
	ID                   string                  `json:"id"`
	TenantID             string                  `json:"tenantId"`
	SourceClusterID      string                  `json:"sourceClusterId"`
	AppID                string                  `json:"appId"`
	AppIDs               []string                `json:"appIds"`
	ScopeType            string                  `json:"scopeType"`
	IncludedResources    []string                `json:"includedResources"`
	ResourceSelection    store.ResourceSelection `json:"resourceSelection"`
	LabelSelector        store.LabelSelector     `json:"labelSelector"`
	IncludeClusterScoped bool                    `json:"includeClusterScoped"`
	StorageRepoID        string                  `json:"storageRepoId"`
	PolicyID             string                  `json:"policyId"`
	TargetClusterID      string                  `json:"targetClusterId"`
	ExcludedResources    []string                `json:"excludedResources"`
	PreHooks             []map[string]any        `json:"preHooks"`
	PostHooks            []map[string]any        `json:"postHooks"`
	Status               string                  `json:"status"`
	CreatedAt            time.Time               `json:"createdAt"`
	UpdatedAt            time.Time               `json:"updatedAt"`
	ActivationTask       *store.Task             `json:"activationTask,omitempty"`
	CleanupTask          *store.Task             `json:"cleanupTask,omitempty"`
	StorageTasks         *[]store.Task           `json:"storageTasks,omitempty"`
	Warning              string                  `json:"warning,omitempty"`
}

func protectionPlanPayloadContract(pattern string) (payloadContract, int, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/protection-plans":                           {Response: reflect.TypeFor[listResponse[store.ProtectionPlan]]()},
		"POST /api/v1/protection-plans":                          {Request: reflect.TypeFor[store.ProtectionPlanInput](), Response: reflect.TypeFor[protectionPlanResponse](), Required: []string{"sourceClusterId", "storageRepoId"}},
		"POST /api/v1/protection-plans/{id}/activate":            {Response: reflect.TypeFor[protectionPlanResponse]()},
		"POST /api/v1/protection-plans/{id}/storage/reconfigure": {Response: reflect.TypeFor[protectionPlanResponse]()},
		"DELETE /api/v1/protection-plans/{id}":                   {Response: reflect.TypeFor[protectionPlanResponse]()},
	}
	c, ok := contracts[pattern]
	status := 200
	switch pattern {
	case "POST /api/v1/protection-plans":
		status = 201
	case "POST /api/v1/protection-plans/{id}/activate", "POST /api/v1/protection-plans/{id}/storage/reconfigure":
		status = 202
	}
	return c, status, ok
}

func applyProtectionPlanPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := protectionPlanPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	if c.Request != nil {
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		schema["anyOf"] = []any{map[string]any{"required": []string{"appId"}}, map[string]any{"required": []string{"appIds"}}}
	}
	if pattern == "GET /api/v1/protection-plans" {
		operation["parameters"] = append(operation["parameters"].([]any), map[string]any{"name": "clusterId", "in": "query", "required": false, "schema": map[string]any{"type": "string"}})
	}
	return true
}
