package httpserver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestCommunityProductInfoIsPublic(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), store.NewMemoryStore()).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/product-info", nil),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("GET product info status = %d, want %d", response.Code, http.StatusOK)
	}
	var result ProductInfo
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode product info: %v", err)
	}
	if result.Product != "HyperCDR" || result.Edition != "community" {
		t.Fatalf("product info = %#v, want HyperCDR community", result)
	}
}

func TestCustomProductInfoIsReturnedUnchanged(t *testing.T) {
	want := ProductInfo{
		Product:      "HyperCDR",
		Edition:      "enterprise",
		Capabilities: map[string]any{"enterpriseOverview": map[string]any{"enabled": true}},
		License:      map[string]any{"mode": "development", "status": "active"},
	}
	response := httptest.NewRecorder()
	NewRouterWithProductInfo(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), store.NewMemoryStore(), want).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/product-info", nil),
	)

	if response.Code != http.StatusOK {
		t.Fatalf("GET custom product info status = %d, want %d", response.Code, http.StatusOK)
	}
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode custom product info: %v", err)
	}
	if result["edition"] != "enterprise" {
		t.Fatalf("edition = %v, want enterprise", result["edition"])
	}
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok || capabilities["enterpriseOverview"] == nil {
		t.Fatalf("capabilities = %#v, want enterpriseOverview", result["capabilities"])
	}
}
