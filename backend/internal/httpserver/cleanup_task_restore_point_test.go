package httpserver

import (
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestEnrichCleanupTaskRestorePointTimesFillsMissingValue(t *testing.T) {
	repo := newTestStore(t)
	clusterID := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, clusterID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	backupTask, err := repo.CreateTask(store.TaskInput{ProtectionPlanID: plan.ID, ClusterID: clusterID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	taskCreatedAt := backupTask.CreatedAt
	point, err := repo.CreateRestorePoint(store.RestorePointInput{
		ProtectionPlanID: plan.ID,
		BackupTaskID:     backupTask.ID,
		SourceClusterID:  clusterID,
		VeleroBackupName: "backup-1",
		TaskCreatedAt:    time.Date(2030, time.July, 22, 18, 39, 29, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateRestorePoint() error = %v", err)
	}

	router := &Router{store: repo}
	items := router.enrichCleanupTaskRestorePointTimes([]store.Task{{
		Type: "retention-cleanup",
		Payload: map[string]any{
			"restorePoints": []any{map[string]any{"id": point.ID}},
		},
	}})

	points := items[0].Payload["restorePoints"].([]any)
	got, ok := points[0].(map[string]any)["taskCreatedAt"].(time.Time)
	if !ok || !got.Equal(taskCreatedAt) {
		t.Fatalf("taskCreatedAt = %#v, want %v", got, taskCreatedAt)
	}
}
