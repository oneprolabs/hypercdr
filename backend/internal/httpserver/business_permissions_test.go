package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestBusinessRoutePermissionMapping(t *testing.T) {
	cases := []struct{ method, path, permission string }{
		{"GET", "/api/v1/clusters", "resources.view"},
		{"GET", "/api/v1/tasks/id/events", "resources.view"},
		{"POST", "/api/v1/agent-tokens", "cluster.register"},
		{"POST", "/api/v1/cluster-registrations/tasks", "cluster.register"},
		{"POST", "/api/v1/clusters/id/unregister", "cluster.unregister"},
		{"POST", "/api/v1/clusters/id/force-cleanup", "cluster.unregister"},
		{"PUT", "/api/v1/clusters/id/cache-settings", "cluster.manage"},
		{"POST", "/api/v1/clusters/id/velero/upgrade", "cluster.manage"},
		{"POST", "/api/v1/clusters/id/logs/collect", "diagnostics.manage"},
		{"PATCH", "/api/v1/applications/id", "sync.manage"},
		{"POST", "/api/v1/protection-plans", "sync.manage"},
		{"DELETE", "/api/v1/protection-plans/id", "sync.manage"},
		{"POST", "/api/v1/tasks/backup", "sync.manage"},
		{"POST", "/api/v1/tasks/drill", "drill.execute"},
		{"POST", "/api/v1/tasks/restore", "recovery.execute"},
		{"POST", "/api/v1/tasks/takeover", "recovery.execute"},
		{"POST", "/api/v1/restore-points/delete", "backup.delete"},
		{"POST", "/api/v1/tasks/id/abort", "task.abort"},
		{"POST", "/api/v1/tasks/id/retry", "task-operation"},
		{"POST", "/api/v1/tasks/id/cleanup-drill", "task-operation"},
		{"POST", "/api/v1/tasks/new-operation", "unmapped-business-operation"},
		{"POST", "/api/v1/saas/roles", ""},
		{"PATCH", "/api/v1/saas/organization", ""},
	}
	for _, test := range cases {
		if actual := businessPermission(test.method, test.path); actual != test.permission {
			t.Errorf("%s %s: want %s, got %s", test.method, test.path, test.permission, actual)
		}
	}
}

func TestCustomBusinessPermissionsRejectBeforeHandler(t *testing.T) {
	user := store.User{ID: "member", TenantID: store.DefaultTenantID, Role: "operator", RoleID: "custom-role", Permissions: []string{"resources.view", "drill.execute"}}
	router := &Router{productInfo: ProductInfo{Edition: "community"}}
	for _, path := range []string{"/api/v1/agent-tokens", "/api/v1/tasks/backup", "/api/v1/tasks/restore", "/api/v1/protection-plans"} {
		response := httptest.NewRecorder()
		if router.authorizeBusinessRequest(response, httptest.NewRequest("POST", path, nil), user) || response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "organization_permission_required") {
			t.Fatalf("unauthorized business mutation %s: %d %s", path, response.Code, response.Body.String())
		}
	}
	if !router.authorizeBusinessRequest(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/tasks/drill", nil), user) {
		t.Fatal("assigned Drill permission denied")
	}
	if !router.authorizeBusinessRequest(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/clusters", nil), user) {
		t.Fatal("assigned view permission denied")
	}
	user.Permissions = nil
	if router.authorizeBusinessRequest(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/clusters", nil), user) {
		t.Fatal("empty role retained resource visibility")
	}
	user.RoleID = "operator"
	for _, permission := range []string{"cluster.register", "sync.manage", "drill.execute", "recovery.execute", "cluster.unregister"} {
		if !hasBusinessPermission(user, permission) {
			t.Fatalf("built-in Operator missing %s", permission)
		}
	}
}
