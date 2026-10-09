package httpserver

import (
	"reflect"
	"strconv"
	"time"
)

type agentTokenRequest struct {
	Description string `json:"description"`
	TTLSeconds  int    `json:"ttlSeconds"`
	ClusterType string `json:"clusterType"`
}
type agentTokenResponse struct {
	ID                 string    `json:"id"`
	Token              string    `json:"token"`
	ExpiresAt          time.Time `json:"expiresAt"`
	ClusterType        string    `json:"clusterType"`
	InstallCommand     string    `json:"installCommand"`
	PrepareNodeCommand string    `json:"prepareNodeCommand,omitempty"`
}
type tokenValidationRequest struct {
	Token string `json:"token"`
}
type tokenValidationResponse struct {
	Valid bool `json:"valid"`
}
type disasterTokenValidationResponse struct {
	Valid   bool   `json:"valid"`
	Purpose string `json:"purpose"`
}
type tokenValidationError struct {
	RequestID string `json:"requestId,omitempty"`
	Valid     bool   `json:"valid"`
	Error     string `json:"error"`
	Message   string `json:"message"`
}

func tokenPayloadContract(pattern string) (payloadContract, int, bool) {
	switch pattern {
	case "POST /api/v1/agent-tokens":
		return payloadContract{Request: reflect.TypeFor[agentTokenRequest](), Response: reflect.TypeFor[agentTokenResponse]()}, 201, true
	case "POST /api/v1/agent-tokens/validate":
		return payloadContract{Request: reflect.TypeFor[tokenValidationRequest](), Response: reflect.TypeFor[tokenValidationResponse](), Required: []string{"token"}}, 200, true
	case "POST /api/v1/disaster-handovers/validate":
		return payloadContract{Request: reflect.TypeFor[tokenValidationRequest](), Response: reflect.TypeFor[disasterTokenValidationResponse](), Required: []string{"token"}}, 200, true
	}
	return payloadContract{}, 0, false
}
func applyTokenPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := tokenPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	body := operation["requestBody"].(map[string]any)
	schema := body["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if pattern == "POST /api/v1/agent-tokens" {
		body["required"] = false
		schema["properties"].(map[string]any)["ttlSeconds"].(map[string]any)["description"] = "Positive TTL in seconds; omitted/non-positive values use 30 minutes."
	} else {
		token := schema["properties"].(map[string]any)["token"].(map[string]any)
		token["writeOnly"] = true
		token["minLength"] = 1
		operation["security"] = []any{}
		purpose := "cluster-registration"
		if pattern == "POST /api/v1/disaster-handovers/validate" {
			purpose = "disaster-handover"
		}
		operation["x-hypercdr-token-purpose"] = purpose
		operation["description"] = "Validates the purpose-scoped, expiring, single-use token supplied in the JSON body. Does not consume it or require a platform login."
		responses := operation["responses"].(map[string]any)
		success := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		success["properties"].(map[string]any)["valid"].(map[string]any)["const"] = true
		if purpose == "disaster-handover" {
			success["properties"].(map[string]any)["purpose"].(map[string]any)["const"] = purpose
		} else {
			for _, status := range []int{400, 401, 409, 410, 500} {
				shape := wireSchema(reflect.TypeFor[tokenValidationError]())
				shape["properties"].(map[string]any)["valid"].(map[string]any)["const"] = false
				responses[strconv.Itoa(status)] = map[string]any{"description": "Invalid, consumed, expired token, or persistence failure", "content": map[string]any{"application/json": map[string]any{"schema": shape}}}
			}
		}
	}
	return true
}
