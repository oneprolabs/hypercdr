package httpserver

import (
	"errors"
	"log/slog"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestRestorePointReplacementWaitsForConfirmedDeletionAndRetriesPublication(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "replacement")
	old := seedContractRestorePoint(t, repo, cluster, "old-replacement-backup")
	policy, err := repo.CreatePolicy(store.PolicyInput{TenantID: actor.TenantID, Name: "replacement-quota", Composition: "combined", RetentionCount: 1, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := repo.GetProtectionPlan(old.ProtectionPlanID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateProtectionPlan(plan.ID, store.ProtectionPlanInput{TenantID: actor.TenantID, SourceClusterID: cluster.ID, AppID: plan.AppID, PolicyID: policy.ID, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	backup, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	input := store.RestorePointInput{SourceClusterID: cluster.ID, ProtectionPlanID: plan.ID, BackupTaskID: backup.ID, VeleroBackupName: "new-replacement-backup", TaskCreatedAt: backup.CreatedAt}
	if _, err := repo.CreateRestorePoint(input); !errors.Is(err, store.ErrRestorePointReplacementPending) {
		t.Fatalf("premature publication: %v", err)
	}
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default(), contentIndexing: map[string]struct{}{}, workersClosing: true}
	router.reconcileRestorePointReplacements()
	pending, err := repo.PendingRestorePointReplacements()
	if err != nil || len(pending) != 1 || pending[0].CleanupTaskID == "" {
		t.Fatalf("no durable cleanup: %v %v", pending, err)
	}
	retained, _, _ := repo.GetRestorePoint(old.ID)
	if retained.Status != "available" {
		t.Fatal("queued deletion released quota prematurely")
	}
	cleanup, _, err := repo.GetTask(pending[0].CleanupTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: cleanup.ID, Status: "failed", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	router.markRetentionCleanupFailed(cleanup, "storage temporarily unavailable")
	router.reconcileRestorePointReplacements()
	parent, _, _ := repo.GetTask(backup.ID)
	if parent.Status != "finalizing" || !parent.CompletedAt.IsZero() {
		t.Fatalf("failed deletion closed parent: %v", parent)
	}
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: backup.ID, Status: "finalizing", Payload: map[string]any{"sizeMetricsV2": map[string]any{"schemaVersion": 2, "logical": map[string]any{"known": true, "totalBytes": 1024}, "newData": map[string]any{"known": true, "totalBytes": float64(123)}}}}); err != nil {
		t.Fatal(err)
	}
	locked, _, _ := repo.GetRestorePoint(old.ID)
	retry, _, err := router.createRestorePointDeleteTask(cluster.ID, []store.RestorePoint{locked})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: retry.ID, Status: "succeeded", MarkDone: true, Payload: map[string]any{"deletedRestorePointIds": []any{old.ID}}}); err != nil {
		t.Fatal(err)
	}
	router.reconcileRestorePointReplacements()
	router.reconcileRestorePointReplacements()
	parent, _, _ = repo.GetTask(backup.ID)
	if parent.Status != "succeeded" || parent.RestorePointID == "" {
		t.Fatalf("new point was not published: %v", parent)
	}
	published, _, _ := repo.GetRestorePoint(parent.RestorePointID)
	if metrics := mapFromAny(published.SizeMetricsV2["newData"]); metrics["totalBytes"] != float64(123) {
		t.Fatalf("replacement lost final task metrics: %v", published.SizeMetricsV2)
	}
	router.reconcileRestorePointReplacements()
	points, err := repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID})
	available := 0
	for _, point := range points {
		if point.Status == "available" {
			available++
		}
	}
	if err != nil || available != 1 {
		t.Fatalf("quota violated after retries: %v %v", points, err)
	}
}
