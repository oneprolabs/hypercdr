package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClusterCacheSettingsScopeValidationAndOffline(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	own := testTenantCluster(t, repo, actor.TenantID, "cache-own")
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "cache-foreign", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := testTenantCluster(t, repo, tenant.ID, "cache-foreign")
	router := &Router{store: repo, hub: newSessionHub()}
	for _, scenario := range []struct {
		method, cluster, body string
		status                int
	}{
		{"GET", own.ID, "", 200},
		{"GET", foreign.ID, "", 404},
		{"PUT", foreign.ID, `{"enabled":true,"maxGiB":5}`, 404},
		{"PUT", own.ID, `{"enabled":true,"maxGiB":0}`, 400},
		{"PUT", own.ID, `{"enabled":true,"maxGiB":1.5}`, 400},
		{"PUT", own.ID, `{"enabled":false,"maxGiB":5}`, 409},
		{"POST", own.ID, "{}", 409},
	} {
		req := tenantRequest(httptest.NewRequest(scenario.method, "/api/v1/clusters/"+scenario.cluster+"/cache-settings", strings.NewReader(scenario.body)), actor)
		req.SetPathValue("id", scenario.cluster)
		response := httptest.NewRecorder()
		router.clusterCacheSettings(response, req)
		if response.Code != scenario.status {
			t.Fatalf("%+v: %d %s", scenario, response.Code, response.Body.String())
		}
	}
	if !requiresAdmin(httptest.NewRequest("PUT", "/api/v1/clusters/id/cache-settings", nil)) {
		t.Fatal("mutation must require administrator")
	}
}

func TestClusterCacheSettingsReportsObservedNotDesired(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "cache-observed")
	router := &Router{store: repo, hub: newSessionHub()}
	for _, observed := range []map[string]any{{"phase": "applied", "enabled": false, "maxGiB": 3}, nil} {
		if _, ok, err := repo.UpdateHeartbeat(store.HeartbeatInput{ClusterID: cluster.ID, Status: "healthy", CacheState: observed}); err != nil || !ok {
			t.Fatalf("heartbeat: %v %v", ok, err)
		}
		req := tenantRequest(httptest.NewRequest("GET", "/api/v1/clusters/"+cluster.ID+"/cache-settings", nil), actor)
		req.SetPathValue("id", cluster.ID)
		response := httptest.NewRecorder()
		router.clusterCacheSettings(response, req)
		var result struct {
			Settings store.ClusterCacheSettings `json:"settings"`
			Observed map[string]any             `json:"observed"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
		}
		if !result.Settings.Enabled || result.Settings.MaxGiB != 5 {
			t.Fatal("registration defaults changed")
		}
		if observed == nil {
			if result.Observed != nil {
				t.Fatal("old agent heartbeat retained stale observed settings")
			}
		} else if result.Observed["enabled"] != false || result.Observed["maxGiB"] != float64(3) || result.Observed["observedAt"] == nil {
			t.Fatalf("desired settings substituted for actual: %+v", result.Observed)
		}
	}
}
