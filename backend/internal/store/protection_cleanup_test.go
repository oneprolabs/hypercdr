package store

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestProtectionCleanupRejectsActiveTasksWithoutChanges(t *testing.T) {
	for _, taskType := range []string{"backup", "drill", "restore", "takeover", "schedule-sync", "storage-sync", "protection-cleanup"} {
		t.Run(taskType, func(t *testing.T) {
			repo, plan := quotaFixture(t, 3)
			point := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
			if _, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: taskType}); err != nil {
				t.Fatal(err)
			}
			if err := repo.BeginProtectionPlanCleanup(plan.ID); !errors.Is(err, ErrProtectionPlanBusy) {
				t.Fatalf("cleanup error = %v", err)
			}
			current, _, _ := repo.GetProtectionPlan(plan.ID)
			unchanged, _, _ := repo.GetRestorePoint(point.ID)
			if current.Status != plan.Status || unchanged.Metadata["retentionState"] != nil {
				t.Fatalf("rejected cleanup changed state: %s %v", current.Status, unchanged.Metadata)
			}
		})
	}
}

func TestProtectionCleanupAndTaskCreationSerialize(t *testing.T) {
	for range 10 {
		repo, plan := quotaFixture(t, 3)
		start := make(chan struct{})
		var cleanupErr, taskErr error
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			cleanupErr = repo.BeginProtectionPlanCleanup(plan.ID)
		}()
		go func() {
			defer workers.Done()
			<-start
			_, taskErr = repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: "backup"})
		}()
		close(start)
		workers.Wait()
		if cleanupErr == nil {
			if !errors.Is(taskErr, ErrProtectionPlanBusy) {
				t.Fatalf("task creation raced past cleanup: %v", taskErr)
			}
			if err := repo.BeginProtectionPlanCleanup(plan.ID); !errors.Is(err, ErrProtectionPlanBusy) {
				t.Fatalf("duplicate cleanup was allowed: %v", err)
			}
		} else if !errors.Is(cleanupErr, ErrProtectionPlanBusy) || taskErr != nil {
			t.Fatalf("unexpected race result cleanup=%v task=%v", cleanupErr, taskErr)
		}
	}
}

func TestProtectionCleanupRejectsPointLinkedTasks(t *testing.T) {
	for _, payloadOnly := range []bool{false, true} {
		repo, plan := quotaFixture(t, 3)
		point := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
		input := TaskInput{ClusterID: plan.SourceClusterID, Type: "drill", RestorePointID: point.ID}
		if payloadOnly {
			input.RestorePointID = ""
			input.Payload = map[string]any{"restorePointId": point.ID}
		}
		if _, err := repo.CreateTask(input); err != nil {
			t.Fatal(err)
		}
		if err := repo.BeginProtectionPlanCleanup(plan.ID); !errors.Is(err, ErrProtectionPlanBusy) {
			t.Fatalf("point-linked task did not block cleanup: %v", err)
		}
		current, _, _ := repo.GetProtectionPlan(plan.ID)
		if current.Status != plan.Status {
			t.Fatalf("rejected cleanup changed plan: %s", current.Status)
		}
	}
}

func TestProtectionCleanupAndPointTaskCreationSerialize(t *testing.T) {
	for range 10 {
		repo, plan := quotaFixture(t, 3)
		point := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
		start := make(chan struct{})
		var cleanupErr, taskErr error
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			cleanupErr = repo.BeginProtectionPlanCleanup(plan.ID)
		}()
		go func() {
			defer workers.Done()
			<-start
			_, taskErr = repo.CreateTask(TaskInput{ClusterID: plan.SourceClusterID, Type: "drill", RestorePointID: point.ID})
		}()
		close(start)
		workers.Wait()
		if cleanupErr == nil {
			if !errors.Is(taskErr, ErrRestorePointLocked) {
				t.Fatalf("point task raced past cleanup: %v", taskErr)
			}
		} else if !errors.Is(cleanupErr, ErrProtectionPlanBusy) || taskErr != nil {
			t.Fatalf("unexpected race result cleanup=%v task=%v", cleanupErr, taskErr)
		}
	}
}

func TestProtectionCleanupOverridesRetentionButBlocksNewWork(t *testing.T) {
	repo, plan := quotaFixture(t, 3)
	if _, err := repo.UpsertProtectionPlanSchedule(ProtectionPlanScheduleInput{ProtectionPlanID: plan.ID, NextFireAt: time.Now().UTC(), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	point := quotaPoint(t, repo, plan, quotaBackup(t, repo, plan))
	if err := repo.SetRestorePointRetention(point.ID, point.TenantID, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimRestorePointDeletion([]string{point.ID}); !errors.Is(err, ErrRestorePointLocked) {
		t.Fatalf("ordinary deletion ignored retention: %v", err)
	}
	if err := repo.BeginProtectionPlanCleanup(plan.ID); err != nil {
		t.Fatal(err)
	}
	schedule, exists, err := repo.GetProtectionPlanSchedule(plan.ID)
	if err != nil || !exists || schedule.Enabled {
		t.Fatalf("cleanup did not disable schedule: %+v exists=%v err=%v", schedule, exists, err)
	}
	current, _, _ := repo.GetProtectionPlan(plan.ID)
	claimed, _, _ := repo.GetRestorePoint(point.ID)
	if current.Status != "cleanup_running" || claimed.Metadata["retentionState"] != "pending_delete" {
		t.Fatalf("cleanup not claimed: %s %v", current.Status, claimed.Metadata)
	}
	for _, taskType := range []string{"backup", "drill", "restore", "takeover", "schedule-sync"} {
		if _, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: taskType}); !errors.Is(err, ErrProtectionPlanBusy) {
			t.Fatalf("new %s was not blocked: %v", taskType, err)
		}
	}
	if _, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: "protection-cleanup", Payload: map[string]any{"cleanupMode": "source", "restorePoints": []map[string]any{{"id": point.ID}}}}); err != nil {
		t.Fatal(err)
	}
}
