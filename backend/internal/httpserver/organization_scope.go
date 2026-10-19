package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strings"
)

func resourcePath(path string) bool {
	for _, prefix := range []string{"/api/v1/clusters", "/api/v1/applications", "/api/v1/tasks", "/api/v1/storage-repositories", "/api/v1/policies", "/api/v1/protection-plans", "/api/v1/restore-points", "/api/v1/tags", "/api/v1/agent-tokens", "/api/v1/cluster-registrations"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// Platform administration permissions never change business-resource ownership.
// Reject obsolete or forged cross-organization scope headers for every account.
func (r *Router) applyOrganizationScope(w http.ResponseWriter, req *http.Request, user store.User) (*http.Request, bool) {
	scope := req.Header.Get("X-HyperCDR-Organization")
	if resourcePath(req.URL.Path) && scope != "" && scope != user.TenantID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "organization_scope_forbidden", "message": "Business resources are restricted to your own organization."})
		return req, false
	}
	return req, true
}
