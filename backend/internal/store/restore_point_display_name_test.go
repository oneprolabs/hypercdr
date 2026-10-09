package store

import "testing"

func seedRestorePointBackupTask(t *testing.T, repo *PostgresStore, clusterID string) (ProtectionPlan, Task) {
	t.Helper()
	apps, err := repo.ListApplications(clusterID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("fixture application: %v", err)
	}
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{SourceClusterID: clusterID, AppID: apps[0].ID, Status: "active"})
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
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	plan, task := seedRestorePointBackupTask(t, repo, cluster.ID)
	point, err := repo.CreateRestorePoint(RestorePointInput{
		ProtectionPlanID: plan.ID,
		BackupTaskID:     task.ID,
		SourceClusterID:  cluster.ID,
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
