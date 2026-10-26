package store

import (
	"errors"
	"sync"
	"testing"
)

func completeResultInput(plan ProtectionPlan, task Task, name string) RestorePointInput {
	return RestorePointInput{ProtectionPlanID: plan.ID, SourceClusterID: plan.SourceClusterID, BackupTaskID: task.ID, VeleroBackupName: name, SizeMetricsV2: map[string]any{
		"schemaVersion": 2, "logical": map[string]any{"known": true, "totalBytes": 1024}, "newData": map[string]any{"known": true, "totalBytes": 0},
	}}
}

func TestBackupResultAtomicPublicationAndConflict(t *testing.T) {
	repo, plan := quotaFixture(t, 1)
	task := quotaBackup(t, repo, plan)
	input := completeResultInput(plan, task, "atomic-result")
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := repo.PublishBackupResult(input); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	points, err := repo.ListRestorePoints(RestorePointFilter{ProtectionPlanID: plan.ID})
	if err != nil || len(points) != 1 {
		t.Fatalf("duplicate publication: %v %v", points, err)
	}
	finished, _, err := repo.GetTask(task.ID)
	if err != nil || finished.Status != "succeeded" || finished.RestorePointID != points[0].ID || finished.CompletedAt.IsZero() {
		t.Fatalf("non-atomic task: %v %v", finished, err)
	}
	var completions int
	if err := repo.db.QueryRow(`select count(*) from task_events where task_id=$1 and reason='completed'`, task.ID).Scan(&completions); err != nil || completions != 1 {
		t.Fatalf("duplicate completion events: %d %v", completions, err)
	}
	input.SizeMetricsV2 = map[string]any{"schemaVersion": 2, "logical": map[string]any{"known": true, "totalBytes": 1024}, "newData": map[string]any{"known": true, "totalBytes": 12}}
	if _, err := repo.PublishBackupResult(input); !errors.Is(err, ErrBackupResultConflict) {
		t.Fatalf("accepted conflicting result: %v", err)
	}
	point, _, _ := repo.GetRestorePoint(points[0].ID)
	if point.SizeMetricsV2["newData"].(map[string]any)["totalBytes"] != float64(0) {
		t.Fatal("result was overwritten")
	}
	if _, _, err := repo.UpdateRestorePointState(RestorePointStateInput{ID: point.ID, Status: "deleted"}); err != nil {
		t.Fatal(err)
	}
	if replayed, err := repo.PublishBackupResult(completeResultInput(plan, task, "atomic-result")); err != nil || replayed.Status != "deleted" {
		t.Fatalf("late result resurrected deleted point: %v %v", replayed, err)
	}
}

func TestBackupResultValidationPrecedesQuotaDeletion(t *testing.T) {
	repo, plan := quotaFixture(t, 1)
	old := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	task := quotaBackup(t, repo, plan)
	input := completeResultInput(plan, task, "replacement-result")
	input.SizeMetricsV2 = nil
	if _, err := repo.PublishBackupResult(input); !errors.Is(err, ErrBackupResultIncomplete) {
		t.Fatalf("incomplete accepted: %v", err)
	}
	var state string
	if err := repo.db.QueryRow(`select state from restore_point_reservations where task_id=$1`, task.ID).Scan(&state); err != nil || state != "reserved" {
		t.Fatalf("deletion started without result: %s %v", state, err)
	}
	input = completeResultInput(plan, task, "replacement-result")
	if _, err := repo.PublishBackupResult(input); !errors.Is(err, ErrRestorePointReplacementPending) {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Payload: map[string]any{"sizeMetricsV2": map[string]any{"garbage": true}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateRestorePointState(RestorePointStateInput{ID: old.ID, Status: "deleted"}); err != nil {
		t.Fatal(err)
	}
	point, err := repo.PublishBackupResult(input)
	if err != nil || point.SizeMetricsV2["newData"] == nil {
		t.Fatalf("immutable pending result lost: %v %v", point, err)
	}
}

func TestBackupResultTransactionRollsBackPublication(t *testing.T) {
	repo, plan := quotaFixture(t, 1)
	task := quotaBackup(t, repo, plan)
	_, err := repo.db.Exec(`create function reject_backup_success() returns trigger language plpgsql as $$ begin if new.status='succeeded' then raise exception 'injected publication crash'; end if; return new; end $$; create trigger reject_backup_success before update on tasks for each row execute function reject_backup_success()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PublishBackupResult(completeResultInput(plan, task, "rollback-result")); err == nil {
		t.Fatal("fault not injected")
	}
	points, err := repo.ListRestorePoints(RestorePointFilter{ProtectionPlanID: plan.ID})
	current, _, _ := repo.GetTask(task.ID)
	if err != nil || len(points) != 0 || current.Payload["backupResult"] != nil || current.Status != "queued" {
		t.Fatalf("partial transaction: %v %v %v", points, current, err)
	}
}
