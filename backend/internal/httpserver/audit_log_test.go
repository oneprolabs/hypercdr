package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAuditLogRecordsAuthenticatedMutationWithoutRequestSecrets(t *testing.T) {
	repo := newTestStore(t)
	router := &Router{store: repo, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := router.withAuditLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "11111111-1111-1111-1111-111111111111", "name": "daily-policy"})
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", strings.NewReader(`{"name":"daily-policy","password":"must-not-be-recorded"}`))
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, testAdmin(t, repo)))
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
	repo := newTestStore(t)
	router := &Router{store: repo, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := router.withAuditLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "policy_in_use", "message": "Policy is still in use."})
	}))
	user := testAdmin(t, repo)
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

func TestAuditLogTenantFilterPrecedesGlobalHistoryAndPagination(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	var own []string
	for i := 0; i < 3; i++ {
		item, err := repo.CreateAuditLog(store.AuditLogInput{ActorID: actor.ID, Actor: actor.Email, Action: "Create Policy", ResourceName: fmt.Sprint(i)})
		if err != nil {
			t.Fatal(err)
		}
		own = append(own, item.ID)
	}
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "audit-foreign", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := repo.CreateUser(tenant.ID, "audit-foreign@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		if _, err := repo.CreateAuditLog(store.AuditLogInput{ActorID: foreign.ID, Action: "Create Policy"}); err != nil {
			t.Fatal(err)
		}
	}
	router := &Router{store: repo, logger: slog.Default()}
	for _, systemAdmin := range []bool{false, true} {
		actor.SystemAdmin = systemAdmin
		req := tenantRequest(httptest.NewRequest("GET", "/api/v1/audit-logs?limit=1&offset=1", nil), actor)
		w := httptest.NewRecorder()
		router.listAuditLogs(w, req)
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
		var body struct {
			Items []store.AuditLog `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		validateWireObject(t, map[string]any{"items": mustAuditJSON(t, body.Items)}, wireSchema(reflect.TypeFor[listResponse[store.AuditLog]]()))
		if len(body.Items) != 1 || body.Items[0].ID != own[1] || body.Items[0].TenantID != actor.TenantID {
			t.Fatalf("tenant page lost: %#v", body.Items)
		}
	}
}

func mustAuditJSON(t *testing.T, items []store.AuditLog) any {
	t.Helper()
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAuditLogContractCoversMountedRoute(t *testing.T) {
	router := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	router.routes()
	found := false
	for _, route := range router.routeContracts {
		if route.Pattern != "GET /api/v1/audit-logs" {
			continue
		}
		found = true
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}, "parameters": []any{}}
		if !applyAuditPayloadContract(route.Pattern, op) {
			t.Fatal("missing payload contract")
		}
		if _, ok := op["responses"].(map[string]any)["200"]; !ok {
			t.Fatal("missing success shape")
		}
	}
	if !found {
		t.Fatal("audit route missing")
	}
}
