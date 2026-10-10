package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticContractsCoverMountedRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		if !strings.Contains(route.Pattern, "/diagnostic-log") && !strings.Contains(route.Pattern, "/logs/collect") && !strings.Contains(route.Pattern, "/logs/search") {
			continue
		}
		count++
		operation := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}, "parameters": []any{}}
		if !applyDiagnosticPayloadContract(route.Pattern, operation) {
			t.Fatalf("missing %s", route.Pattern)
		}
		if _, ok := operation["responses"].(map[string]any)["2XX"]; ok {
			t.Fatal("placeholder success remains")
		}
	}
	if count != 5 {
		t.Fatalf("coverage %d", count)
	}
}

func TestDiagnosticContractActualEmptyListAndTextExport(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	r := &Router{store: repo}
	for _, export := range []bool{false, true} {
		req := tenantRequest(httptest.NewRequest("GET", "/api/v1/diagnostic-logs?source=platform&limit=2", nil), actor)
		w := httptest.NewRecorder()
		if export {
			r.exportDiagnosticLogs(w, req)
		} else {
			r.listDiagnosticLogs(w, req)
		}
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if export && !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
			t.Fatal("export is not text")
		}
		if !export && !strings.Contains(w.Body.String(), `"items":[]`) {
			t.Fatal("empty collection mismatch")
		}
	}
}

func TestDiagnosticContractCachedSearchAndOfflineCollection(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "diagnostic-contract")
	r := &Router{store: repo, hub: newSessionHub()}
	now := time.Now().UTC()
	from := now.Add(-2 * time.Hour)
	if _, err := repo.UpsertClusterLogCoverage(store.ClusterLogCoverageInput{ClusterID: cluster.ID, TenantID: actor.TenantID, Component: "velero", CoveredFrom: from, CoveredTo: now, CollectedAt: now}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(clusterLogSearchRequest{Component: "velero", From: from.Add(time.Minute), To: now.Add(-time.Minute)})
	req := tenantRequest(httptest.NewRequest("POST", "/api/v1/clusters/"+cluster.ID+"/logs/search", strings.NewReader(string(body))), actor)
	req.SetPathValue("id", cluster.ID)
	w := httptest.NewRecorder()
	r.searchClusterLogs(w, req)
	if w.Code != 200 {
		t.Fatalf("cached search %d %s", w.Code, w.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	validateWireObject(t, response, wireSchema(reflect.TypeFor[clusterLogCachedResponse]()))
	if response["collected"] != false {
		t.Fatal("cached search unexpectedly collected")
	}
	req = tenantRequest(httptest.NewRequest("POST", "/api/v1/clusters/"+cluster.ID+"/logs/collect", strings.NewReader(`{"component":"velero"}`)), actor)
	req.SetPathValue("id", cluster.ID)
	w = httptest.NewRecorder()
	r.collectClusterLogs(w, req)
	if w.Code != 409 {
		t.Fatalf("offline collection %d %s", w.Code, w.Body.String())
	}
}

func TestDiagnosticContractsActualTenantIsolation(t *testing.T) {
	repo := newTestStore(t)
	admin := testAdmin(t, repo)
	foreignTenant, err := repo.CreateTenant(store.TenantInput{Name: "diagnostic-foreign", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreignCluster := testTenantCluster(t, repo, foreignTenant.ID, "diagnostic-foreign")
	ownCluster := testTenantCluster(t, repo, admin.TenantID, "diagnostic-own")
	for _, cluster := range []store.Cluster{ownCluster, foreignCluster} {
		if _, err := repo.CreateDiagnosticLog(store.DiagnosticLogInput{TenantID: cluster.TenantID, Scope: "tenant", Level: "info", Component: "velero", ClusterID: cluster.ID, Message: cluster.Name, EventAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	user, err := repo.CreateUser(admin.TenantID, "diagnostic-operator@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SetUserPassword(user.ID, "test-password", false); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(config.Config{}, slog.Default(), repo)
	for _, item := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/v1/diagnostic-logs?tenantId=" + foreignTenant.ID + "&source=cluster", "", 200},
		{"GET", "/api/v1/diagnostic-log-sources", "", 200},
		{"GET", "/api/v1/diagnostic-logs/export?source=cluster", "", 200},
		{"GET", "/api/v1/diagnostic-logs?clusterId=" + foreignCluster.ID, "", 403},
		{"POST", "/api/v1/clusters/" + foreignCluster.ID + "/logs/search", `{"component":"velero"}`, 404},
		{"POST", "/api/v1/clusters/" + foreignCluster.ID + "/logs/collect", `{"component":"velero"}`, 404},
	} {
		req := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		req.Header.Set("Authorization", "Bearer "+session.Token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != item.status {
			t.Fatalf("%s: %d %s", item.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), foreignCluster.Name) || strings.Contains(w.Body.String(), foreignCluster.ID) {
			t.Fatalf("foreign diagnostic data leaked: %s", w.Body.String())
		}
	}
}
