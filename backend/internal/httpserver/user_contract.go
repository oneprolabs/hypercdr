package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strconv"
	"strings"
)

type communityAdminProfileRequest struct {
	DisplayName string `json:"displayName"`
}
type communityAdminPasswordRequest struct {
	Password string `json:"password"`
}
type communityAdminRecoveryRequest struct {
	Email string `json:"email"`
}
type communityAdminPasswordResponse struct {
	Updated bool `json:"updated"`
}
type communityAdminRecoveryResponse struct {
	RecoveryEmail string `json:"recoveryEmail"`
	Verified      bool   `json:"verified"`
}

func userPayloadContract(pattern string) (payloadContract, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/users":                       {Response: reflect.TypeFor[listResponse[store.User]]()},
		"PATCH /api/v1/users/{id}":                {Request: reflect.TypeFor[communityAdminProfileRequest](), Response: reflect.TypeFor[store.User]()},
		"POST /api/v1/users/{id}/password":        {Request: reflect.TypeFor[communityAdminPasswordRequest](), Response: reflect.TypeFor[communityAdminPasswordResponse](), Required: []string{"password"}},
		"PATCH /api/v1/users/{id}/recovery-email": {Request: reflect.TypeFor[communityAdminRecoveryRequest](), Response: reflect.TypeFor[communityAdminRecoveryResponse](), Required: []string{"email"}},
	}
	c, ok := contracts[pattern]
	return c, ok
}

func applyUserPayloadContract(pattern string, operation map[string]any) bool {
	c, ok := userPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, 200)
	operation["description"] = "Community System Administrator only. Mutations require the authenticated administrator's own user ID; another user's ID returns 404."
	if strings.HasSuffix(pattern, "/password") {
		operation["description"] = "Community System Administrator only, own ID required. Updating the password revokes all sessions, including the caller; sign in again afterwards."
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		field := schema["properties"].(map[string]any)["password"].(map[string]any)
		field["writeOnly"] = true
		field["minLength"] = 8
		field["maxLength"] = 128
	}
	if strings.HasSuffix(pattern, "/recovery-email") {
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		schema["properties"].(map[string]any)["email"].(map[string]any)["format"] = "email"
	}
	responses := operation["responses"].(map[string]any)
	for _, code := range []int{400, 401, 403, 404, 423, 500} {
		responses[strconv.Itoa(code)] = map[string]any{"description": "Invalid input, authentication or administrator permission required, missing owner, migration freeze, or persistence failure", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiError"}}}}
	}
	return true
}
