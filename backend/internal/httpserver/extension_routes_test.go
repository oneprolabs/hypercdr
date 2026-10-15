package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestExtensionRouteReceivesAuthenticatedPrincipal(t *testing.T) {
	var principal EditionPrincipal
	router := &Router{
		cfg: config.Config{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), mux: http.NewServeMux(), store: newTestStore(t),
		extensionRoutes: []ExtensionRoute{{Pattern: "GET /api/v1/enterprise/check", Handler: func(w http.ResponseWriter, _ *http.Request, got EditionPrincipal) {
			principal = got
			w.WriteHeader(http.StatusNoContent)
		}}},
	}
	router.mountExtensionRoutes()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/enterprise/check", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestUserContextKey{}, store.User{ID: "user-1", TenantID: "tenant-1", Email: "admin@example.com", Role: "admin", SystemAdmin: true}))
	response := httptest.NewRecorder()
	router.mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || principal.ID != "user-1" || principal.TenantID != "tenant-1" || !principal.SystemAdmin {
		t.Fatalf("response=%d principal=%#v", response.Code, principal)
	}
}

func TestExtensionRouteRejectsMissingPrincipal(t *testing.T) {
	router := &Router{mux: http.NewServeMux(), extensionRoutes: []ExtensionRoute{{Pattern: "GET /api/v1/enterprise/check", Handler: func(http.ResponseWriter, *http.Request, EditionPrincipal) { t.Fatal("handler called") }}}}
	router.mountExtensionRoutes()
	response := httptest.NewRecorder()
	router.mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/enterprise/check", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestPublicExtensionIsExactAndMethodScoped(t *testing.T) {
	called := 0
	router := &Router{mux: http.NewServeMux(), extensionRoutes: []ExtensionRoute{{Public: true, Pattern: "POST /api/v1/enterprise/auth/google/start", Handler: func(w http.ResponseWriter, _ *http.Request, p EditionPrincipal) {
		called++
		if p.ID != "" || p.TenantID != "" {
			t.Fatal("public route received principal")
		}
		w.WriteHeader(204)
	}}}}
	router.mountExtensionRoutes()
	handler := router.withPlatformAuth(router.mux)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/api/v1/enterprise/auth/google/start", 204},
		{"GET", "/api/v1/enterprise/auth/google/start", 401},
		{"POST", "/api/v1/enterprise/auth/google/start/extra", 401},
		{"POST", "/api/v1/enterprise/auth/unknown", 401},
		{"GET", "/api/v1/clusters", 401},
		{"GET", "/api/v1/enterprise/invitations", 401},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
	if called != 1 {
		t.Fatalf("public handler calls %d", called)
	}
}
func TestPublicExtensionsCannotExposeBusinessOrWildcardRoutes(t *testing.T) {
	for _, pattern := range []string{"GET /api/v1/clusters", "GET /api/v1/enterprise/auth/{path...}", "DELETE /api/v1/enterprise/auth/account", "GET /api/v1/enterprise/auth/test?query=1"} {
		t.Run(pattern, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("unsafe public route accepted")
				}
			}()
			r := &Router{mux: http.NewServeMux(), extensionRoutes: []ExtensionRoute{{Public: true, Pattern: pattern, Handler: func(http.ResponseWriter, *http.Request, EditionPrincipal) {}}}}
			r.mountExtensionRoutes()
		})
	}
}

func TestAuthenticationQuerySecretsAreNotLogged(t *testing.T) {
	for _, path := range []string{"/api/v1/enterprise/auth/google/callback?code=secret&state=secret", "/api/v1/auth/reset-password?token=secret"} {
		if got := accessLogQuery(httptest.NewRequest("GET", path, nil)); got != "[redacted]" {
			t.Fatal("authentication query leaked", got)
		}
	}
	if got := accessLogQuery(httptest.NewRequest("GET", "/api/v1/clusters?limit=10", nil)); got != "limit=10" {
		t.Fatal("ordinary query lost")
	}
}
