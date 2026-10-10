package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUserContractsCoverMountedCommunityRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		if !strings.Contains(route.Pattern, "/api/v1/users") {
			continue
		}
		count++
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}}
		if !applyUserPayloadContract(route.Pattern, op) {
			t.Fatalf("missing %s", route.Pattern)
		}
		if _, ok := op["responses"].(map[string]any)["2XX"]; ok {
			t.Fatal("placeholder response remains")
		}
	}
	if count != 4 {
		t.Fatalf("coverage %d", count)
	}
}

func TestUserContractsActualResponsesAndRoleOwnerIsolation(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	other, err := repo.CreateUser(actor.TenantID, "ordinary@example.com", "ordinary-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SetUserPassword(other.ID, "ordinary-password", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateUser(store.UserUpdateInput{ID: other.ID, TenantID: other.TenantID, Email: other.Email, Role: "admin", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(config.Config{}, slog.Default(), repo)
	session, err := repo.CreatePlatformSession(actor.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := repo.CreatePlatformSession(other.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	run := func(pattern, path, body, token string, status int) map[string]any {
		t.Helper()
		method, _, _ := strings.Cut(pattern, " ")
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s: %d %s", pattern, w.Code, w.Body.String())
		}
		var value map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if status < 400 {
			c, ok := userPayloadContract(pattern)
			if !ok {
				t.Fatal("missing contract")
			}
			validateWireObject(t, value, wireSchema(c.Response))
		}
		return value
	}
	run("GET /api/v1/users", "/api/v1/users", "", session.Token, 200)
	run("GET /api/v1/users", "/api/v1/users", "", ordinary.Token, 403)
	run("GET /api/v1/users", "/api/v1/users", "", "", 401)
	for _, item := range []struct{ pattern, suffix, body string }{
		{"PATCH /api/v1/users/{id}", "", `{"displayName":"Contract administrator"}`},
		{"PATCH /api/v1/users/{id}/recovery-email", "/recovery-email", `{"email":"recovery@example.com"}`},
		{"POST /api/v1/users/{id}/password", "/password", `{"password":"new-contract-password"}`},
	} {
		run(item.pattern, "/api/v1/users/"+other.ID+item.suffix, item.body, session.Token, 404)
		run(item.pattern, "/api/v1/users/"+actor.ID+item.suffix, item.body, ordinary.Token, 403)
		run(item.pattern, "/api/v1/users/"+actor.ID+item.suffix, item.body, session.Token, 200)
	}
	run("GET /api/v1/users", "/api/v1/users", "", session.Token, 401)
	if _, ok, err := repo.AuthenticateUser(store.UserAuthInput{Email: other.Email, Password: "ordinary-password"}); err != nil || !ok {
		t.Fatalf("foreign password changed: %v", err)
	}
}

// Enterprise owns its user routes; Community's system-administrator restriction
// must not override the extension's tenant-administrator authorization policy.
func TestUserContractsEnterprisePermissionBoundary(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	user, err := repo.CreateUser(actor.TenantID, "enterprise-admin@example.com", "enterprise-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SetUserPassword(user.ID, "enterprise-password", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateUser(store.UserUpdateInput{ID: user.ID, TenantID: user.TenantID, Email: user.Email, Role: "admin", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, edition := range []string{"community", "enterprise"} {
		router := &Router{store: repo, logger: slog.Default(), identityProvider: storeIdentityProvider{store: repo}, productInfo: ProductInfo{Edition: edition}}
		reached := false
		handler := router.withPlatformAuth(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { reached = true; w.WriteHeader(204) }))
		req := httptest.NewRequest("GET", "/api/v1/users", nil)
		req.Header.Set("Authorization", "Bearer "+session.Token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		expected := 204
		if edition == "community" {
			expected = 403
		}
		if w.Code != expected || reached != (edition == "enterprise") {
			t.Fatalf("%s: status=%d reached=%v", edition, w.Code, reached)
		}
	}
}
