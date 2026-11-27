package httpserver

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestRecoveryRetryUsesSourceBackupAndDropsRuntimeProgress(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	plan := testTenantPlan(t, repo, actor.TenantID)
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	point, err := repo.CreateRestorePoint(store.RestorePointInput{ProtectionPlanID: plan.ID, SourceClusterID: plan.SourceClusterID, BackupTaskID: backup.ID, VeleroBackupName: "source-backup", Status: "available"})
	if err != nil {
		t.Fatal(err)
	}
	original, err := repo.CreateTask(store.TaskInput{ClusterID: plan.TargetClusterID, ProtectionPlanID: plan.ID, RestorePointID: point.ID, Type: "drill", Status: "failed", Payload: map[string]any{"veleroBackupName": "hcdr-restore-old", "sourceNamespace": "demo", "volumeProgress": map[string]any{"failedCount": 1}, "recoveryStages": []any{}, "failureDetails": []any{"old failure"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workerContext: ctx}
	req := tenantRequest(httptest.NewRequest("POST", "/api/v1/tasks/"+original.ID+"/retry", nil), actor)
	req.SetPathValue("id", original.ID)
	response := httptest.NewRecorder()
	router.retryRecoveryTask(response, req)
	if response.Code != 201 {
		t.Fatalf("retry: %d %s", response.Code, response.Body.String())
	}
	tasks, err := repo.ListTasks(plan.TargetClusterID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.ID == original.ID || task.Type != "drill" {
			continue
		}
		if task.Payload["veleroBackupName"] != "source-backup" || task.Payload["sourceNamespace"] != "demo" || task.Payload["volumeProgress"] != nil || task.Payload["recoveryStages"] != nil || task.Payload["failureDetails"] != nil {
			t.Fatalf("unsafe retry payload: %#v", task.Payload)
		}
		return
	}
	t.Fatal("retry task not persisted")
}
