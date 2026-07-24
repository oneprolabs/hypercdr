package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestAuditLogRecordsAuthenticatedMutationWithoutRequestSecrets(t *testing.T) {
	repo := store.NewMemoryStore()
	router := &Router{store: repo, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := router.withAuditLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "11111111-1111-1111-1111-111111111111", "name": "daily-policy"})
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", strings.NewReader(`{"name":"daily-policy","password":"must-not-be-recorded"}`))
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "00000000-0000-0000-0000-00000000a001", Email: "admin"}))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	items, err := repo.ListAuditLogs(10, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected one audit log, items=%#v err=%v", items, err)
	}
	item := items[0]
	if item.Action != "Create Policy" || item.Result != "Success" || item.Actor != "admin" || item.ResourceName != "daily-policy" {
		t.Fatalf("unexpected audit record: %#v", item)
	}
	if strings.Contains(strings.TrimSpace(item.Message), "must-not-be-recorded") {
		t.Fatalf("request secret leaked into audit record: %#v", item)
	}
}

func TestAuditLogRecordsFailureAndSkipsReads(t *testing.T) {
	repo := store.NewMemoryStore()
	router := &Router{store: repo, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := router.withAuditLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "policy_in_use", "message": "Policy is still in use."})
	}))
	user := store.User{ID: "00000000-0000-0000-0000-00000000a001", Email: "admin"}
	failed := httptest.NewRequest(http.MethodDelete, "/api/v1/policies/22222222-2222-2222-2222-222222222222", nil)
	failed = failed.WithContext(context.WithValue(failed.Context(), requestUserContextKey{}, user))
	handler.ServeHTTP(httptest.NewRecorder(), failed)
	read := httptest.NewRequest(http.MethodGet, "/api/v1/policies", nil)
	read = read.WithContext(context.WithValue(read.Context(), requestUserContextKey{}, user))
	handler.ServeHTTP(httptest.NewRecorder(), read)
	internal := httptest.NewRequest(http.MethodPost, "/api/v1/agent-tokens", nil)
	internal = internal.WithContext(context.WithValue(internal.Context(), requestUserContextKey{}, user))
	handler.ServeHTTP(httptest.NewRecorder(), internal)

	items, _ := repo.ListAuditLogs(10, 0)
	if len(items) != 1 || items[0].Result != "Failed" || items[0].Message != "Policy is still in use." || items[0].Action != "Delete Policy" {
		t.Fatalf("unexpected failure audit record: %#v", items)
	}
}
