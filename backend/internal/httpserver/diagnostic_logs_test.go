package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestClusterLogSearchStartsAtRequestedBeginning(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	requestedFrom := now.Add(-2 * time.Hour)
	if got := clusterLogCollectFrom(requestedFrom, now); !got.Equal(requestedFrom) {
		t.Fatalf("collect from = %v, want requested beginning %v", got, requestedFrom)
	}
}

func TestClusterLogSearchCapsUnavailableHistory(t *testing.T) {
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	want := now.Add(-24 * time.Hour)
	if got := clusterLogCollectFrom(now.Add(-48*time.Hour), now); !got.Equal(want) {
		t.Fatalf("collect from = %v, want retention boundary %v", got, want)
	}
}

func TestDiagnosticLogFilterForcesTenantScope(t *testing.T) {
	r := &Router{store: store.NewMemoryStore()}
	req := httptest.NewRequest("GET", "/api/v1/diagnostic-logs?tenantId=another-tenant&scope=system", nil)
	user := store.User{ID: "user-a", TenantID: "tenant-a", Role: "admin"}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user))
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if filter.TenantID != "tenant-a" || filter.Scope != "tenant" {
		t.Fatalf("tenant scope was not enforced: %#v", filter)
	}
}

func TestAccessLogKeepsFailureReason(t *testing.T) {
	repo := store.NewMemoryStore()
	r := &Router{store: repo, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := r.withAccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "storage_endpoint_invalid", "message": "Storage endpoint is unreachable"})
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/storage-repositories/test", nil))
	items, err := repo.ListDiagnosticLogs(store.DiagnosticLogFilter{Source: "platform"})
	if err != nil || len(items) != 1 {
		t.Fatalf("unexpected logs: %#v err=%v", items, err)
	}
	if items[0].ErrorCode != "storage_endpoint_invalid" || items[0].Message != "POST /api/v1/storage-repositories/test failed: Storage endpoint is unreachable" {
		t.Fatalf("failure reason was not retained: %#v", items[0])
	}
}

func TestSystemAdminCanSelectAllTenants(t *testing.T) {
	r := &Router{store: store.NewMemoryStore()}
	req := httptest.NewRequest("GET", "/api/v1/diagnostic-logs", nil)
	user := store.User{ID: "admin", TenantID: store.DefaultTenantID, SystemAdmin: true}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user))
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		t.Fatal(err)
	}
	if filter.TenantID != "" || filter.Scope != "" {
		t.Fatalf("system admin all-tenant filter changed: %#v", filter)
	}
}

func TestClusterSourceRejectsSystemScope(t *testing.T) {
	r := &Router{store: store.NewMemoryStore()}
	req := httptest.NewRequest("GET", "/api/v1/diagnostic-logs?source=cluster&scope=system", nil)
	user := store.User{ID: "admin", TenantID: store.DefaultTenantID, SystemAdmin: true}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user))
	if _, err := r.diagnosticLogFilter(req); err == nil {
		t.Fatal("cluster source accepted system scope")
	}
}
