package httpserver

import (
	"errors"
	"log/slog"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestBackupResultSurvivesObservationAndCoordinatorRestart(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "immutable-result")
	old := seedContractRestorePoint(t, repo, cluster, "old-result")
	plan, _, err := repo.GetProtectionPlan(old.ProtectionPlanID)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.CreatePolicy(store.PolicyInput{TenantID: actor.TenantID, Name: "one-point", Composition: "combined", RetentionCount: 1, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateProtectionPlan(plan.ID, store.ProtectionPlanInput{TenantID: actor.TenantID, SourceClusterID: cluster.ID, AppID: plan.AppID, PolicyID: policy.ID, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running", Payload: map[string]any{"veleroBackupName": "new-result"}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true, contentIndexing: map[string]struct{}{}}
	result := protocol.TaskCompletedPayload{TaskID: task.ID, Status: "succeeded", Velero: map[string]any{"kind": "Backup", "name": "new-result"}, SizeMetricsV2: &protocol.SizeMetricsV2{SchemaVersion: 2, Logical: protocol.SizeValueV2{Known: true, TotalBytes: 1024}, NewData: protocol.SizeValueV2{Known: true, TotalBytes: 123}}}
	if err := router.acceptBackupCompletion(task, result); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := router.handleVeleroBackupEvent(cluster.ID, protocol.VeleroEventPayload{TaskID: task.ID, PlanID: plan.ID, BackupName: "new-result", EventType: "backup_completed", Phase: "Completed"}); err != nil {
			t.Fatal(err)
		}
		router.ingestVeleroBackupsFromInventory(cluster.ID, []map[string]any{{"name": "new-result", "phase": "Completed", "labels": map[string]any{"hypercdr.io/task-id": task.ID, "hypercdr.io/plan-id": plan.ID}}})
	}
	pending, err := repo.PendingRestorePointReplacements()
	if err != nil || len(pending) != 1 || pending[0].Input.SizeMetricsV2["newData"] == nil {
		t.Fatalf("pending result overwritten: %v %v", pending, err)
	}
	parent, _, _ := repo.GetTask(task.ID)
	if parent.Status != "finalizing" || !parent.CompletedAt.IsZero() {
		t.Fatal("observation completed parent")
	}
	if _, _, err := repo.UpdateRestorePointState(store.RestorePointStateInput{ID: old.ID, Status: "deleted"}); err != nil {
		t.Fatal(err)
	}
	restarted := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true, contentIndexing: map[string]struct{}{}}
	restarted.reconcileRestorePointReplacements()
	restarted.reconcileRestorePointReplacements()
	points, err := repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID})
	if err != nil || len(points) != 1 {
		t.Fatalf("publication: %v %v", points, err)
	}
	if mapFromAny(points[0].SizeMetricsV2["newData"])["totalBytes"] != float64(123) {
		t.Fatal("metrics missing at publication")
	}
	parent, _, _ = repo.GetTask(task.ID)
	if parent.Status != "succeeded" || parent.RestorePointID != points[0].ID {
		t.Fatalf("inconsistent finalization: %v", parent)
	}
}

func TestBackupCompletionRejectsIncompleteAndConflictingIdentity(t *testing.T) {
	repo := newTestStore(t)
	clusterID := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, clusterID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running", CommandID: store.NewPublicID()})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true, contentIndexing: map[string]struct{}{}}
	result := protocol.TaskCompletedPayload{Status: "succeeded", Velero: map[string]any{"kind": "Backup", "name": "incomplete"}}
	if err := router.acceptBackupCompletion(task, result); !errors.Is(err, store.ErrBackupResultIncomplete) {
		t.Fatalf("incomplete accepted: %v", err)
	}
	points, _ := repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID})
	if len(points) != 0 {
		t.Fatal("published incomplete point")
	}
	result.CommandID = store.NewPublicID()
	if err := router.acceptBackupCompletion(task, result); !errors.Is(err, store.ErrBackupResultConflict) {
		t.Fatalf("wrong command accepted: %v", err)
	}
	result.CommandID = task.CommandID
	result.Size = map[string]any{"totalBytes": 1024, "uploadedBytes": 0, "volumeCount": 0}
	if err := router.acceptBackupCompletion(task, result); err != nil {
		t.Fatalf("legacy V1 known zero not adapted: %v", err)
	}
}
