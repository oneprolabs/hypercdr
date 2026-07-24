package httpserver

import (
	"context"
	"net/http/httptest"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestDiagnosticLogFilterForcesTenantScope(t *testing.T) {
	r := &Router{store: store.NewMemoryStore()}
	req := httptest.NewRequest("GET", "/api/v1/diagnostic-logs?tenantId=another-tenant&scope=system", nil)
	user := store.User{ID: "user-a", TenantID: "tenant-a", Role: "admin"}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user))
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if filter.TenantID != "tenant-a" || filter.Scope != "tenant" {
		t.Fatalf("tenant scope was not enforced: %#v", filter)
	}
}

func TestSystemAdminCanSelectAllTenants(t *testing.T) {
	r := &Router{store: store.NewMemoryStore()}
	req := httptest.NewRequest("GET", "/api/v1/diagnostic-logs", nil)
	user := store.User{ID: "admin", TenantID: store.DefaultTenantID, SystemAdmin: true}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user))
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if filter.TenantID != "" || filter.Scope != "" {
		t.Fatalf("system admin all-tenant filter changed: %#v", filter)
	}
}
