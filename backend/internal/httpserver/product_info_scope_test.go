package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestProductInfoUsesAuthenticatedLicenseScope(t *testing.T) {
	r := &Router{productInfo: ProductInfo{Edition: "enterprise", License: map[string]string{"detail": "platform secret"}}}
	r.scopedLicenseProvider = func(_ context.Context, p EditionPrincipal) (any, error) {
		if p.TenantID != "organization-a" || p.SystemAdmin {
			t.Fatalf("wrong principal: %+v", p)
		}
		return map[string]string{"detail": "organization allocation"}, nil
	}
	req := httptest.NewRequest("GET", "/api/v1/product-info?tenantId=organization-b", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "user-a", TenantID: "organization-a"}))
	w := httptest.NewRecorder()
	r.getProductInfo(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "organization allocation") || strings.Contains(w.Body.String(), "platform secret") {
		t.Fatalf("response=%s", w.Body.String())
	}
	r.scopedLicenseProvider = func(context.Context, EditionPrincipal) (any, error) { return nil, errors.New("unavailable") }
	w = httptest.NewRecorder()
	r.getProductInfo(w, req)
	if w.Code != 503 || strings.Contains(w.Body.String(), "platform secret") {
		t.Fatalf("unsafe fallback: %s", w.Body.String())
	}
}

func TestScopedProductMetadataRequiresAuthentication(t *testing.T) {
	r := &Router{}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	req := httptest.NewRequest("GET", "/api/v1/product-info", nil)
	w := httptest.NewRecorder()
	r.withPlatformAuth(next).ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("community metadata changed: %d", w.Code)
	}
	r.scopedLicenseProvider = func(context.Context, EditionPrincipal) (any, error) { return nil, nil }
	w = httptest.NewRecorder()
	r.withPlatformAuth(next).ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("public edition discovery status=%d", w.Code)
	}
}

func TestAnonymousProductInfoOmitsScopedLicenseDetails(t *testing.T) {
	r := &Router{productInfo: ProductInfo{Edition: "enterprise", License: map[string]string{"detail": "private capacity"}}, scopedLicenseProvider: func(context.Context, EditionPrincipal) (any, error) {
		t.Fatal("anonymous scoped provider called")
		return nil, nil
	}}
	w := httptest.NewRecorder()
	r.getProductInfo(w, httptest.NewRequest("GET", "/api/v1/product-info", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "private capacity") || !strings.Contains(w.Body.String(), "enterprise") {
		t.Fatalf("unsafe anonymous discovery: %s", w.Body.String())
	}
}
