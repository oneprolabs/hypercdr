package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strconv"
)

type applicationStatusRequest struct {
	ProtectionStatus string `json:"protectionStatus"`
}
type applicationTagsRequest struct {
	TagIDs []string `json:"tagIds"`
}
type tagNameRequest struct {
	Name string `json:"name"`
}
type deletedResponse struct {
	Deleted bool `json:"deleted"`
}
type listResponse[T any] struct {
	Items []T `json:"items"`
}

func applicationPayloadContract(pattern string) (payloadContract, int, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/applications":           {Response: reflect.TypeFor[listResponse[store.Application]]()},
		"PATCH /api/v1/applications/{id}":    {Request: reflect.TypeFor[applicationStatusRequest](), Response: reflect.TypeFor[store.Application]()},
		"PUT /api/v1/applications/{id}/tags": {Request: reflect.TypeFor[applicationTagsRequest](), Response: reflect.TypeFor[store.Application]()},
		"GET /api/v1/tags":                   {Response: reflect.TypeFor[listResponse[store.Tag]]()},
		"POST /api/v1/tags":                  {Request: reflect.TypeFor[tagNameRequest](), Response: reflect.TypeFor[store.Tag](), Required: []string{"name"}},
		"PATCH /api/v1/tags/{id}":            {Request: reflect.TypeFor[tagNameRequest](), Response: reflect.TypeFor[store.Tag](), Required: []string{"name"}},
		"DELETE /api/v1/tags/{id}":           {Response: reflect.TypeFor[deletedResponse]()},
	}
	c, ok := contracts[pattern]
	status := 200
	if pattern == "POST /api/v1/tags" {
		status = 201
	}
	return c, status, ok
}

func applyPayloadShape(operation map[string]any, c payloadContract, status int) {
	if c.Request != nil {
		schema := wireSchema(c.Request)
		delete(schema, "required")
		if len(c.Required) > 0 {
			schema["required"] = c.Required
		}
		operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	responses := operation["responses"].(map[string]any)
	delete(responses, "2XX")
	responses[strconv.Itoa(status)] = map[string]any{"description": "Successful response", "content": map[string]any{"application/json": map[string]any{"schema": wireSchema(c.Response)}}}
	operation["x-hypercdr-payload-contract"] = true
}

func applyApplicationPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := applicationPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	if pattern == "GET /api/v1/applications" {
		parameters := operation["parameters"].([]any)
		for _, name := range []string{"clusterId", "view", "page", "pageSize"} {
			schema := map[string]any{"type": "string"}
			if name == "page" || name == "pageSize" {
				schema = map[string]any{"type": "integer", "minimum": 1}
			}
			parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": false, "schema": schema})
		}
		operation["parameters"] = parameters
	}
	return true
}
