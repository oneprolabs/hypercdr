package store

import (
	"errors"
	"testing"
	"time"
)

func TestTaskAndRestorePointRejectForeignReferencesBeforeMutation(t *testing.T) {
	repo := newTestStore(t)
	ownCluster, ownApp := seedPlanApplication(t, repo)
	otherTenant, err := repo.CreateTenant(TenantInput{Name: "foreign-references", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := repo.CreateAgentToken(otherTenant.ID, "", "foreign-references", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	foreignCluster, _, err := repo.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "foreign"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ApplyInventory(InventoryInput{ClusterID: foreignCluster.ID, Apps: []Application{{Namespace: "foreign", Name: "foreign", Status: "active"}}}); err != nil {
		t.Fatal(err)
	}
	apps, err := repo.ListApplications(foreignCluster.ID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("applications: %v %v", apps, err)
	}
	foreignApp := apps[0]
	ownPlan, err := repo.CreateProtectionPlan(ProtectionPlanInput{SourceClusterID: ownCluster.ID, AppID: ownApp.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreignPlan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: otherTenant.ID, SourceClusterID: foreignCluster.ID, AppID: foreignApp.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreignBackup, err := repo.CreateTask(TaskInput{ClusterID: foreignCluster.ID, AppID: foreignApp.ID, ProtectionPlanID: foreignPlan.ID, Type: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	foreignPoint, err := repo.CreateRestorePoint(RestorePointInput{SourceClusterID: foreignCluster.ID, AppID: foreignApp.ID, ProtectionPlanID: foreignPlan.ID, BackupTaskID: foreignBackup.ID, VeleroBackupName: "foreign"})
	if err != nil {
		t.Fatal(err)
	}
	foreignStorage, err := repo.CreateStorageRepository(StorageRepositoryInput{TenantID: otherTenant.ID, Name: "foreign", Type: "S3"})
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]TaskInput{
		"application":         {ClusterID: ownCluster.ID, AppID: foreignApp.ID, ProtectionPlanID: ownPlan.ID},
		"restore point":       {ClusterID: ownCluster.ID, AppID: ownApp.ID, ProtectionPlanID: ownPlan.ID, RestorePointID: foreignPoint.ID},
		"missing application": {ClusterID: ownCluster.ID, AppID: NewPublicID(), ProtectionPlanID: ownPlan.ID},
	} {
		t.Run("task/"+name, func(t *testing.T) {
			if _, err := repo.CreateTask(input); !errors.Is(err, ErrTenantResourceMismatch) {
				t.Fatalf("invalid task accepted: %v", err)
			}
		})
	}
	unchangedPlan, _, err := repo.GetProtectionPlan(ownPlan.ID)
	if err != nil || unchangedPlan.LatestSyncTaskID != "" {
		t.Fatalf("rejected task changed latest pointer: %+v %v", unchangedPlan, err)
	}
	items, err := repo.ListTasksFiltered(TaskFilter{TenantID: ownCluster.TenantID})
	if err != nil || len(items) != 0 {
		t.Fatalf("rejected tasks persisted: %+v %v", items, err)
	}
	ownBackup, err := repo.CreateTask(TaskInput{ClusterID: ownCluster.ID, AppID: ownApp.ID, ProtectionPlanID: ownPlan.ID, Type: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	valid := RestorePointInput{SourceClusterID: ownCluster.ID, AppID: ownApp.ID, ProtectionPlanID: ownPlan.ID, BackupTaskID: ownBackup.ID, VeleroBackupName: "own"}
	for _, field := range []string{"application", "plan", "storage", "backup", "source"} {
		t.Run("restore/"+field, func(t *testing.T) {
			input := valid
			switch field {
			case "application":
				input.AppID = foreignApp.ID
			case "plan":
				input.ProtectionPlanID = foreignPlan.ID
			case "storage":
				input.StorageRepoID = foreignStorage.ID
			case "backup":
				input.BackupTaskID = foreignBackup.ID
			case "source":
				input.SourceClusterID = NewPublicID()
			}
			if _, err := repo.CreateRestorePoint(input); !errors.Is(err, ErrTenantResourceMismatch) {
				t.Fatalf("invalid restore point accepted: %v", err)
			}
		})
	}
	points, err := repo.ListRestorePoints(RestorePointFilter{ClusterID: ownCluster.ID})
	if err != nil || len(points) != 0 {
		t.Fatalf("invalid restore points persisted: %v %v", points, err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: ownBackup.ID, RestorePointID: foreignPoint.ID, Status: "succeeded", Progress: 100, MarkDone: true}); !errors.Is(err, ErrTenantResourceMismatch) {
		t.Fatalf("status linked foreign restore point: %v", err)
	}
	unchanged, _, err := repo.GetTask(ownBackup.ID)
	if err != nil || unchanged.Status != "queued" || unchanged.Progress != 0 || !unchanged.CompletedAt.IsZero() || unchanged.RestorePointID != "" {
		t.Fatalf("invalid update partially applied: %+v %v", unchanged, err)
	}
	point, err := repo.CreateRestorePoint(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: ownBackup.ID, RestorePointID: point.ID, Status: "succeeded", Progress: 100, MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	plan, _, err := repo.GetProtectionPlan(ownPlan.ID)
	if err != nil || plan.LatestSyncTaskID != ownBackup.ID {
		t.Fatalf("invalid task changed plan pointer: %+v %v", plan, err)
	}
}
