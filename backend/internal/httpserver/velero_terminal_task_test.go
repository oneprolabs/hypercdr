package httpserver

import (
	"log/slog"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestLateVeleroCompletionDoesNotResurrectCanceledBackup(t *testing.T) {
	repo := newTestStore(t)
	clusterID := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, clusterID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, AppID: app.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running", Payload: map[string]any{"veleroBackupName": "backup-canceled"}})
	if err != nil {
		t.Fatal(err)
	}
	task, ok, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "canceled", ErrorCode: "SYNC_FORCE_STOPPED", ErrorMessage: "stopped", MarkDone: true})
	if err != nil || !ok {
		t.Fatalf("cancel task: ok=%v err=%v", ok, err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	got, err := router.handleVeleroBackupEvent(clusterID, protocol.VeleroEventPayload{TaskID: task.ID, PlanID: plan.ID, BackupName: "backup-canceled", Phase: "Completed", EventType: "backup_completed", Progress: 100})
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
	repo := newTestStore(t)
	clusterID := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, clusterID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	if _, err := router.handleVeleroBackupEvent(clusterID, protocol.VeleroEventPayload{PlanID: plan.ID, BackupName: "backup-orphan", EventType: "backup_progress"}); err == nil {
		t.Fatal("expected missing task id to be rejected")
	}
	tasks, err := repo.ListTasks(clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("orphan event created %d task(s)", len(tasks))
	}
}

func TestVeleroTaskIdentityCannotFallBackToName(t *testing.T) {
	repo := newTestStore(t)
	clusterID := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, clusterID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plan.ID, Type: "backup", CommandID: "00000000-0000-0000-0000-00000000c001", Payload: map[string]any{"veleroBackupName": "backup-1"}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	for _, tc := range []struct{ name, taskID, clusterID, commandID, backup string }{
		{"unknown task same backup", "unknown", clusterID, "00000000-0000-0000-0000-00000000c001", "backup-1"},
		{"wrong cluster", task.ID, "cluster-2", "00000000-0000-0000-0000-00000000c001", "backup-1"},
		{"wrong command", task.ID, clusterID, "wrong", "backup-1"},
		{"wrong backup", task.ID, clusterID, "00000000-0000-0000-0000-00000000c001", "wrong"},
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
