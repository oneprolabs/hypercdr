package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/httpserver"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestMissingRegistryBackfillUsesConfiguredRegistryAndPreservesSettings(t *testing.T) {
	settings := store.PlatformSettings{AgentNamespace: "hypercdr-enterprise-agent", VeleroVersion: "v1.18.2", PublicEndpoint: "https://192.168.8.149:3102"}
	input, required := missingRegistryBackfill(settings, config.Config{ImageRegistry: "registry.example/hypercdr/"})
	if !required {
		t.Fatal("empty persisted registry was not backfilled")
	}
	if input.ImageRegistry != "registry.example/hypercdr" || input.AgentNamespace != settings.AgentNamespace || input.VeleroVersion != settings.VeleroVersion || input.PublicEndpoint != settings.PublicEndpoint {
		t.Fatalf("backfill changed unrelated settings: %#v", input)
	}
	settings.ImageRegistry = "persisted.example/hypercdr"
	if _, required = missingRegistryBackfill(settings, config.Config{ImageRegistry: "registry.example/hypercdr"}); required {
		t.Fatal("an existing persisted registry must not be overwritten at startup")
	}
}

func TestReconcileRunningReleaseActivatesExactDeployedVersion(t *testing.T) {
	repo := store.NewMemoryStore()
	oldRelease, err := repo.UpsertPlatformRelease(store.PlatformReleaseInput{Version: "1.0.12.20260903", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	currentRelease, err := repo.UpsertPlatformRelease(store.PlatformReleaseInput{Version: "1.0.13.20260903", Status: "candidate"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcileRunningRelease(repo, currentRelease.Version); err != nil {
		t.Fatal(err)
	}
	releases, err := repo.ListPlatformReleases()
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, release := range releases {
		statuses[release.ID] = release.Status
	}
	if statuses[currentRelease.ID] != "active" || statuses[oldRelease.ID] != "retired" {
		t.Fatalf("unexpected release statuses: %#v", statuses)
	}
}

type recordingAuthorizer struct {
	request AuthorizationRequest
}

type recordingAdmissionController struct{ request AdmissionRequest }

func (a *recordingAdmissionController) Admit(_ context.Context, request AdmissionRequest) AuthorizationDecision {
	a.request = request
	return AuthorizationDecision{Allowed: false, Code: "LICENSE_NODE_CAPACITY_EXCEEDED", Message: "licensed 10 nodes"}
}

func TestEditionAdmissionMapsMeasuredRegistration(t *testing.T) {
	recorder := &recordingAdmissionController{}
	adapter := editionAdmissionController(recorder)
	decision := adapter(context.Background(), httpserver.EditionAdmissionRequest{Operation: "cluster.register", TenantID: "tenant-1", ClusterID: "cluster-1", WorkerNodes: 3})
	if decision.Allowed || decision.Code != "LICENSE_NODE_CAPACITY_EXCEEDED" || recorder.request.WorkerNodes != 3 || recorder.request.ClusterID != "cluster-1" {
		t.Fatalf("decision=%#v request=%#v", decision, recorder.request)
	}
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
		{method: "GET", path: "/api/v1/users", allowed: true},
		{method: "PATCH", path: "/api/v1/users/admin-id", allowed: true},
		{method: "POST", path: "/api/v1/users/admin-id/password", allowed: true},
		{method: "POST", path: "/api/v1/users", allowed: false},
		{method: "GET", path: "/api/v1/diagnostic-logs", allowed: true},
		{method: "GET", path: "/api/v1/diagnostic-logs/export", allowed: true},
		{method: "POST", path: "/api/v1/tasks/backup", allowed: true},
	} {
		decision := authorizer.Authorize(context.Background(), AuthorizationRequest{Method: test.method, Path: test.path})
		if decision.Allowed != test.allowed {
			t.Errorf("%s %s allowed = %v, want %v", test.method, test.path, decision.Allowed, test.allowed)
		}
	}
}
