package httpserver

import (
	"net/http"
	"strings"
)

// RouteContract records the effective access policy beside the live route table.
// Auth middleware denies anonymous access to all API routes unless explicitly
// public. Resource-ID guards are selected here, not opt-in in each handler.
type RouteContract struct {
	Pattern  string `json:"pattern"`
	Scope    string `json:"scope"`
	Resource string `json:"resource,omitempty"`
}

func (r *Router) handleRoute(pattern string, handler http.HandlerFunc) {
	contract := describeRoute(pattern)
	if contract.Resource != "" {
		handler = r.tenantGuard(contract.Resource, handler)
	}
	r.routeContracts = append(r.routeContracts, contract)
	r.mux.HandleFunc(pattern, handler)
}

func describeRoute(pattern string) RouteContract {
	_, path, _ := strings.Cut(pattern, " ")
	c := RouteContract{Pattern: pattern, Scope: "authenticated"}
	if !strings.HasPrefix(path, "/api/v1/") {
		c.Scope = "public-or-protocol"
		return c
	}
	if publicAPIPath(path) {
		c.Scope = "public-or-token"
		return c
	}
	resourceRoots := map[string]string{
		"clusters": "cluster", "applications": "application", "tags": "tag",
		"storage-repositories": "storage", "policies": "policy",
		"protection-plans": "plan", "tasks": "task",
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
	if kind, ok := resourceRoots[parts[0]]; ok {
		c.Scope = "tenant"
		if len(parts) > 1 && parts[1] == "{id}" {
			c.Resource = kind
			if kind == "cluster" && len(parts) > 2 && parts[2] == "logs" {
				c.Resource = "cluster-log"
				c.Scope = "tenant-or-system-admin-logs"
			}
		}
		return c
	}
	// These domains apply their own ownership/system-admin checks. Unknown domains
	// fail at startup until their policy is explicitly declared here.
	switch parts[0] {
	case "auth":
		c.Scope = "session-owner"
	case "users", "tenants", "email-settings", "platform":
		c.Scope = "role-or-system-admin"
	case "cluster-registrations", "agent-tokens", "community-migrations", "restore-points", "diagnostic-logs", "diagnostic-log-sources", "support-bundles", "audit-logs", "schema":
		c.Scope = "handler-scoped"
	default:
		panic("API route has no access policy: " + pattern)
	}
	return c
}

func publicAPIPath(path string) bool {
	switch path {
	case "/api/v1/product-info", "/api/v1/auth/captcha", "/api/v1/auth/login", "/api/v1/auth/forgot-password", "/api/v1/auth/reset-password", "/api/v1/auth/config", "/api/v1/auth/turnstile/config", "/api/v1/disaster-handovers/validate", "/api/v1/agent-tokens/validate":
		return true
	}
	return strings.HasPrefix(path, "/api/v1/community-migrations/source/")
}

func (r *Router) apiSchema(w http.ResponseWriter, req *http.Request) {
	paths := map[string]any{}
	for _, route := range r.routeContracts {
		method, path, ok := strings.Cut(route.Pattern, " ")
		if !ok || !strings.HasPrefix(path, "/api/v1/") {
			continue
		}
		operations, ok := paths[path].(map[string]any)
		if !ok {
			operations = map[string]any{}
			paths[path] = operations
		}
		parameters := []any{}
		for _, part := range strings.Split(path, "/") {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				parameters = append(parameters, map[string]any{"name": strings.Trim(part, "{}"), "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			}
		}
		security := []any{map[string]any{"bearerAuth": []string{}}}
		if route.Scope == "public-or-token" {
			security = []any{}
		}
		if allowsReleaseToken(method, path) {
			security = append(security, map[string]any{"releaseToken": []string{}})
		}
		if strings.HasPrefix(path, "/api/v1/community-migrations/source/") && strings.Contains(path, "/{id}") {
			security = []any{map[string]any{"migrationSession": []string{}}}
		}
		operations[strings.ToLower(method)] = map[string]any{
			"operationId": method + " " + path, "parameters": parameters, "security": security,
			"x-hypercdr-scope": route.Scope,
			"responses": map[string]any{
				"2XX":     map[string]any{"description": "Successful response; endpoint payload schemas are being added incrementally."},
				"default": map[string]any{"description": "Structured API error", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ApiError"}}}},
			},
		}
		operation := operations[strings.ToLower(method)].(map[string]any)
		for _, apply := range []func(string, map[string]any) bool{r.applyAuthPayloadContract, applyApplicationPayloadContract, applyPolicyPayloadContract, applyStoragePayloadContract, applyTaskPayloadContract, applyProtectionPlanPayloadContract, applyRestorePointPayloadContract, applyRegistrationPayloadContract, applyTokenPayloadContract, applyClusterPayloadContract, applyUserPayloadContract, applyAuditPayloadContract, applySupportPayloadContract, applyDiagnosticPayloadContract} {
			if apply(route.Pattern, operation) {
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"openapi": "3.1.0", "info": map[string]any{"title": "HyperCDR API", "version": "v1", "description": "Live route/access/error contract. Request and success payload schemas are not yet comprehensive; this is not a client-generation contract."},
		"paths": paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth":       map[string]any{"type": "http", "scheme": "bearer"},
				"releaseToken":     map[string]any{"type": "apiKey", "in": "header", "name": "X-HyperCDR-Release-Token"},
				"migrationSession": map[string]any{"type": "apiKey", "in": "header", "name": "Authorization", "description": "Migration <sessionToken>, bound to the session path ID; not a platform Bearer token."},
			},
			"schemas": map[string]any{"ApiError": map[string]any{"type": "object", "required": []string{"error"}, "properties": map[string]any{"error": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}, "requestId": map[string]any{"type": "string"}}, "additionalProperties": true}},
		},
	})
}
