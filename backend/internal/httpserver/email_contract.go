package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type emailTestRequest struct {
	Recipient string `json:"recipient"`
}
type emailTestResponse struct {
	Sent     bool      `json:"sent"`
	TestedAt time.Time `json:"testedAt"`
}

func emailPayloadContract(pattern string) (payloadContract, int, bool) {
	c := payloadContract{}
	status := 200
	switch pattern {
	case "GET /api/v1/email-settings":
		c.Response = reflect.TypeFor[store.EmailSettings]()
	case "GET /api/v1/email-settings/configurations":
		c.Response = reflect.TypeFor[listResponse[store.EmailSettings]]()
	case "PUT /api/v1/email-settings", "PUT /api/v1/email-settings/configurations/{id}", "POST /api/v1/email-settings/configurations":
		c.Request = reflect.TypeFor[emailSettingsRequest]()
		c.Response = reflect.TypeFor[store.EmailSettings]()
		c.Required = []string{"host", "port", "security", "senderEmail"}
		if pattern != "PUT /api/v1/email-settings" {
			c.Required = append(c.Required, "name")
		}
		if strings.HasPrefix(pattern, "POST ") {
			status = 201
			c.Required = append(c.Required, "password")
		}
	case "DELETE /api/v1/email-settings/configurations/{id}":
		c.Response = reflect.TypeFor[deletedResponse]()
	case "POST /api/v1/email-settings/configurations/{id}/default":
		c.Response = reflect.TypeFor[store.EmailSettings]()
	case "POST /api/v1/email-settings/test", "POST /api/v1/email-settings/configurations/{id}/test":
		c.Request = reflect.TypeFor[emailTestRequest]()
		c.Response = reflect.TypeFor[emailTestResponse]()
		c.Required = []string{"recipient"}
	default:
		return c, 0, false
	}
	return c, status, true
}

func applyEmailPayloadContract(pattern string, operation map[string]any) bool {
	c, status, ok := emailPayloadContract(pattern)
	if !ok {
		return false
	}
	applyPayloadShape(operation, c, status)
	operation["description"] = "System Administrator only: installation-wide SMTP configuration. Passwords are encrypted at rest and never returned. Blank update password retains the existing credential; deleting the default configuration returns 409."
	if c.Request != nil {
		schema := operation["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		fields := schema["properties"].(map[string]any)
		if field, ok := fields["password"].(map[string]any); ok {
			field["writeOnly"] = true
		}
		if field, ok := fields["port"].(map[string]any); ok {
			field["minimum"] = 1
			field["maximum"] = 65535
		}
		if field, ok := fields["security"].(map[string]any); ok {
			field["enum"] = []string{"none", "starttls", "tls"}
		}
		for _, name := range []string{"recipient", "senderEmail"} {
			if field, ok := fields[name].(map[string]any); ok {
				field["format"] = "email"
			}
		}
	}
	responses := operation["responses"].(map[string]any)
	for _, code := range []int{400, 401, 403, 404, 409, 423, 500, 502} {
		responses[strconv.Itoa(code)] = map[string]any{"description": "Invalid request, denied identity, missing configuration, duplicate/default conflict, migration freeze, persistence or SMTP failure", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiError"}}}}
	}
	return true
}
