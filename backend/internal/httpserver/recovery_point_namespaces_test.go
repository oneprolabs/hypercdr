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

func TestRecoveryNamespaceValidationUsesBackupManifest(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "manifest-namespace-validation")
	app := seedSchedulerApplication(t, repo, cluster.ID, "first")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: actor.TenantID, SourceClusterID: cluster.ID, AppID: app.ID, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, AppID: app.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	point, err := repo.CreateRestorePoint(store.RestorePointInput{SourceClusterID: cluster.ID, ProtectionPlanID: plan.ID, AppID: app.ID, BackupTaskID: backup.ID, VeleroBackupName: "merged-backup", Status: "available", SourceNamespace: "first", Metadata: map[string]any{"velero": map[string]any{"manifest": map[string]any{"spec": map[string]any{"includedNamespaces": []any{"first", "second"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	for _, test := range []struct {
		name       string
		namespaces []string
		want       string
	}{
		{"both", []string{"first", "second"}, "unsupported_restore_mode"},
		{"second only", []string{"second"}, "unsupported_restore_mode"},
		{"outside backup", []string{"outside"}, "source_namespace_not_in_restore_point"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"clusterId": cluster.ID, "restorePointId": point.ID, "sourceNamespaces": test.namespaces, "restoreMode": "test-invalid-mode"})
			request := tenantRequest(httptest.NewRequest("POST", "/api/v1/tasks/drill", strings.NewReader(string(body))), actor)
			response := httptest.NewRecorder()
			router.createDrillTask(response, request)
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != 400 || result["error"] != test.want {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestRecoveryPointNamespaces(t *testing.T) {
	for _, test := range []struct {
		name     string
		metadata map[string]any
		want     []string
	}{
		{"legacy", nil, []string{"first"}},
		{"top level", map[string]any{"includedNamespaces": []any{"first", "second"}}, []string{"first", "second"}},
		{"velero", map[string]any{"velero": map[string]any{"includedNamespaces": []any{"first", "second"}}}, []string{"first", "second"}},
		{"manifest authoritative", map[string]any{"includedNamespaces": []any{"stale"}, "velero": map[string]any{"manifest": map[string]any{"spec": map[string]any{"includedNamespaces": []any{"first", "second", "first"}}}}}, []string{"first", "second"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := recoveryPointNamespaces(store.RestorePoint{SourceNamespace: "first", Metadata: test.metadata})
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
