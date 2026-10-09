package httpserver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestNewResourceRouteGetsTenantGuardByDefault(t *testing.T) {
	repo := newTestStore(t)
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "Other tenant", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := repo.CreateAgentToken(tenant.ID, "", "tenant route", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: "other"})
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{store: repo, mux: http.NewServeMux(), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	called := false
	r.handleRoute("GET /api/v1/clusters/{id}/future-operation", func(w http.ResponseWriter, req *http.Request) { called = true; w.WriteHeader(204) })
	req := tenantRequest(httptest.NewRequest("GET", "/api/v1/clusters/"+cluster.ID+"/future-operation", nil), store.User{TenantID: store.DefaultTenantID})
	res := httptest.NewRecorder()
	r.mux.ServeHTTP(res, req)
	if res.Code != 404 || called {
		t.Fatalf("cross-tenant route: status=%d called=%v", res.Code, called)
	}
	req = tenantRequest(httptest.NewRequest("GET", "/api/v1/clusters/"+cluster.ID+"/future-operation", nil), store.User{TenantID: tenant.ID})
	res = httptest.NewRecorder()
	r.mux.ServeHTTP(res, req)
	if res.Code != 204 || !called {
		t.Fatalf("same-tenant route: status=%d called=%v", res.Code, called)
	}
}

func TestRouteContractCoversMountedAPI(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	res := httptest.NewRecorder()
	r.apiSchema(res, httptest.NewRequest("GET", "/api/v1/schema", nil))
	var schema struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.OpenAPI != "3.1.0" || len(schema.Paths) < 80 {
		t.Fatalf("incomplete route schema: %s %d", schema.OpenAPI, len(schema.Paths))
	}
	for _, c := range r.routeContracts {
		if c.Scope == "" {
			t.Fatalf("missing policy: %s", c.Pattern)
		}
	}
}

func TestUnknownAPIDomainCannotMountWithoutPolicy(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unknown API domain mounted without policy")
		}
	}()
	describeRoute("GET /api/v1/new-domain/{id}")
}

func TestAPIErrorIncludesRequestCorrelationWithoutMutatingCaller(t *testing.T) {
	input := map[string]any{"error": "resource_not_found"}
	res := httptest.NewRecorder()
	res.Header().Set("X-Request-ID", "request-1")
	writeJSON(res, 404, input)
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "resource_not_found" || body["requestId"] != "request-1" {
		t.Fatalf("unexpected error: %v", body)
	}
	if _, ok := input["requestId"]; ok {
		t.Fatal("caller error map was mutated")
	}
}

func TestSchemaSecurityMatchesSpecialTokenRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	w := httptest.NewRecorder()
	r.apiSchema(w, httptest.NewRequest("GET", "/api/v1/schema", nil))
	var schema struct {
		Paths map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &schema); err != nil {
		t.Fatal(err)
	}
	for path, methods := range schema.Paths {
		for method, op := range methods {
			release, migration := false, false
			for _, requirement := range op.Security {
				_, hasRelease := requirement["releaseToken"]
				release = release || hasRelease
				_, hasMigration := requirement["migrationSession"]
				migration = migration || hasMigration
			}
			if release != allowsReleaseToken(strings.ToUpper(method), path) {
				t.Fatalf("release-token schema drift: %s %s", method, path)
			}
			wantsMigration := strings.HasPrefix(path, "/api/v1/community-migrations/source/") && strings.Contains(path, "/{id}")
			if migration != wantsMigration {
				t.Fatalf("migration-token schema drift: %s %s", method, path)
			}
		}
	}
}
