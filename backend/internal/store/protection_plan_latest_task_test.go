package store

import "testing"

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
