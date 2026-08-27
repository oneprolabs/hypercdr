package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCreateTaskUpdatesOnlyMatchingProtectionPlanLatestPointer(t *testing.T) {
	repo := NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{
		TenantID: "tenant-1", SourceClusterID: "cluster-1", AppID: "app-1", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	backup, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	current, ok, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !ok {
		t.Fatalf("get plan: ok=%v err=%v", ok, err)
	}
	if current.LatestSyncTaskID != backup.ID || current.LatestRecoveryTaskID != "" {
		t.Fatalf("latest pointers after backup = sync %q recovery %q", current.LatestSyncTaskID, current.LatestRecoveryTaskID)
	}

	drill, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "drill", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ = repo.GetProtectionPlan(plan.ID)
	if current.LatestSyncTaskID != backup.ID || current.LatestRecoveryTaskID != drill.ID {
		t.Fatalf("latest pointers after drill = sync %q recovery %q", current.LatestSyncTaskID, current.LatestRecoveryTaskID)
	}

	if _, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "retention-cleanup", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	current, _, _ = repo.GetProtectionPlan(plan.ID)
	if current.LatestSyncTaskID != backup.ID || current.LatestRecoveryTaskID != drill.ID {
		t.Fatalf("non-display task changed latest pointers = sync %q recovery %q", current.LatestSyncTaskID, current.LatestRecoveryTaskID)
	}
}

func TestHistoricalReconciliationTaskDoesNotPublishLatestPointer(t *testing.T) {
	repo := NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-1", SourceClusterID: "cluster-1", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	historical, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded", SuppressLatestPointer: true})
	if err != nil {
		t.Fatal(err)
	}
	updated, ok, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !ok {
		t.Fatalf("get plan: ok=%v err=%v", ok, err)
	}
	if updated.LatestSyncTaskID != current.ID || updated.LatestSyncTaskID == historical.ID {
		t.Fatalf("historical reconciliation changed pointer: got %q, current %q historical %q", updated.LatestSyncTaskID, current.ID, historical.ID)
	}
}

func TestConcurrentTaskCreationPublishesOneCompleteTask(t *testing.T) {
	repo := NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-1", SourceClusterID: "cluster-1", AppID: "app-1", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	const count = 24
	created := make(chan Task, count)
	errors := make(chan error, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			task, createErr := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "queued", Payload: map[string]any{"request": index}})
			if createErr != nil {
				errors <- createErr
				return
			}
			created <- task
		}(index)
	}
	wait.Wait()
	close(created)
	close(errors)
	for createErr := range errors {
		t.Fatal(createErr)
	}
	byID := map[string]Task{}
	for task := range created {
		byID[task.ID] = task
	}
	if len(byID) != count {
		t.Fatalf("created %d unique tasks, want %d", len(byID), count)
	}
	current, ok, err := repo.GetProtectionPlan(plan.ID)
	if err != nil || !ok {
		t.Fatalf("get plan: ok=%v err=%v", ok, err)
	}
	if _, exists := byID[current.LatestSyncTaskID]; !exists {
		t.Fatalf("published pointer %q is not one of the committed tasks", current.LatestSyncTaskID)
	}
	if current.LatestRecoveryTaskID != "" {
		t.Fatalf("backup concurrency changed recovery pointer: %q", current.LatestRecoveryTaskID)
	}
}

func TestTaskEventsAndRestorePointStayBoundToExactTask(t *testing.T) {
	repo := NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-1", SourceClusterID: "cluster-1", AppID: "app-1", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []Task{first, second} {
		if err := repo.AddTaskEvent(TaskEventInput{TaskID: task.ID, Level: "info", Reason: "result", Message: fmt.Sprintf("result for %s", task.ID)}); err != nil {
			t.Fatal(err)
		}
	}
	firstEvents, err := repo.ListTaskEvents(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondEvents, err := repo.ListTaskEvents(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstEvents) != 1 || firstEvents[0].TaskID != first.ID || len(secondEvents) != 1 || secondEvents[0].TaskID != second.ID {
		t.Fatalf("events crossed task boundaries: first=%#v second=%#v", firstEvents, secondEvents)
	}
	point, err := repo.CreateRestorePoint(RestorePointInput{ProtectionPlanID: plan.ID, SourceClusterID: "cluster-1", BackupTaskID: second.ID, VeleroBackupName: "backup-second", TaskCreatedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if point.BackupTaskID != second.ID || !point.TaskCreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("restore point relation = task %q time %v, want %q %v", point.BackupTaskID, point.TaskCreatedAt, second.ID, second.CreatedAt)
	}
	if _, err := repo.CreateRestorePoint(RestorePointInput{ProtectionPlanID: plan.ID, SourceClusterID: "cluster-1", BackupTaskID: first.ID, VeleroBackupName: "backup-wrong-status"}); err != nil {
		// A failed backup is still a backup task; callers decide whether its
		// outcome is usable. The relation itself remains structurally valid.
		t.Fatal(err)
	}
}

func TestTaskStatusChangesDoNotChangeProtectionPlanLatestPointer(t *testing.T) {
	repo := NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{
		TenantID: "tenant-1", SourceClusterID: "cluster-1", AppID: "app-1", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "running", Progress: 50}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "succeeded", Progress: 100, MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	current, _, _ := repo.GetProtectionPlan(plan.ID)
	if current.LatestSyncTaskID != task.ID {
		t.Fatalf("status update changed latest sync pointer: got %q want %q", current.LatestSyncTaskID, task.ID)
	}
}

func TestTerminalTaskStateIsImmutable(t *testing.T) {
	repo := NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: "tenant-1", SourceClusterID: "cluster-1", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	terminal, ok, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "canceled", Progress: 20, ErrorCode: "SYNC_FORCE_STOPPED", ErrorMessage: "stopped", MarkDone: true})
	if err != nil || !ok {
		t.Fatalf("terminal update: ok=%v err=%v", ok, err)
	}
	late, ok, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "succeeded", Progress: 100, RestorePointID: "late-point", MarkDone: true})
	if err != nil || !ok {
		t.Fatalf("late update: ok=%v err=%v", ok, err)
	}
	if late.Status != "canceled" || late.ErrorCode != "SYNC_FORCE_STOPPED" || late.ErrorMessage != "stopped" || late.RestorePointID != "" || !late.CompletedAt.Equal(terminal.CompletedAt) {
		t.Fatalf("late completion mutated terminal task: %#v", late)
	}
}
