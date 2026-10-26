package store

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestQuotaFailedReservationRecovery(t *testing.T) {
	for _, status := range []string{"failed", "canceled", "cancelled"} {
		for _, recovery := range []string{"status_update", "reconcile", "next_backup"} {
			t.Run(status+"/"+recovery, func(t *testing.T) {
				repo, plan := quotaFixture(t, 1)
				old := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
				backup := quotaBackup(t, repo, plan)
				if _, err := repo.db.Exec(`update tasks set status=$2 where id=$1`, backup.ID, status); err != nil {
					t.Fatal(err)
				}
				var next Task
				if recovery == "status_update" {
					if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: backup.ID, Status: status}); err != nil {
						t.Fatal(err)
					}
				} else if recovery == "reconcile" {
					for range 2 {
						if _, err := repo.PendingRestorePointReplacements(); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					next = quotaBackup(t, repo, plan)
				}
				point, _, err := repo.GetRestorePoint(old.ID)
				if err != nil || point.Status != "available" {
					t.Fatalf("old point changed: %v %v", point, err)
				}
				if recovery != "next_backup" && point.Metadata["replacementTaskId"] != nil {
					t.Fatalf("stale lock: %v", point.Metadata)
				}
				if recovery == "next_backup" && point.Metadata["replacementTaskId"] != next.ID {
					t.Fatalf("replacement not reclaimed: %v", point.Metadata)
				}
				var state string
				if err := repo.db.QueryRow(`select state from restore_point_reservations where task_id=$1`, backup.ID).Scan(&state); err != nil || state != "released" {
					t.Fatalf("reservation not released: %s %v", state, err)
				}
			})
		}
	}
}

func TestQuotaRepeatedFailuresDoNotAccumulateLocks(t *testing.T) {
	repo, plan := quotaFixture(t, 3)
	for range 3 {
		quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	}
	for range 5 {
		backup := quotaBackup(t, repo, plan)
		if backup.Status != "queued" {
			t.Fatalf("backup rejected after failure: %v", backup)
		}
		var locks int
		if err := repo.db.QueryRow(`select count(*) from restore_points where protection_plan_id=$1 and metadata ? 'replacementTaskId'`, plan.ID).Scan(&locks); err != nil || locks != 1 {
			t.Fatalf("locks accumulated: %d %v", locks, err)
		}
		if _, err := repo.db.Exec(`update tasks set status='failed' where id=$1`, backup.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.PendingRestorePointReplacements(); err != nil {
		t.Fatal(err)
	}
	var available, locks int
	if err := repo.db.QueryRow(`select count(*) filter(where status='available'),count(*) filter(where metadata ? 'replacementTaskId') from restore_points where protection_plan_id=$1`, plan.ID).Scan(&available, &locks); err != nil || available != 3 || locks != 0 {
		t.Fatalf("old restore points not preserved: available=%d locks=%d err=%v", available, locks, err)
	}
}

func TestQuotaReservationRecoveryPreservesActiveAndDataReadyLocks(t *testing.T) {
	for _, stage := range []string{"reserved", "data_ready", "deleting"} {
		t.Run(stage, func(t *testing.T) {
			repo, plan := quotaFixture(t, 1)
			old := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
			backup := quotaBackup(t, repo, plan)
			if stage != "reserved" {
				if _, err := repo.db.Exec(`update restore_point_reservations set state=$2 where task_id=$1`, backup.ID, stage); err != nil {
					t.Fatal(err)
				}
				if _, err := repo.db.Exec(`update tasks set status='failed' where id=$1`, backup.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.PendingRestorePointReplacements(); err != nil {
				t.Fatal(err)
			}
			_, nextErr := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup"})
			point, _, err := repo.GetRestorePoint(old.ID)
			if err != nil || point.Metadata["replacementTaskId"] != backup.ID || !errors.Is(nextErr, ErrProtectionPlanBusy) {
				t.Fatalf("unsafe lock release: point=%v nextErr=%v err=%v", point, nextErr, err)
			}
		})
	}
}

func TestQuotaConcurrentBackupsCannotStealPendingPublicationSlot(t *testing.T) {
	repo, plan := quotaFixture(t, 1)
	old := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	var workers sync.WaitGroup
	var mutex sync.Mutex
	var winners []Task
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup"})
			mutex.Lock()
			defer mutex.Unlock()
			if err != nil && !errors.Is(err, ErrProtectionPlanBusy) {
				t.Error(err)
			}
			if task.Status == "queued" {
				winners = append(winners, task)
			}
		}()
	}
	workers.Wait()
	if len(winners) != 1 {
		t.Fatalf("reserved replacement %d times", len(winners))
	}
	input := RestorePointInput{SourceClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, BackupTaskID: winners[0].ID, VeleroBackupName: "concurrent-pending"}
	if _, err := repo.CreateRestorePoint(input); !errors.Is(err, ErrRestorePointReplacementPending) {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateRestorePointState(RestorePointStateInput{ID: old.ID, Status: "deleted"}); err != nil {
		t.Fatal(err)
	}
	contender, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup"})
	if !errors.Is(err, ErrProtectionPlanBusy) {
		t.Fatalf("pending publication slot stolen: %v %v", contender, err)
	}
	if _, err := repo.CreateRestorePoint(input); err != nil {
		t.Fatal(err)
	}
}

func quotaFixture(t *testing.T, quota int) (*PostgresStore, ProtectionPlan) {
	t.Helper()
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	policy, err := repo.CreatePolicy(PolicyInput{Name: "quota", Composition: "combined", RetentionCount: quota, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{SourceClusterID: cluster.ID, AppID: app.ID, PolicyID: policy.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	return repo, plan
}

func quotaBackup(t *testing.T, repo *PostgresStore, plan ProtectionPlan) Task {
	t.Helper()
	task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: "backup", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func quotaPoint(t *testing.T, repo *PostgresStore, plan ProtectionPlan, task Task) RestorePoint {
	t.Helper()
	point, err := repo.CreateRestorePoint(RestorePointInput{SourceClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, BackupTaskID: task.ID, VeleroBackupName: "backup-" + task.ID, TaskCreatedAt: task.CreatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: "succeeded", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	return point
}

func TestQuotaReplacementRollbackAndPublication(t *testing.T) {
	repo, plan := quotaFixture(t, 1)
	old := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	replacement := quotaBackup(t, repo, plan)
	locked, _, _ := repo.GetRestorePoint(old.ID)
	if locked.Metadata["replacementTaskId"] != replacement.ID {
		t.Fatal("replacement was not locked")
	}
	if err := repo.SetRestorePointRetention(old.ID, old.TenantID, true); !errors.Is(err, ErrRestorePointLocked) {
		t.Fatalf("locked retention err=%v", err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: replacement.ID, Status: "failed", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	locked, _, _ = repo.GetRestorePoint(old.ID)
	if locked.Status != "available" || locked.Metadata["replacementTaskId"] != nil {
		t.Fatal("failed backup did not restore old point")
	}
	replacement = quotaBackup(t, repo, plan)
	input := RestorePointInput{SourceClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, BackupTaskID: replacement.ID, VeleroBackupName: "replacement", TaskCreatedAt: replacement.CreatedAt}
	if _, err := repo.CreateRestorePoint(input); !errors.Is(err, ErrRestorePointReplacementPending) {
		t.Fatalf("published before old deleted: %v", err)
	}
	pending, err := repo.PendingRestorePointReplacements()
	if err != nil || len(pending) != 1 || pending[0].Input.VeleroBackupName != "replacement" {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	if _, _, err := repo.UpdateRestorePointState(RestorePointStateInput{ID: old.ID, Status: "deleted"}); err != nil {
		t.Fatal(err)
	}
	created, err := repo.CreateRestorePoint(pending[0].Input)
	if err != nil || created.Status != "available" {
		t.Fatalf("publication failed: %v", err)
	}
	duplicate, err := repo.CreateRestorePoint(pending[0].Input)
	if err != nil || duplicate.ID != created.ID {
		t.Fatalf("non-idempotent publication %v", err)
	}
}

func TestQuotaAllRetainedAndReservationContention(t *testing.T) {
	repo, plan := quotaFixture(t, 1)
	point := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	if err := repo.SetRestorePointRetention(point.ID, point.TenantID, true); err != nil {
		t.Fatal(err)
	}
	if task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup"}); err != nil || task.Status != "failed" || task.ErrorCode != "RESTORE_POINT_QUOTA_EXHAUSTED" {
		t.Fatalf("retained point task=%v err=%v", task, err)
	}
	if err := repo.ClaimRestorePointDeletion([]string{point.ID}); !errors.Is(err, ErrRestorePointLocked) {
		t.Fatalf("manual deletion bypassed protection: %v", err)
	}
	if err := repo.SetRestorePointRetention(point.ID, point.TenantID, false); err != nil {
		t.Fatal(err)
	}
	quotaBackup(t, repo, plan)
	if task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup"}); !errors.Is(err, ErrProtectionPlanBusy) {
		t.Fatalf("same point reserved twice: %v %v", task, err)
	}
}

func TestQuotaSevenRetainedPointsFailNextBackupWithoutDeleting(t *testing.T) {
	repo, plan := quotaFixture(t, 7)
	for range 7 {
		point := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
		if err := repo.SetRestorePointRetention(point.ID, point.TenantID, true); err != nil {
			t.Fatal(err)
		}
	}
	failed, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, Type: "backup"})
	if err != nil || failed.Status != "failed" || failed.ErrorCode != "RESTORE_POINT_QUOTA_EXHAUSTED" {
		t.Fatalf("quota failure missing: %v %v", failed, err)
	}
	points, err := repo.ListRestorePoints(RestorePointFilter{ProtectionPlanID: plan.ID})
	if err != nil || len(points) != 7 {
		t.Fatalf("retained points changed: %v %v", points, err)
	}
	for _, point := range points {
		if point.Status != "available" || point.Metadata["manualRetained"] != true || point.Metadata["replacementTaskId"] != nil {
			t.Fatalf("protected point modified: %v", point)
		}
	}
}

func TestDrillProtectionTransfersOnlyOnSuccess(t *testing.T) {
	repo, plan := quotaFixture(t, 3)
	first := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	second := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	drill := func(point RestorePoint, status string) Task {
		task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, RestorePointID: point.ID, Type: "drill"})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Status: status, MarkDone: true}); err != nil {
			t.Fatal(err)
		}
		return task
	}
	oldDrill := drill(first, "succeeded")
	if err := repo.SetRestorePointRetention(first.ID, first.TenantID, true); err != nil {
		t.Fatal(err)
	}
	drill(second, "failed")
	current, _, _ := repo.GetRestorePoint(first.ID)
	if current.Metadata["drillRetained"] != true {
		t.Fatal("failed drill released previous protection")
	}
	time.Sleep(time.Millisecond)
	drill(second, "succeeded")
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: oldDrill.ID, Status: "succeeded", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	current, _, _ = repo.GetRestorePoint(first.ID)
	latest, _, _ := repo.GetRestorePoint(second.ID)
	if current.Metadata["drillRetained"] == true || current.Metadata["manualRetained"] != true || latest.Metadata["drillRetained"] != true {
		t.Fatalf("incorrect transfer: %v %v", current.Metadata, latest.Metadata)
	}
}
