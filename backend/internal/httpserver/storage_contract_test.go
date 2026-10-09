package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStorageContractsCoverMountedRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		_, path, _ := strings.Cut(route.Pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/storage-repositories") {
			continue
		}
		count++
		c, _, ok := storagePayloadContract(route.Pattern)
		if !ok || c.Response == nil {
			t.Fatalf("missing %s", route.Pattern)
		}
		op := map[string]any{"responses": map[string]any{"2XX": map[string]any{}}}
		applyStoragePayloadContract(route.Pattern, op)
		if _, found := wireSchema(c.Response)["properties"].(map[string]any)["secret"]; found {
			t.Fatal("secret in output")
		}
		if route.Pattern == "POST /api/v1/storage-repositories/{id}/sync" {
			if _, found := op["responses"].(map[string]any)["202"]; !found {
				t.Fatal("missing queued response")
			}
		}
	}
	if count != 7 {
		t.Fatalf("coverage %d", count)
	}
}

func TestStorageActualResponsesMatchContracts(t *testing.T) {
	repo := newTestStore(t)
	r := &Router{store: repo, logger: slog.Default(), hub: newSessionHub()}
	actor := testAdmin(t, repo)
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(204) }))
	defer probe.Close()
	body := `{"name":"contract-storage","type":"minio","bucket":"test","endpoint":"` + probe.URL + `","accessKey":"private-access","secretKey":"private-secret"}`
	run := func(pattern, input, id string, handler http.HandlerFunc, status int) map[string]any {
		t.Helper()
		method, path, _ := strings.Cut(pattern, " ")
		req := tenantRequest(httptest.NewRequest(method, strings.ReplaceAll(path, "{id}", id), strings.NewReader(input)), actor)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != status {
			t.Fatalf("%s: %d %s", pattern, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-secret") || strings.Contains(w.Body.String(), "private-access") {
			t.Fatal("credentials leaked")
		}
		var value map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		c, _, _ := storagePayloadContract(pattern)
		validateWireObject(t, value, wireSchema(c.Response))
		return value
	}
	created := run("POST /api/v1/storage-repositories", body, "", r.createStorageRepository, 201)
	id := created["id"].(string)
	run("GET /api/v1/storage-repositories", "", "", r.listStorageRepositories, 200)
	run("POST /api/v1/storage-repositories/test", body, "", r.testStorageRepositoryDraft, 200)
	run("POST /api/v1/storage-repositories/{id}/test", "", id, r.testStorageRepository, 200)
	run("PATCH /api/v1/storage-repositories/{id}", body, id, r.updateStorageRepository, 200)
	disposable := run("POST /api/v1/storage-repositories", strings.Replace(body, "contract-storage", "disposable-storage", 1), "", r.createStorageRepository, 201)
	run("DELETE /api/v1/storage-repositories/{id}", "", disposable["id"].(string), r.deleteStorageRepository, 200)
	token, err := repo.CreateAgentToken(store.DefaultTenantID, actor.ID, "sync-contract", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := repo.RegisterCluster(store.RegisterClusterInput{Token: token.Token, ClusterName: "sync-contract"})
	if err != nil {
		t.Fatal(err)
	}
	req := tenantRequest(httptest.NewRequest("POST", "/api/v1/storage-repositories/"+id+"/sync", strings.NewReader(`{"clusterId":"`+cluster.ID+`"}`)), actor)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	r.syncStorageRepository(w, req)
	if w.Code != 202 {
		t.Fatalf("sync: %d %s", w.Code, w.Body.String())
	}
	var queued map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	validateWireObject(t, queued, wireSchema(reflect.TypeFor[storageSyncQueuedResponse]()))
	// The sync created a persisted binding, so deletion correctly reports in-use.
	if strings.Contains(w.Body.String(), "private-secret") {
		t.Fatal("sync leaked secret")
	}

}
