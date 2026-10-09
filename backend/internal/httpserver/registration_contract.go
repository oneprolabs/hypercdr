package httpserver

import (
	"hypercdr-platform/platform/backend/internal/registration"
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strings"
	"time"
)

type kubeconfigUploadResponse struct {
	ID             string              `json:"id"`
	Fingerprint    string              `json:"fingerprint"`
	CurrentContext string              `json:"currentContext"`
	Contexts       []cceContextSummary `json:"contexts"`
	ExpiresAt      time.Time           `json:"expiresAt"`
}

func registrationPayloadContract(pattern string) (payloadContract, int, bool) {
	_, path, _ := strings.Cut(pattern, " ")
	if !strings.HasPrefix(path, "/api/v1/cluster-registrations/") {
		return payloadContract{}, 0, false
	}
	switch {
	case strings.HasPrefix(pattern, "POST ") && strings.HasSuffix(path, "/kubeconfigs"):
		return payloadContract{Response: reflect.TypeFor[kubeconfigUploadResponse]()}, 201, true
	case strings.HasPrefix(pattern, "POST ") && strings.HasSuffix(path, "/inspections"):
		return payloadContract{Request: reflect.TypeFor[registration.InspectRequest](), Response: reflect.TypeFor[registration.Inspection](), Required: []string{"sessionId", "context"}}, 200, true
	case strings.HasPrefix(pattern, "POST ") && strings.HasSuffix(path, "/tasks"):
		return payloadContract{Request: reflect.TypeFor[cceDirectInstallRequest](), Response: reflect.TypeFor[store.Task](), Required: []string{"sessionId", "context", "idempotencyKey"}}, 202, true
	case strings.HasPrefix(pattern, "DELETE ") && strings.HasSuffix(path, "/kubeconfigs/{id}"):
		return payloadContract{}, 204, true
	}
	return payloadContract{}, 0, false
}
func applyRegistrationPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := registrationPayloadContract(pattern)
	if !ok {
		return false
	}
	if status == 204 {
		responses := operation["responses"].(map[string]any)
		delete(responses, "2XX")
		responses["204"] = map[string]any{"description": "Uploader-owned session and its temporary files removed; no response body"}
		operation["x-hypercdr-payload-contract"] = true
		return true
	}
	applyPayloadShape(operation, c, status)
	_, path, _ := strings.Cut(pattern, " ")
	isLegacy := strings.Contains(path, "/cce/")
	if strings.HasSuffix(path, "/kubeconfigs") {
		required := []string{"kubeconfig"}
		if !isLegacy {
			required = append(required, "clusterType")
		}
		operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"multipart/form-data": map[string]any{"schema": map[string]any{"type": "object", "required": required, "properties": map[string]any{"kubeconfig": map[string]any{"type": "string", "format": "binary", "description": "Embedded-credential YAML/JSON kubeconfig, at most 1 MiB; no external auth plugins or credential files"}, "clusterType": map[string]any{"type": "string", "enum": []string{"native-kubernetes", "huaweicloud-cce", "openshift"}}}, "additionalProperties": false}}}}
	} else {
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		if !isLegacy {
			schema["required"] = append(c.Required, "clusterType")
		}
		properties := schema["properties"].(map[string]any)
		properties["clusterType"].(map[string]any)["enum"] = []string{"native-kubernetes", "huaweicloud-cce", "openshift"}
		if strings.HasSuffix(path, "/tasks") {
			key := properties["idempotencyKey"].(map[string]any)
			key["minLength"] = 16
			key["maxLength"] = 128
			operation["responses"].(map[string]any)["200"] = map[string]any{"description": "Existing tenant-scoped registration task reused", "content": map[string]any{"application/json": map[string]any{"schema": wireSchema(c.Response)}}}
		}
	}
	operation["description"] = "Temporary kubeconfig sessions are bound to both tenant and uploader. After an API restart, upload and inspect again; persisted registration tasks remain available. The /cce/ alias defaults to Huawei Cloud CCE."
	return true
}
