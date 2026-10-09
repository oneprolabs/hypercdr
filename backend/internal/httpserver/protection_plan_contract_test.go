package httpserver

import (
	"context"
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProtectionPlanContractsCoverMountedRoutes(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		_, path, _ := strings.Cut(route.Pattern, " ")
		if !strings.HasPrefix(path, "/api/v1/protection-plans") || strings.HasSuffix(path, "/latest-sync") || strings.HasSuffix(path, "/latest-recovery") {
			continue
		}
		count++
		c, _, ok := protectionPlanPayloadContract(route.Pattern)
		if !ok || c.Response == nil {
			t.Fatal("missing plan contract", route.Pattern)
		}
		if c.Request != nil {
			if _, found := wireSchema(c.Request)["properties"].(map[string]any)["tenantId"]; found {
				t.Fatal("tenant input exposed")
			}
		}
	}
	if count != 5 {
		t.Fatalf("plan coverage %d", count)
	}
}

func TestProtectionPlanActualLifecycleMatchesContracts(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	workerContext, stop := context.WithCancel(context.Background())
	r := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workerContext: workerContext, stopWorkers: stop}
	h := &managedRouter{router: r}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	cluster := testTenantCluster(t, repo, store.DefaultTenantID, "plan-contract")
	app := seedSchedulerApplication(t, repo, cluster.ID, "demo")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: store.DefaultTenantID, Name: "plan-contract-storage", Type: "S3", Endpoint: "localhost:9000", Bucket: "test"})
	if err != nil {
		t.Fatal(err)
	}
	run := func(pattern, body, id string, handler http.HandlerFunc, status int) map[string]any {
		t.Helper()
		method, path, _ := strings.Cut(pattern, " ")
		req := tenantRequest(httptest.NewRequest(method, strings.ReplaceAll(path, "{id}", id), strings.NewReader(body)), actor)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != status {
			t.Fatalf("%s: %d %s", pattern, w.Code, w.Body.String())
		}
		var value map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		c, _, ok := protectionPlanPayloadContract(pattern)
		if !ok {
			t.Fatal("missing contract")
		}
		validateWireObject(t, value, wireSchema(c.Response))
		return value
	}
	created := run("POST /api/v1/protection-plans", `{"sourceClusterId":"`+cluster.ID+`","appId":"`+app.ID+`","storageRepoId":"`+storage.ID+`","scopeType":"all"}`, "", r.createProtectionPlan, 201)
	id := created["id"].(string)
	if created["tenantId"] != store.DefaultTenantID {
		t.Fatal("wrong tenant")
	}
	// Preserve the established non-null empty arrays on activation responses.
	for _, name := range []string{"appIds", "includedResources", "excludedResources", "preHooks", "postHooks"} {
		if _, ok := created[name].([]any); !ok {
			t.Fatal("array changed", name)
		}
	}
	run("GET /api/v1/protection-plans", "", "", r.listProtectionPlans, 200)
	run("POST /api/v1/protection-plans/{id}/activate", "", id, r.tenantGuard("plan", r.activateProtectionPlan), 202)
	configured := run("POST /api/v1/protection-plans/{id}/storage/reconfigure", "", id, r.tenantGuard("plan", r.reconfigureProtectionPlanStorage), 202)
	if _, ok := configured["storageTasks"].([]any); !ok {
		t.Fatal("storageTasks missing")
	}
	run("DELETE /api/v1/protection-plans/{id}", "", id, r.tenantGuard("plan", r.deleteProtectionPlan), 200)
}
