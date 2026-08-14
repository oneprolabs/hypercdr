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
		cfg: config.Config{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), mux: http.NewServeMux(), store: store.NewMemoryStore(),
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
