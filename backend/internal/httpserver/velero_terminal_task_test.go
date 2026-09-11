package httpserver

import (
	"log/slog"
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
	router := &Router{store: repo, logger: slog.Default()}
	got, err := router.handleVeleroBackupEvent("cluster-1", protocol.VeleroEventPayload{TaskID: task.ID, PlanID: plan.ID, BackupName: "backup-canceled", Phase: "Completed", EventType: "backup_completed", Progress: 100})
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

func TestVeleroEventWithoutTaskIDIsRejected(t *testing.T) {
	repo := store.NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: "cluster-1", AppID: "app-1", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	if _, err := router.handleVeleroBackupEvent("cluster-1", protocol.VeleroEventPayload{PlanID: plan.ID, BackupName: "backup-orphan", EventType: "backup_progress"}); err == nil {
		t.Fatal("expected missing task id to be rejected")
	}
	tasks, err := repo.ListTasks("cluster-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("orphan event created %d task(s)", len(tasks))
	}
}

func TestVeleroTaskIdentityCannotFallBackToName(t *testing.T) {
	repo := store.NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: "cluster-1", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: "cluster-1", ProtectionPlanID: plan.ID, Type: "backup", CommandID: "command-1", Payload: map[string]any{"veleroBackupName": "backup-1"}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	for _, tc := range []struct{ name, taskID, clusterID, commandID, backup string }{
		{"unknown task same backup", "unknown", "cluster-1", "command-1", "backup-1"},
		{"wrong cluster", task.ID, "cluster-2", "command-1", "backup-1"},
		{"wrong command", task.ID, "cluster-1", "wrong", "backup-1"},
		{"wrong backup", task.ID, "cluster-1", "command-1", "wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := router.findOrCreateVeleroBackupTask(tc.clusterID, plan, protocol.VeleroEventPayload{TaskID: tc.taskID, CommandID: tc.commandID, BackupName: tc.backup})
			if err == nil {
				t.Fatal("expected identity mismatch rejection")
			}
		})
	}
	tasks, err := repo.ListTasks("")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks=%d err=%v", len(tasks), err)
	}
	got, _, err := repo.GetTask(task.ID)
	if err != nil || got.Status != task.Status {
		t.Fatalf("rejected event changed original task: %#v %v", got, err)
	}
}
