package httpserver

import (
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUpgradeRunnerReleaseReadAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, path, token string
		want              int
	}{
		{"release read", "/api/v1/platform/releases/release-id", "runner-secret", 204},
		{"wrong token", "/api/v1/platform/releases/release-id", "wrong", 401},
		{"missing token", "/api/v1/platform/releases/release-id", "", 401},
		{"unrelated API", "/api/v1/clusters", "runner-secret", 401},
		{"nested resource", "/api/v1/platform/releases/release-id/other", "runner-secret", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Router{cfg: config.Config{ReleaseToken: "runner-secret"}}
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("X-HyperCDR-Release-Token", tc.token)
			rec := httptest.NewRecorder()
			r.withPlatformAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d; want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestOrdinaryAccountNamedReleasePipelineCannotBypassSystemAdmin(t *testing.T) {
	repo := newTestStore(t)
	user, err := repo.CreateUser(store.DefaultTenantID, "release-pipeline", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.SetUserPassword(user.ID, "test-password", false); err != nil || !found {
		t.Fatalf("activate account: %v", err)
	}
	session, err := repo.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := &Router{store: repo, identityProvider: storeIdentityProvider{store: repo}, logger: slog.Default()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/releases", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()
	r.withPlatformAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "system_administrator_required") {
		t.Fatalf("ordinary account bypassed system-admin: %d", w.Code)
	}
}
