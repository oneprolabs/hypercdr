package httpserver

import (
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestLateVeleroCompletionDoesNotResurrectCanceledBackup(t *testing.T) {
	repo := store.NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: "cluster-1", AppID: "app-1", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: "cluster-1", AppID: "app-1", ProtectionPlanID: plan.ID, Type: "backup", Status: "running", Payload: map[string]any{"veleroBackupName": "backup-canceled"}})
	if err != nil {
		t.Fatal(err)
	}
	task, ok, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "canceled", ErrorCode: "SYNC_FORCE_STOPPED", ErrorMessage: "stopped", MarkDone: true})
	if err != nil || !ok {
		t.Fatalf("cancel task: ok=%v err=%v", ok, err)
	}
	router := &Router{store: repo}
	got, err := router.handleVeleroBackupEvent("cluster-1", protocol.VeleroEventPayload{PlanID: plan.ID, BackupName: "backup-canceled", Phase: "Completed", EventType: "backup_completed", Progress: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "canceled" || got.ErrorCode != "SYNC_FORCE_STOPPED" || !got.CompletedAt.Equal(task.CompletedAt) {
		t.Fatalf("late completion resurrected canceled task: %#v", got)
	}
	points, err := repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID, IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 0 {
		t.Fatalf("late completion created restore points: %#v", points)
	}
}
