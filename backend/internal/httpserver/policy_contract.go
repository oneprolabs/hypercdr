package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
)

func policyPayloadContract(pattern string) (payloadContract, int, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/policies":         {Response: reflect.TypeFor[listResponse[store.Policy]]()},
		"POST /api/v1/policies":        {Request: reflect.TypeFor[store.PolicyInput](), Response: reflect.TypeFor[store.Policy](), Required: []string{"name"}},
		"PATCH /api/v1/policies/{id}":  {Request: reflect.TypeFor[store.PolicyInput](), Response: reflect.TypeFor[store.Policy](), Required: []string{"name"}},
		"DELETE /api/v1/policies/{id}": {Response: reflect.TypeFor[deletedResponse]()},
	}
	c, ok := contracts[pattern]
	status := 200
	if pattern == "POST /api/v1/policies" {
		status = 201
	}
	return c, status, ok
}

func applyPolicyPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := policyPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	return true
}
