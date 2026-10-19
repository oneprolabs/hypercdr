package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlatformAdministratorBusinessResourcesStayInOwnOrganization(t *testing.T) {
	repo := newTestStore(t)
	a, _ := repo.CreateTenant(store.TenantInput{Name: "Scope A", Status: "active"})
	b, _ := repo.CreateTenant(store.TenantInput{Name: "Scope B", Status: "active"})
	repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: a.ID, Name: "A", Type: "S3"})
	repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: b.ID, Name: "B", Type: "S3"})
	router := &Router{store: repo}
	sys := store.User{ID: "platform-actor", TenantID: a.ID, Role: "admin", SystemAdmin: true}
	org := store.User{ID: "org-actor", TenantID: a.ID, Role: "admin"}
	for _, tc := range []struct {
		scope, method string
		actor         store.User
		status, count int
	}{{"", "GET", sys, 200, 1}, {a.ID, "GET", sys, 200, 1}, {"all", "GET", sys, 403, 0}, {b.ID, "GET", sys, 403, 0}, {"all", "POST", sys, 403, 0}, {b.ID, "GET", org, 403, 0}, {"all", "GET", org, 403, 0}, {"invalid", "GET", sys, 403, 0}} {
		req := tenantRequest(httptest.NewRequest(tc.method, "/api/v1/storage-repositories", nil), tc.actor)
		req.Header.Set("X-HyperCDR-Organization", tc.scope)
		w := httptest.NewRecorder()
		scoped, ok := router.applyOrganizationScope(w, req, tc.actor)
		if !ok {
			if w.Code != tc.status {
				t.Fatalf("%+v: %d", tc, w.Code)
			}
			continue
		}
		identity, _ := requestUser(scoped)
		if identity.ID != tc.actor.ID || identity.TenantID != tc.actor.TenantID {
			t.Fatal("organization browsing altered authenticated identity")
		}
		router.listStorageRepositories(w, scoped)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		var out struct{ Items []store.StorageRepository }
		json.Unmarshal(w.Body.Bytes(), &out)
		if len(out.Items) != tc.count || (len(out.Items) > 0 && out.Items[0].TenantID != a.ID) {
			t.Fatalf("%+v: %s", tc, w.Body.String())
		}
	}
	req := tenantRequest(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil), sys)
	req.Header.Set("X-HyperCDR-Organization", b.ID)
	scoped, ok := router.applyOrganizationScope(httptest.NewRecorder(), req, sys)
	u, _ := requestUser(scoped)
	if !ok || u.TenantID != a.ID {
		t.Fatal("scope affected personal identity")
	}
}
