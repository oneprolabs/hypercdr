package httpserver

import (
	"reflect"
	"time"
)

type supportBundleResponse struct {
	Name        string    `json:"name"`
	DownloadURL string    `json:"downloadUrl"`
	Size        int64     `json:"size"`
	GeneratedAt time.Time `json:"generatedAt"`
	TimeZone    string    `json:"timeZone"`
}

func applySupportPayloadContract(pattern string, operation map[string]any) bool {
	switch pattern {
	case "POST /api/v1/support-bundles":
		applyPayloadShape(operation, payloadContract{Request: reflect.TypeFor[supportBundleRequest](), Response: reflect.TypeFor[supportBundleResponse](), Required: []string{"description"}}, 201)
		operation["description"] = "System Administrator only: synchronous platform-wide diagnostics. JSON body limited to 14 MiB; description required. sinceHours outside 1..168 uses 24; invalid timezone uses UTC. Restart interruption requires retry; bundles are local to the installation."
	case "GET /api/v1/support-bundles/{name}/download":
		operation["x-hypercdr-payload-contract"] = true
		responses := operation["responses"].(map[string]any)
		delete(responses, "2XX")
		responses["200"] = map[string]any{"description": "System Administrator only: gzip archive; supports standard HTTP conditional and range requests.", "content": map[string]any{"application/gzip": map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}}}
		responses["206"] = map[string]any{"description": "Partial archive for a satisfiable Range request", "content": map[string]any{"application/gzip": map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}}}
		responses["304"] = map[string]any{"description": "Not modified; no response body"}
		responses["416"] = map[string]any{"description": "Unsatisfiable Range request", "content": map[string]any{"text/plain": map[string]any{"schema": map[string]any{"type": "string"}}}}
		responses["404"] = map[string]any{"description": "Unknown or invalid bundle name", "content": map[string]any{"text/plain": map[string]any{"schema": map[string]any{"type": "string"}}}}
	case "DELETE /api/v1/support-bundles/{name}":
		operation["x-hypercdr-payload-contract"] = true
		responses := operation["responses"].(map[string]any)
		delete(responses, "2XX")
		responses["204"] = map[string]any{"description": "System Administrator only: deleted; absent valid names are idempotent. No response body."}
		responses["404"] = map[string]any{"description": "Invalid bundle name", "content": map[string]any{"text/plain": map[string]any{"schema": map[string]any{"type": "string"}}}}
	default:
		return false
	}
	return true
}
