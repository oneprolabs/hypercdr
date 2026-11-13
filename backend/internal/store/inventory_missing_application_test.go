package store

import (
	"testing"
	"time"
)

func TestInventoryMissingApplicationReferences(t *testing.T) {
	for _, reference := range []string{"none", "plan", "plan-member", "restore-point", "task"} {
		t.Run(reference, func(t *testing.T) {
			repo := newTestStore(t)
			cluster, app := seedPlanApplication(t, repo)
			var err error
			switch reference {
			case "plan":
				_, err = repo.CreateProtectionPlan(ProtectionPlanInput{SourceClusterID: cluster.ID, AppID: app.ID, Status: "active"})
			case "plan-member":
				_, _, err = repo.ApplyInventory(InventoryInput{ClusterID: cluster.ID, Apps: []Application{app, {Namespace: "other", Status: "Active"}}})
				if err != nil {
					t.Fatal(err)
				}
				apps, listErr := repo.ListApplications(cluster.ID)
				if listErr != nil {
					t.Fatal(listErr)
				}
				var otherID string
				for _, candidate := range apps {
					if candidate.Namespace == "other" {
						otherID = candidate.ID
					}
				}
				_, err = repo.CreateProtectionPlan(ProtectionPlanInput{SourceClusterID: cluster.ID, AppID: otherID, AppIDs: []string{otherID, app.ID}, Status: "active"})
			case "restore-point":
				backup, taskErr := repo.CreateTask(TaskInput{ClusterID: cluster.ID, Type: "backup", Status: "succeeded"})
				if taskErr != nil {
					t.Fatal(taskErr)
				}
				_, err = repo.CreateRestorePoint(RestorePointInput{SourceClusterID: cluster.ID, AppID: app.ID, BackupTaskID: backup.ID, VeleroBackupName: "source-loss-test", Status: "available"})
			case "task":
				_, err = repo.CreateTask(TaskInput{ClusterID: cluster.ID, AppID: app.ID, Type: "restore", Status: "succeeded"})
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, apps := range [][]Application{{{Namespace: "survivor", Status: "Active", WorkloadCount: 2}}, {{Namespace: "survivor", Status: "Active", WorkloadCount: 4}}, {}} {
				updated, ok, err := repo.ApplyInventory(InventoryInput{ClusterID: cluster.ID, Apps: apps, CollectedAt: time.Now().UTC()})
				if err != nil || !ok {
					t.Fatalf("inventory: %v %v", ok, err)
				}
				if updated.ApplicationCount != len(apps) {
					t.Fatalf("live count = %d", updated.ApplicationCount)
				}
				if len(apps) > 0 {
					current, listErr := repo.ListApplications(cluster.ID)
					if listErr != nil {
						t.Fatal(listErr)
					}
					var survivor *Application
					for index := range current {
						if current[index].Namespace == "survivor" {
							survivor = &current[index]
						}
					}
					if survivor == nil || survivor.WorkloadCount != apps[0].WorkloadCount {
						t.Fatalf("surviving namespace did not update: %+v", survivor)
					}
				}
				stored, found, err := repo.GetApplication(app.ID)
				if err != nil {
					t.Fatal(err)
				}
				if reference == "none" {
					if found {
						t.Fatal("unreferenced missing application retained")
					}
				} else if !found || stored.Status != "Missing" || stored.WorkloadCount != 0 || stored.PVCCount != 0 {
					t.Fatalf("missing application identity/state: %+v, found=%v", stored, found)
				}
			}
			if reference != "none" {
				originalID := app.ID
				app.ID = ""
				app.Status = "Active"
				app.WorkloadCount = 3
				if _, _, err := repo.ApplyInventory(InventoryInput{ClusterID: cluster.ID, Apps: []Application{app}}); err != nil {
					t.Fatal(err)
				}
				stored, found, err := repo.GetApplication(originalID)
				if err != nil || !found || stored.Status != "Active" || stored.WorkloadCount != 3 {
					t.Fatalf("reappearance: %+v %v", stored, err)
				}
			}
		})
	}
}
