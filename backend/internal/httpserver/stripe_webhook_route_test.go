package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStripeWebhookPublicRouteIsExact(t *testing.T) {
	router := &Router{mux: http.NewServeMux(), extensionRoutes: []ExtensionRoute{{Pattern: "POST /api/v1/saas/subscription/webhook", Public: true, Handler: func(w http.ResponseWriter, _ *http.Request, _ EditionPrincipal) { w.WriteHeader(204) }}}}
	router.mountExtensionRoutes()
	handler := router.withPlatformAuth(router.mux)
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/api/v1/saas/subscription/webhook", 204},
		{"GET", "/api/v1/saas/subscription/webhook", 401},
		{"POST", "/api/v1/saas/subscription/webhook/other", 401},
		{"POST", "/api/v1/saas/subscription/checkout", 401},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(test.method, test.path, nil))
		if w.Code != test.status {
			t.Errorf("%s %s: %d", test.method, test.path, w.Code)
		}
	}
}

func TestStripeWebhookCannotExposeOtherBillingRoutes(t *testing.T) {
	for _, pattern := range []string{"GET /api/v1/saas/subscription/webhook", "POST /api/v1/saas/subscription/checkout", "POST /api/v1/saas/subscription/{operation}"} {
		t.Run(pattern, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("unsafe public billing route accepted")
				}
			}()
			router := &Router{mux: http.NewServeMux(), extensionRoutes: []ExtensionRoute{{Pattern: pattern, Public: true, Handler: func(http.ResponseWriter, *http.Request, EditionPrincipal) {}}}}
			router.mountExtensionRoutes()
		})
	}
}
