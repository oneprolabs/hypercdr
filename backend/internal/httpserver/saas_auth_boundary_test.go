package httpserver

import (
	"context"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifiedProfileEmailCannotBeReassigned(t *testing.T) {
	r := &Router{}
	req := httptest.NewRequest("PATCH", "/api/v1/auth/me", strings.NewReader(`{"email":"unverified@example.test","displayName":"Changed"}`))
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "user", Email: "verified@example.test"}))
	w := httptest.NewRecorder()
	r.updateCurrentUser(w, req)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "verified_email_immutable") {
		t.Fatalf("unverified email reassignment: %d %s", w.Code, w.Body.String())
	}
}
func TestSaaSPublicRoutesDoNotExposeProtectedOrWildcardEndpoints(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), extensionRoutes: []ExtensionRoute{{Public: true, Pattern: "POST /api/v1/saas/auth/signup/request", Handler: func(w http.ResponseWriter, _ *http.Request, p EditionPrincipal) {
		if p.ID != "" {
			t.Fatal("public request inherited a principal")
		}
		w.WriteHeader(202)
	}}}}
	r.mountExtensionRoutes()
	handler := r.withPlatformAuth(r.mux)
	for _, v := range []struct {
		method, path string
		status       int
	}{{"POST", "/api/v1/saas/auth/signup/request", 202}, {"GET", "/api/v1/saas/auth/signup/request", 401}, {"POST", "/api/v1/saas/auth/signup/request/extra", 401}, {"POST", "/api/v1/saas/invitations", 401}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(v.method, v.path, nil))
		if w.Code != v.status {
			t.Fatalf("%s %s: %d", v.method, v.path, w.Code)
		}
	}
}
