package store

import "testing"

func seedRestorePointBackupTask(t *testing.T, repo *MemoryStore, clusterID string) (ProtectionPlan, Task) {
	t.Helper()
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{SourceClusterID: clusterID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: clusterID, Type: "backup", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	return plan, task
}

func TestRestorePointDisplayNameIsDeprecated(t *testing.T) {
	repo := NewMemoryStore()
	plan, task := seedRestorePointBackupTask(t, repo, "cluster-1")
	point, err := repo.CreateRestorePoint(RestorePointInput{
		ProtectionPlanID: plan.ID,
		BackupTaskID:     task.ID,
		SourceClusterID:  "cluster-1",
		VeleroBackupName: "backup-1",
		DisplayName:      "RP-custom",
	})
	if err != nil {
		t.Fatalf("CreateRestorePoint() error = %v", err)
	}
	if point.DisplayName != "" {
		t.Fatalf("CreateRestorePoint() displayName = %q, want empty", point.DisplayName)
	}
}
