package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlatformPayloadContractsMountedAndAuthentication(t *testing.T) {
	repo := newTestStore(t)
	admin := testAdmin(t, repo)
	user, err := repo.CreateUser(admin.TenantID, "platform-tenant-admin@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SetUserPassword(user.ID, "test-password", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateUser(store.UserUpdateInput{ID: user.ID, TenantID: user.TenantID, Email: user.Email, Role: "admin", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreatePlatformSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := repo.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var router *Router
	handler := NewRouterWithProductInfo(config.Config{ReleaseToken: "isolated-release-token"}, slog.Default(), repo, ProductInfo{Edition: "community"}, RouterOption(func(r *Router) { router = r }))
	count := 0
	for _, route := range router.routeContracts {
		if !strings.Contains(route.Pattern, "/api/v1/platform/") {
			continue
		}
		count++
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}, "parameters": []any{}}
		if !applyPlatformPayloadContract(route.Pattern, op) {
			t.Fatalf("missing %s", route.Pattern)
		}
	}
	if count != 9 {
		t.Fatalf("coverage %d", count)
	}
	run := func(method, path, body, bearer, pipeline string, status int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		if pipeline != "" {
			req.Header.Set("X-HyperCDR-Release-Token", pipeline)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, path := range []string{"/api/v1/platform/releases", "/api/v1/platform/upgrades", "/api/v1/platform/upgrades/precheck"} {
		run("GET", path, "", ordinary.Token, "", 403)
		run("GET", path, "", "", "", 401)
	}
	for _, path := range []string{"/api/v1/users", "/api/v1/email-settings", "/api/v1/platform/releases", "/api/v1/platform/version"} {
		run("GET", path, "", "", "isolated-release-token", 401)
	}
	run("GET", "/api/v1/platform/upgrades", "", "", "wrong-token", 401)
	run("GET", "/api/v1/platform/upgrades", "", "", "isolated-release-token", 200)
	run("GET", "/api/v1/platform/releases/11111111-1111-1111-1111-111111111111", "", "", "isolated-release-token", 404)
	run("POST", "/api/v1/platform/releases", `{}`, "", "isolated-release-token", 400)
	run("POST", "/api/v1/platform/upgrades", `{}`, "", "isolated-release-token", 400)
	run("POST", "/api/v1/platform/upgrades/11111111-1111-1111-1111-111111111111/status", `{"status":"running"}`, "", "isolated-release-token", 404)
	for _, path := range []string{"/api/v1/platform/version", "/api/v1/platform/releases", "/api/v1/platform/upgrades", "/api/v1/platform/upgrades/precheck"} {
		v := run("GET", path, "", session.Token, "", 200)
		c, _, _ := platformPayloadContract("GET " + path)
		validateWireObject(t, v, wireSchema(c.Response))
	}
	router.cfg.ReleaseRepository = "oneprolabs/hypercdr"
	router.releaseCatalogItems = []store.PlatformRelease{}
	router.releaseCatalogAt = time.Now()
	v := run("GET", "/api/v1/platform/available-releases", "", ordinary.Token, "", 200)
	c, _, _ := platformPayloadContract("GET /api/v1/platform/available-releases")
	validateWireObject(t, v, wireSchema(c.Response))
}
