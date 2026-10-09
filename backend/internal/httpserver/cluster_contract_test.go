package httpserver

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type clusterDisconnectStore struct {
	store.Store
	once       sync.Once
	disconnect func()
}

func (s *clusterDisconnectStore) ListTasks(clusterID string) ([]store.Task, error) {
	items, err := s.Store.ListTasks(clusterID)
	if err == nil {
		s.once.Do(s.disconnect)
	}
	return items, err
}

func clusterContractSocket(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, req, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	server := <-accepted
	t.Cleanup(func() { client.Close(); server.Close(); s.Close() })
	return server, client
}

func validateClusterResponse(t *testing.T, pattern string, status int, body any) {
	t.Helper()
	op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
	if !applyClusterPayloadContract(pattern, op) {
		t.Fatalf("missing contract %s", pattern)
	}
	responses := op["responses"].(map[string]any)
	schema := responses[strconv.Itoa(status)].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	validateWireObject(t, body, schema)
}

func TestClusterContractsCoverMountedOperations(t *testing.T) {
	r := &Router{mux: http.NewServeMux(), productInfo: ProductInfo{Edition: "community"}}
	r.routes()
	count := 0
	for _, route := range r.routeContracts {
		if !strings.Contains(route.Pattern, "/api/v1/clusters") || strings.Contains(route.Pattern, "/logs/") {
			continue
		}
		count++
		op := map[string]any{"parameters": []any{}, "responses": map[string]any{"2XX": map[string]any{}}}
		if !applyClusterPayloadContract(route.Pattern, op) {
			t.Fatalf("uncovered %s", route.Pattern)
		}
		if _, exists := op["responses"].(map[string]any)["2XX"]; exists {
			t.Fatal("placeholder success remains")
		}
	}
	if count != 11 {
		t.Fatalf("coverage %d", count)
	}
}

func TestClusterActualResponsesAndForeignMutations(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	own := testTenantCluster(t, repo, actor.TenantID, "own-cluster")
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "foreign-cluster", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := testTenantCluster(t, repo, tenant.ID, "foreign-cluster")
	r := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), inventory: map[string]inventoryRequestStatus{}}
	run := func(pattern, path, body, id, requestID string, handler http.HandlerFunc, status int) map[string]any {
		t.Helper()
		method, _, _ := strings.Cut(pattern, " ")
		req := tenantRequest(httptest.NewRequest(method, path, strings.NewReader(body)), actor)
		req.SetPathValue("id", id)
		req.SetPathValue("requestId", requestID)
		contract := describeRoute(pattern)
		if contract.Resource != "" {
			handler = r.tenantGuard(contract.Resource, handler)
		}
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != status {
			t.Fatalf("%s: %d %s", pattern, w.Code, w.Body.String())
		}
		var value map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		validateClusterResponse(t, pattern, status, value)
		return value
	}
	for _, view := range []string{"", "?view=summary"} {
		value := run("GET /api/v1/clusters", "/api/v1/clusters"+view, "", "", "", r.listClusters, 200)
		items := value["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != own.ID {
			t.Fatalf("foreign cluster in collection: %v", items)
		}
	}
	run("PATCH /api/v1/clusters/{id}", "/api/v1/clusters/"+own.ID, `{"name":"renamed"}`, own.ID, "", r.updateCluster, 200)
	run("POST /api/v1/clusters/{id}/default", "/api/v1/clusters/"+own.ID+"/default", "", own.ID, "", r.setDefaultCluster, 200)
	run("GET /api/v1/clusters/{id}/unregister/precheck", "/api/v1/clusters/"+own.ID+"/unregister/precheck", "", own.ID, "", r.precheckUnregisterCluster, 200)
	run("POST /api/v1/clusters/{id}/inventory/request", "/api/v1/clusters/"+own.ID+"/inventory/request", `{"requestId":"offline-request"}`, own.ID, "", r.requestClusterInventory, 409)
	run("GET /api/v1/clusters/{id}/inventory/requests/{requestId}", "/api/v1/clusters/"+own.ID+"/inventory/requests/offline-request", "", own.ID, "offline-request", r.getClusterInventoryRequest, 200)
	r.setInventoryRequestStatus(inventoryRequestStatus{RequestID: "timed-out", ClusterID: own.ID, Status: "pending", Scope: "summary", CreatedAt: time.Now().Add(-time.Minute - time.Second)})
	value := run("GET /api/v1/clusters/{id}/inventory/requests/{requestId}", "/api/v1/clusters/"+own.ID+"/inventory/requests/timed-out", "", own.ID, "timed-out", r.getClusterInventoryRequest, 200)
	if value["status"] != "timeout" {
		t.Fatal("pending inventory did not time out")
	}
	run("GET /api/v1/clusters/{id}/inventory/requests/{requestId}", "/api/v1/clusters/"+own.ID+"/inventory/requests/restarted", "", own.ID, "restarted", r.getClusterInventoryRequest, 404)
	for _, item := range []struct {
		pattern, body string
		handler       http.HandlerFunc
	}{
		{"PATCH /api/v1/clusters/{id}", `{"name":"foreign-mutation"}`, r.updateCluster},
		{"POST /api/v1/clusters/{id}/default", "", r.setDefaultCluster},
		{"POST /api/v1/clusters/{id}/unregister", `{}`, r.unregisterCluster},
		{"POST /api/v1/clusters/{id}/force-cleanup", "", r.forceCleanupCluster},
		{"DELETE /api/v1/clusters/{id}", "", r.deleteCluster},
		{"POST /api/v1/clusters/{id}/agent/upgrade", `{}`, r.upgradeClusterAgent},
		{"POST /api/v1/clusters/{id}/velero/upgrade", `{}`, r.upgradeClusterVelero},
	} {
		run(item.pattern, "/api/v1/clusters/"+foreign.ID+"?force=true", item.body, foreign.ID, "", item.handler, 404)
	}
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	for _, cluster := range clusters {
		if cluster.ID == foreign.ID && cluster.Name != "foreign-cluster" {
			t.Fatal("foreign mutation persisted")
		}
	}
	removable := testTenantCluster(t, repo, actor.TenantID, "removable")
	conn, client := clusterContractSocket(t)
	r.hub.set(removable.ID, conn)
	sent := run("POST /api/v1/clusters/{id}/inventory/request", "/api/v1/clusters/"+removable.ID+"/inventory/request", `{"requestId":"sent-request","scope":"namespaceResources","namespace":"default"}`, removable.ID, "", r.requestClusterInventory, 202)
	if sent["scope"] != "capabilities" {
		t.Fatal("namespace scope not normalized")
	}
	var inventory protocol.Message[protocol.InventoryRequestPayload]
	if err := client.ReadJSON(&inventory); err != nil {
		t.Fatal(err)
	}
	r.completeInventoryRequest(removable.ID, protocol.InventoryReportPayload{RequestID: "sent-request"})
	run("GET /api/v1/clusters/{id}/inventory/requests/{requestId}", "/api/v1/clusters/"+removable.ID+"/inventory/requests/sent-request", "", removable.ID, "sent-request", r.getClusterInventoryRequest, 200)
	run("POST /api/v1/clusters/{id}/unregister", "/api/v1/clusters/"+removable.ID+"/unregister", `{}`, removable.ID, "", r.unregisterCluster, 202)
	var dispatch protocol.Message[protocol.TaskDispatchPayload]
	if err := client.ReadJSON(&dispatch); err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.Type != "unregister" {
		t.Fatalf("unexpected unregister dispatch: %+v", dispatch)
	}
	conn.Close()
	run("POST /api/v1/clusters/{id}/inventory/request", "/api/v1/clusters/"+removable.ID+"/inventory/request", `{"requestId":"dispatch-failed"}`, removable.ID, "", r.requestClusterInventory, 202)
	disconnected := testTenantCluster(t, repo, actor.TenantID, "disconnect-during-precheck")
	disconnectConn, _ := clusterContractSocket(t)
	r.hub.set(disconnected.ID, disconnectConn)
	r.store = &clusterDisconnectStore{Store: repo, disconnect: func() { r.hub.remove(disconnected.ID, disconnectConn) }}
	queued := run("POST /api/v1/clusters/{id}/unregister", "/api/v1/clusters/"+disconnected.ID+"/unregister", `{}`, disconnected.ID, "", r.unregisterCluster, 202)
	r.store = repo
	if queued["warning"] == nil || queued["task"] == nil {
		t.Fatal("disconnect race lost persisted queued task/warning")
	}
	cleanable := testTenantCluster(t, repo, actor.TenantID, "cleanable")
	run("POST /api/v1/clusters/{id}/force-cleanup", "/api/v1/clusters/"+cleanable.ID+"/force-cleanup", "", cleanable.ID, "", r.forceCleanupCluster, 200)
	deletable := testTenantCluster(t, repo, actor.TenantID, "deletable")
	run("DELETE /api/v1/clusters/{id}", "/api/v1/clusters/"+deletable.ID+"?force=true", "", deletable.ID, "", r.deleteCluster, 200)
}

func TestClusterEmptyCollectionMatchesBothViews(t *testing.T) {
	r := &Router{store: newTestStore(t), hub: newSessionHub(), logger: slog.Default()}
	for _, view := range []string{"", "?view=summary"} {
		w := httptest.NewRecorder()
		r.listClusters(w, httptest.NewRequest("GET", "/api/v1/clusters"+view, nil))
		var body any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		validateClusterResponse(t, "GET /api/v1/clusters", 200, body)
	}
}

func TestInventoryRequestIdentityIsClusterScoped(t *testing.T) {
	r := &Router{inventory: map[string]inventoryRequestStatus{}}
	for _, cluster := range []string{"cluster-a", "cluster-b"} {
		r.setInventoryRequestStatus(inventoryRequestStatus{ClusterID: cluster, RequestID: "shared-request", Status: "pending", Scope: "summary", CreatedAt: time.Now()})
	}
	r.completeInventoryRequest("cluster-a", protocol.InventoryReportPayload{RequestID: "shared-request"})
	r.failInventoryRequest("cluster-b", protocol.MessageErrorPayload{RequestID: "shared-request", ErrorCode: "B_FAILURE", Message: "B only"})
	a, aok := r.getInventoryRequestStatus("cluster-a", "shared-request")
	b, bok := r.getInventoryRequestStatus("cluster-b", "shared-request")
	if !aok || !bok || a.Status != "succeeded" || b.Status != "failed" || a.ErrorCode != "" || b.ErrorCode != "B_FAILURE" {
		t.Fatalf("cluster requests collided: %+v %+v", a, b)
	}
	if _, ok := r.getInventoryRequestStatus("cluster-c", "shared-request"); ok {
		t.Fatal("foreign cluster retrieved request")
	}
}

func TestClusterVeleroUpgradeResponseMatchesContract(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "velero-contract")
	if _, _, err := repo.UpdateHeartbeat(store.HeartbeatInput{ClusterID: cluster.ID, VeleroVersion: "v1", VeleroImageDigest: "sha256:old", VeleroNodeAgentImageDigest: "sha256:old"}); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]store.ReleaseComponent{}
	for _, component := range []string{"velero", "velero-plugin-for-aws", "velero-plugin-for-microsoft-azure", "velero-plugin-for-gcp"} {
		manifest[component] = store.ReleaseComponent{Version: "v2", Image: "registry.example/" + component + ":v2", ImageDigest: "sha256:" + strings.Repeat("a", 64)}
	}
	if _, err := repo.UpsertPlatformRelease(store.PlatformReleaseInput{Version: "v2", Status: "active", APIImage: "registry.example/api:v2", FrontendImage: "registry.example/frontend:v2", APIImageDigest: "sha256:api", FrontendImageDigest: "sha256:frontend", ComponentManifest: manifest}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), workerContext: ctx, cfg: config.Config{AgentNamespace: "hypercdr-agent"}}
	t.Cleanup(func() { cancel(); r.workers.Wait() })
	conn, client := clusterContractSocket(t)
	r.hub.set(cluster.ID, conn)
	request := tenantRequest(httptest.NewRequest("POST", "/api/v1/clusters/"+cluster.ID+"/velero/upgrade", strings.NewReader(`{"repair":true}`)), actor)
	request.SetPathValue("id", cluster.ID)
	w := httptest.NewRecorder()
	r.upgradeClusterVelero(w, request)
	if w.Code != 202 {
		t.Fatalf("velero repair: %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	validateClusterResponse(t, "POST /api/v1/clusters/{id}/velero/upgrade", 202, body)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	var dispatch protocol.Message[protocol.TaskDispatchPayload]
	if err := client.ReadJSON(&dispatch); err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.Type != "velero-upgrade" || dispatch.Payload.TaskID != body["id"] {
		t.Fatalf("wrong persisted task dispatched: %+v", dispatch)
	}
	r.workers.Wait()
}
