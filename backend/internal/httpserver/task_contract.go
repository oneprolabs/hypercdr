package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
)

type latestTaskResponse struct {
	Task      *store.Task `json:"task"`
	Recovered bool        `json:"recovered,omitempty"`
}
type taskActionResponse struct {
	Task       store.Task `json:"task"`
	CancelTask store.Task `json:"cancelTask,omitempty"`
	Warning    string     `json:"warning,omitempty"`
	Reused     bool       `json:"reused,omitempty"`
}
type backupTaskResponse struct {
	Task    store.Task   `json:"task,omitempty"`
	Tasks   []store.Task `json:"tasks,omitempty"`
	Warning string       `json:"warning,omitempty"`
	Reused  any          `json:"reused,omitempty"`
}

func taskPayloadContract(pattern string) (payloadContract, int, bool) {
	contracts := map[string]payloadContract{
		"GET /api/v1/tasks":                                 {Response: reflect.TypeFor[listResponse[store.Task]]()},
		"GET /api/v1/tasks/{id}":                            {Response: reflect.TypeFor[store.Task]()},
		"GET /api/v1/tasks/{id}/events":                     {Response: reflect.TypeFor[listResponse[store.TaskEvent]]()},
		"GET /api/v1/protection-plans/{id}/latest-sync":     {Response: reflect.TypeFor[latestTaskResponse]()},
		"GET /api/v1/protection-plans/{id}/latest-recovery": {Response: reflect.TypeFor[latestTaskResponse]()},
		"POST /api/v1/tasks/{id}/cancel":                    {Response: reflect.TypeFor[taskActionResponse]()},
		"POST /api/v1/tasks/{id}/cleanup-drill":             {Response: reflect.TypeFor[taskActionResponse]()},
		"POST /api/v1/tasks/{id}/retry":                     {Response: reflect.TypeFor[store.Task]()},
		"POST /api/v1/tasks/backup":                         {Request: reflect.TypeFor[backupTaskRequest](), Response: reflect.TypeFor[backupTaskResponse](), Required: []string{"clusterId"}},
		"POST /api/v1/tasks/restore":                        {Request: reflect.TypeFor[recoveryTaskRequest](), Response: reflect.TypeFor[store.Task](), Required: []string{"clusterId", "restorePointId"}},
		"POST /api/v1/tasks/drill":                          {Request: reflect.TypeFor[recoveryTaskRequest](), Response: reflect.TypeFor[store.Task](), Required: []string{"clusterId", "restorePointId"}},
		"POST /api/v1/tasks/takeover":                       {Request: reflect.TypeFor[recoveryTaskRequest](), Response: reflect.TypeFor[store.Task](), Required: []string{"clusterId", "restorePointId"}},
	}
	c, ok := contracts[pattern]
	status := 200
	switch pattern {
	case "POST /api/v1/tasks/{id}/cancel", "POST /api/v1/tasks/{id}/cleanup-drill":
		status = 202
	case "POST /api/v1/tasks/{id}/retry", "POST /api/v1/tasks/backup", "POST /api/v1/tasks/restore", "POST /api/v1/tasks/drill", "POST /api/v1/tasks/takeover":
		status = 201
	}
	return c, status, ok
}

func applyTaskPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := taskPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	responses := operation["responses"].(map[string]any)
	if pattern == "POST /api/v1/tasks/{id}/cancel" || pattern == "POST /api/v1/tasks/{id}/cleanup-drill" {
		responses["200"] = map[string]any{"description": "Existing operation reused", "content": map[string]any{"application/json": map[string]any{"schema": wireSchema(c.Response)}}}
	}
	if pattern == "POST /api/v1/tasks/backup" {
		schema := wireSchema(c.Response)
		schema["oneOf"] = []any{map[string]any{"required": []string{"task"}}, map[string]any{"required": []string{"tasks"}}}
		schema["properties"].(map[string]any)["reused"] = map[string]any{"oneOf": []any{map[string]any{"type": "boolean"}, map[string]any{"type": "integer", "minimum": 1}}}
		for _, code := range []string{"200", "201"} {
			responses[code] = map[string]any{"description": "Single task or one task per application; 200 when all are reused", "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
		}
		request := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		request["anyOf"] = []any{map[string]any{"required": []string{"sourceNamespace"}}, map[string]any{"required": []string{"appId"}}, map[string]any{"required": []string{"protectionPlanId"}}}
	}
	if pattern == "GET /api/v1/tasks" {
		parameters := operation["parameters"].([]any)
		for _, name := range []string{"clusterId", "types", "statuses", "view", "limit"} {
			schema := map[string]any{"type": "string"}
			if name == "limit" {
				schema = map[string]any{"type": "integer", "description": "Positive limit, capped at 1000 by the server"}
			}
			parameters = append(parameters, map[string]any{"name": name, "in": "query", "required": false, "schema": schema})
		}
		operation["parameters"] = parameters
	}
	return true
}
