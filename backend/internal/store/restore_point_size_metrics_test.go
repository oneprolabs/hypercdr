package store

import "testing"

func TestRestorePointSizeMetricsEventOrdering(t *testing.T) {
	for _, order := range []string{"metrics_first", "point_first", "completed_task_first"} {
		t.Run(order, func(t *testing.T) {
			repo := newTestStore(t)
			cluster, _ := seedPlanApplication(t, repo)
			plan, task := seedRestorePointBackupTask(t, repo, cluster.ID)
			input := RestorePointInput{SourceClusterID: cluster.ID, ProtectionPlanID: plan.ID, BackupTaskID: task.ID, VeleroBackupName: "metrics-order"}
			metrics := map[string]any{"schemaVersion": float64(2), "newData": map[string]any{"known": true, "totalBytes": float64(23988)}}
			writeMetrics := func() {
				t.Helper()
				if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "succeeded", MarkDone: true, Payload: map[string]any{"sizeMetricsV2": metrics}}); err != nil {
					t.Fatal(err)
				}
			}
			if order == "metrics_first" {
				writeMetrics()
			}
			point, err := repo.CreateRestorePoint(input)
			if err != nil {
				t.Fatal(err)
			}
			if order == "completed_task_first" {
				if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "succeeded", MarkDone: true}); err != nil {
					t.Fatal(err)
				}
			}
			writeMetrics()
			for range 2 {
				if _, err := repo.CreateRestorePoint(input); err != nil {
					t.Fatal(err)
				}
			}
			persisted, found, err := repo.GetRestorePoint(point.ID)
			if err != nil || !found {
				t.Fatalf("point missing: %v", err)
			}
			newData, _ := persisted.SizeMetricsV2["newData"].(map[string]any)
			if newData["totalBytes"] != float64(23988) || newData["known"] != true {
				t.Fatalf("metrics lost for %s: %v", order, persisted.SizeMetricsV2)
			}
			persistedTask, _, err := repo.GetTask(task.ID)
			if err != nil || persistedTask.RestorePointID != "" {
				t.Fatalf("test must not depend on task restore point pointer: %v %v", persistedTask, err)
			}
		})
	}
}

func TestRestorePointSizeMetricsV2RoundTripAndUpsert(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	plan, task := seedRestorePointBackupTask(t, repo, cluster.ID)
	point, err := repo.CreateRestorePoint(RestorePointInput{
		SourceClusterID: cluster.ID, VeleroBackupName: "backup-size",
		ProtectionPlanID: plan.ID, BackupTaskID: task.ID,
		SizeMetricsV2: map[string]any{
			"schemaVersion": float64(2),
			"newData":       map[string]any{"totalBytes": float64(0), "known": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	newData := point.SizeMetricsV2["newData"].(map[string]any)
	if known, _ := newData["known"].(bool); !known {
		t.Fatal("known zero incremental data was not preserved")
	}
	updated, err := repo.CreateRestorePoint(RestorePointInput{
		SourceClusterID: cluster.ID, VeleroBackupName: "backup-size",
		ProtectionPlanID: plan.ID, BackupTaskID: task.ID,
		SizeMetricsV2: map[string]any{"measurementStatus": "partial"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.SizeMetricsV2["measurementStatus"]; got != "partial" {
		t.Fatalf("measurementStatus = %v, want partial", got)
	}
	for _, metrics := range []map[string]any{nil, {}} {
		updated, err = repo.CreateRestorePoint(RestorePointInput{
			SourceClusterID: cluster.ID, VeleroBackupName: "backup-size",
			ProtectionPlanID: plan.ID, BackupTaskID: task.ID, SizeMetricsV2: metrics,
		})
		if err != nil || updated.SizeMetricsV2["measurementStatus"] != "partial" {
			t.Fatalf("empty inventory overwrote final metrics: %v %v", updated.SizeMetricsV2, err)
		}
	}
}
