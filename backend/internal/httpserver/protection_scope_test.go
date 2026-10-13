package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestProtectionScopeModesPersistWithoutChangingMeaning(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection store.ResourceSelection
		status    int
	}{
		{"include", store.ResourceSelection{Mode: "custom", NamespaceScoped: []string{"deployments.apps", "secrets"}}, 201},
		{"exclude", store.ResourceSelection{Mode: "exclude", NamespaceScoped: []string{"events"}}, 201},
		{"empty exclusion", store.ResourceSelection{Mode: "exclude"}, 201},
		{"empty inclusion", store.ResourceSelection{Mode: "custom"}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestStore(t)
			clusterID := seedSchedulerCluster(t, repo)
			app := seedSchedulerApplication(t, repo, clusterID, "scope-test")
			storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "scope", Type: "s3", Endpoint: "http://minio:9000", Bucket: "scope"})
			if err != nil {
				t.Fatal(err)
			}
			// Exercise the real HTTP save boundary and PostgreSQL roundtrip without
			// dispatching test configuration to any managed cluster.
			r := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true}
			raw, err := json.Marshal(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, StorageRepoID: storage.ID, ScopeType: "all", ResourceSelection: tc.selection})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/v1/protection-plans", strings.NewReader(string(raw)))
			w := httptest.NewRecorder()
			r.createProtectionPlan(w, req)
			if w.Code != tc.status {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			if tc.status == 400 {
				if !strings.Contains(w.Body.String(), "resource_selection_required") {
					t.Fatal(w.Body.String())
				}
				return
			}
			var response store.ProtectionPlan
			if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			persisted, ok, err := repo.GetProtectionPlan(response.ID)
			if err != nil || !ok {
				t.Fatalf("load plan: %v %v", ok, err)
			}
			if persisted.ResourceSelection.Mode != tc.selection.Mode || !reflect.DeepEqual(persisted.ResourceSelection.NamespaceScoped, tc.selection.NamespaceScoped) {
				t.Fatalf("scope changed in database: %#v want %#v", persisted.ResourceSelection, tc.selection)
			}
			payload := resourceSelectionPayload(map[string]any{"resourceSelection": persisted.ResourceSelection})
			if !reflect.DeepEqual(payload, persisted.ResourceSelection) {
				t.Fatalf("task mapping changed scope: %#v", payload)
			}
		})
	}
}
