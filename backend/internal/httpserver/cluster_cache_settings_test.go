package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCacheObservationDoesNotOverrideLaterTaskSuccess(t *testing.T) {
	completed := time.Date(2026, 11, 13, 6, 36, 40, 0, time.UTC)
	task := &store.Task{Status: "succeeded", CompletedAt: completed}
	for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
		observed := map[string]any{"observedAt": completed.Add(offset).Format(time.RFC3339Nano)}
		if got := cacheObservationNewerThanTask(observed, task); got != (offset > 0) {
			t.Fatalf("offset %s: newer = %v", offset, got)
		}
	}
	if cacheObservationNewerThanTask(nil, task) {
		t.Fatal("missing observation superseded successful task")
	}
}

func TestCacheTaskTransitionPreservesIsolationOnDispatch(t *testing.T) {
	want := &protocol.CacheSettingsTransition{IsolationID: strings.Repeat("a", 64), Nodes: []string{"node-one"}, Paths: []string{"/cache/old"}, Previous: protocol.CacheConfigurationSnapshot{IndexEnabled: true, IndexPath: "/cache/index", IndexMaxGiB: 5}}
	for _, kind := range []string{"cache-settings", "cache-cleanup"} {
		task := store.Task{Type: kind, Payload: map[string]any{"transition": want}}
		transition, err := cacheTaskTransition(task)
		if err != nil || transition.IsolationID != want.IsolationID || transition.Nodes[0] != "node-one" || transition.Previous.IndexPath != "/cache/index" {
			t.Fatalf("%s lost transition identity: %+v %v", kind, transition, err)
		}
	}
	if _, err := cacheTaskTransition(store.Task{Payload: map[string]any{"transition": map[string]any{"isolationId": "incomplete"}}}); err == nil {
		t.Fatal("malformed transition silently fell back to original-directory cleanup")
	}
}

func TestCacheTaskTransitionAcceptsAbsentAndTypedNilSnapshot(t *testing.T) {
	var absent *protocol.CacheSettingsTransition
	for _, payload := range []map[string]any{nil, {}, {"transition": nil}, {"transition": absent}, {"transition": json.RawMessage("null")}} {
		transition, err := cacheTaskTransition(store.Task{Payload: payload})
		if err != nil || transition != nil {
			t.Fatalf("configuration without isolation could not dispatch: %v %v", transition, err)
		}
	}
}

func TestCacheCleanupRetryProtectsCurrentDirectories(t *testing.T) {
	settings := store.ClusterCacheSettings{IndexEnabled: true, IndexPath: "/cache/index", DrillEnabled: true, DrillPath: "/cache/drill"}
	for _, path := range []string{"/", "relative", "/cache", "/cache/index", "/cache/drill/child", "/cache/../old"} {
		if cacheCleanupPathsInactive([]string{path}, settings) {
			t.Errorf("accepted unsafe cleanup retry: %s", path)
		}
	}
	if !cacheCleanupPathsInactive([]string{"/cache/old"}, settings) {
		t.Fatal("rejected inactive directory")
	}
	settings.DrillEnabled = false
	if !cacheCleanupPathsInactive([]string{"/cache/drill"}, settings) {
		t.Fatal("rejected disabled directory")
	}
}

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
		{"PUT", own.ID, `{"indexEnabled":true,"indexMaxGiB":0,"drillMaxGiB":50}`, 400},
		{"PUT", own.ID, `{"indexEnabled":true,"indexMaxGiB":1.5,"drillMaxGiB":50}`, 400},
		{"PUT", own.ID, `{"indexEnabled":false,"indexMaxGiB":5,"drillMaxGiB":50}`, 409},
		{"POST", own.ID, "{}", 409},
		{"POST", own.ID, `{"taskId":"missing-cleanup-task"}`, 404},
		{"POST", foreign.ID, `{"taskId":"missing-cleanup-task"}`, 404},
		{"POST", own.ID, `{"taskId":`, 400},
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
		if !result.Settings.IndexEnabled || result.Settings.IndexMaxGiB != 5 {
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

func TestCacheHeartbeatPreservesCollectionTime(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "cache-collection-time")
	collectedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	updated, ok, err := repo.UpdateHeartbeat(store.HeartbeatInput{ClusterID: cluster.ID, Status: "healthy", CacheState: map[string]any{"phase": "applied", "observedAt": collectedAt, "drillEnabled": false}})
	if err != nil || !ok {
		t.Fatalf("heartbeat: %v %v", ok, err)
	}
	if updated.CacheState["observedAt"] != collectedAt || updated.CacheState["drillEnabled"] != false {
		t.Fatalf("collection time or actual state was replaced: %+v", updated.CacheState)
	}
}

func TestCacheCleanupPathsOnlyReturnsReplacedOrDisabledPaths(t *testing.T) {
	previous := store.ClusterCacheSettings{IndexPath: "/cache/index", DrillPath: "/cache/drill", IndexEnabled: true, DrillEnabled: true}
	requested := store.ClusterCacheSettings{IndexPath: "/cache/new-index", DrillPath: "/cache/drill", IndexEnabled: true, DrillEnabled: false}
	paths := cacheCleanupPaths(previous, requested)
	if len(paths) != 2 || paths[0] != "/cache/index" || paths[1] != "/cache/drill" {
		t.Fatalf("unexpected cleanup paths: %#v", paths)
	}
}

func TestCacheSizeChangesPreserveExistingDirectories(t *testing.T) {
	previous := store.ClusterCacheSettings{RootPath: "/cache", IndexPath: "/cache/index", DrillPath: "/cache/drill", IndexEnabled: true, DrillEnabled: true, IndexMaxGiB: 5, DrillMaxGiB: 60}
	for _, sizes := range [][2]int{{10, 120}, {1, 10}, {5, 60}} {
		requested := previous
		requested.IndexMaxGiB = sizes[0]
		requested.DrillMaxGiB = sizes[1]
		if paths := cacheCleanupPaths(previous, requested); len(paths) != 0 {
			t.Fatalf("size-only change requested cache deletion: %v", paths)
		}
	}
}

func TestCacheCleanupNestedLegacyPaths(t *testing.T) {
	for _, previous := range []store.ClusterCacheSettings{
		{IndexPath: "/cache", DrillPath: "/cache/drill", IndexEnabled: true, DrillEnabled: true},
		{IndexPath: "/cache/index", DrillPath: "/cache", IndexEnabled: true, DrillEnabled: true},
		{IndexPath: "/cache", DrillPath: "/cache", IndexEnabled: true, DrillEnabled: true},
	} {
		requested := previous
		requested.IndexEnabled = false
		requested.DrillEnabled = false
		paths := cacheCleanupPaths(previous, requested)
		if len(paths) != 1 || paths[0] != "/cache" {
			t.Fatalf("nested legacy cleanup was not normalized: %v", paths)
		}
		if !cacheCleanupPathsInactive(paths, requested) {
			t.Fatal("disabled legacy caches cannot be migrated")
		}
		requested.IndexEnabled = true
		if cacheCleanupPathsInactive(paths, requested) {
			t.Fatal("normalized cleanup bypassed active index protection")
		}
		requested.IndexEnabled = false
		requested.DrillEnabled = true
		if cacheCleanupPathsInactive(paths, requested) {
			t.Fatal("normalized cleanup bypassed active Drill protection")
		}
	}
}
