package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/httpserver"
)

type recordingAuthorizer struct {
	request AuthorizationRequest
}

func TestEditionRoutesMapAuthenticatedPrincipal(t *testing.T) {
	var got Principal
	routes := editionRoutes([]Route{{Pattern: "GET /api/v1/enterprise/check", Handler: func(w http.ResponseWriter, _ *http.Request, principal Principal) {
		got = principal
		w.WriteHeader(http.StatusNoContent)
	}}})
	if len(routes) != 1 || routes[0].Pattern != "GET /api/v1/enterprise/check" {
		t.Fatalf("routes = %#v", routes)
	}
	response := httptest.NewRecorder()
	routes[0].Handler(response, httptest.NewRequest(http.MethodGet, "/api/v1/enterprise/check", nil), httpserver.EditionPrincipal{ID: "user-1", TenantID: "tenant-1", Email: "admin@example.com", Role: "admin", SystemAdmin: true})
	if response.Code != http.StatusNoContent || got.ID != "user-1" || got.TenantID != "tenant-1" || !got.SystemAdmin {
		t.Fatalf("response=%d principal=%#v", response.Code, got)
	}
}

func (a *recordingAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) AuthorizationDecision {
	a.request = request
	return AuthorizationDecision{Allowed: false, Code: "governance_denied", Message: "denied by test policy"}
}

func TestEditionAuthorizerMapsStablePublicContract(t *testing.T) {
	recorder := &recordingAuthorizer{}
	adapter := editionAuthorizer(recorder)
	decision := adapter(context.Background(), httpserver.EditionAuthorizationRequest{
		Method: "POST",
		Path:   "/api/v1/tenants",
		Principal: httpserver.EditionPrincipal{
			ID: "user-1", TenantID: "tenant-1", Email: "admin@example.com", Role: "admin", SystemAdmin: true,
		},
	})

	if decision.Allowed || decision.Code != "governance_denied" || decision.Message != "denied by test policy" {
		t.Fatalf("decision = %#v", decision)
	}
	if recorder.request.Method != "POST" || recorder.request.Path != "/api/v1/tenants" || recorder.request.Principal.TenantID != "tenant-1" || !recorder.request.Principal.SystemAdmin {
		t.Fatalf("mapped request = %#v", recorder.request)
	}
}

func TestNilEditionAuthorizerKeepsCommunityStandalone(t *testing.T) {
	if editionAuthorizer(nil) != nil {
		t.Fatal("nil authorizer must preserve Community standalone behavior")
	}
}

func TestCommunityUsesFixedDefaultTenant(t *testing.T) {
	options := CommunityOptions()
	authorizer := options.Authorizer
	if options.DiagnosticLogRetention != 30*24*time.Hour {
		t.Fatalf("Community diagnostic retention = %v", options.DiagnosticLogRetention)
	}
	for _, test := range []struct {
		method, path string
		allowed      bool
	}{
		{method: "GET", path: "/api/v1/tenants", allowed: false},
		{method: "POST", path: "/api/v1/tenants", allowed: false},
		{method: "PATCH", path: "/api/v1/tenants/tenant-2", allowed: false},
		{method: "DELETE", path: "/api/v1/tenants/tenant-2", allowed: false},
		{method: "GET", path: "/api/v1/users", allowed: false},
		{method: "POST", path: "/api/v1/users", allowed: false},
		{method: "GET", path: "/api/v1/diagnostic-logs", allowed: true},
		{method: "GET", path: "/api/v1/diagnostic-logs/export", allowed: false},
		{method: "POST", path: "/api/v1/tasks/backup", allowed: true},
	} {
		decision := authorizer.Authorize(context.Background(), AuthorizationRequest{Method: test.method, Path: test.path})
		if decision.Allowed != test.allowed {
			t.Errorf("%s %s allowed = %v, want %v", test.method, test.path, decision.Allowed, test.allowed)
		}
	}
}
