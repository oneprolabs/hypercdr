package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestCacheUsageReportDoesNotApplyResourceInventory(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "cache-usage-report")
	if _, ok, err := repo.ApplyInventory(store.InventoryInput{ClusterID: cluster.ID, NodeCount: 2, NamespaceCount: 1, Apps: []store.Application{{Namespace: "protected", Name: "protected"}}, CollectedAt: time.Now().UTC()}); err != nil || !ok {
		t.Fatalf("seed inventory: %v %v", ok, err)
	}
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), inventory: map[string]inventoryRequestStatus{}}
	server, client := clusterContractSocket(t)
	done := make(chan struct{})
	go func() { defer close(done); router.readAgentMessages(server, cluster.ID) }()
	now := time.Now().UTC()
	payload := protocol.InventoryReportPayload{Scope: "cache-usage", CacheUsage: map[string]any{"phase": "succeeded", "observedAt": now.Format(time.RFC3339Nano), "nodes": []any{}}, CollectedAt: now}
	if err := client.WriteJSON(protocol.Message[protocol.InventoryReportPayload]{Version: protocol.Version, MessageKind: protocol.MessageKindEvent, Type: protocol.MessageAgentInventoryReport, ClusterID: cluster.ID, AgentID: "test-agent", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	saved := false
	for time.Now().Before(deadline) {
		clusters, err := repo.ListClusters()
		if err != nil {
			t.Fatal(err)
		}
		for _, observed := range clusters {
			if observed.ID == cluster.ID && observed.CacheUsage != nil {
				if observed.NodeCount != 2 || observed.NamespaceCount != 1 {
					t.Fatal("cache report overwrote resource counts")
				}
				saved = true
			}
		}
		if saved {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	client.Close()
	<-done
	if !saved {
		t.Fatal("cache usage report was not persisted")
	}
	apps, err := repo.ListApplications(cluster.ID)
	if err != nil || len(apps) != 1 || apps[0].Namespace != "protected" {
		t.Fatalf("cache report altered applications: %+v %v", apps, err)
	}
}

func TestCacheUsageManualRefreshDeduplicatesAndUsesLongerTimeout(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "cache-usage-refresh")
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), inventory: map[string]inventoryRequestStatus{}}
	request := func() *httptest.ResponseRecorder {
		req := tenantRequest(httptest.NewRequest("POST", "/", strings.NewReader(`{"scope":"cache-usage"}`)), actor)
		req.SetPathValue("id", cluster.ID)
		response := httptest.NewRecorder()
		router.requestClusterInventory(response, req)
		return response
	}
	if response := request(); response.Code != 409 || !strings.Contains(response.Body.String(), "cache_usage_unsupported") {
		t.Fatalf("old agent guard: %d %s", response.Code, response.Body.String())
	}
	if _, ok, err := repo.UpdateHeartbeat(store.HeartbeatInput{ClusterID: cluster.ID, NodeCacheUsageSupported: true}); err != nil || !ok {
		t.Fatalf("heartbeat: %v %v", ok, err)
	}
	server, client := clusterContractSocket(t)
	router.hub.set(cluster.ID, server)
	first := request()
	if first.Code != 202 {
		t.Fatalf("refresh: %d %s", first.Code, first.Body.String())
	}
	var firstPayload map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := client.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	second := request()
	var secondPayload map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if firstPayload["requestId"] != secondPayload["requestId"] {
		t.Fatal("duplicate refresh was not coalesced")
	}
	requestID := firstPayload["requestId"].(string)
	status, _ := router.getInventoryRequestStatus(cluster.ID, requestID)
	for _, test := range []struct {
		age      time.Duration
		expected string
	}{{2 * time.Minute, "pending"}, {8 * time.Minute, "timeout"}} {
		status.Status = "pending"
		status.CreatedAt = time.Now().Add(-test.age)
		router.setInventoryRequestStatus(status)
		req := httptest.NewRequest("GET", "/", nil)
		req.SetPathValue("id", cluster.ID)
		req.SetPathValue("requestId", requestID)
		response := httptest.NewRecorder()
		router.getClusterInventoryRequest(response, req)
		var payload inventoryRequestStatus
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Status != test.expected {
			t.Fatalf("after %s: got %s", test.age, payload.Status)
		}
	}
}
