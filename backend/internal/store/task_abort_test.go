package store

import (
	"sync"
	"testing"
)

func TestConcurrentAbortRequestsAtomicallyClaimOneTaskDuringCleanup(t *testing.T) {
	repo, plan := quotaFixture(t, 3)
	_, _, _ = repo.UpdateProtectionPlanStatus(plan.ID, "cleanup_running")
	target, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: "protection-cleanup", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan Task, 8)
	errors := make(chan error, 8)
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			task, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: "task-abort", Payload: map[string]any{"targetTaskId": target.ID}})
			results <- task
			errors <- err
		}()
	}
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	ids := map[string]bool{}
	for task := range results {
		ids[task.ID] = true
	}
	if len(ids) != 1 {
		t.Fatalf("created %d abort commands", len(ids))
	}
	updated, _, _ := repo.GetTask(target.ID)
	if updated.Status != "canceling" || updated.Payload["abortRequested"] != true || !ids[updated.Payload["abortTaskId"].(string)] {
		t.Fatalf("target was not atomically claimed: %+v", updated)
	}
	if _, err := repo.CreateTask(TaskInput{ProtectionPlanID: plan.ID, ClusterID: plan.SourceClusterID, Type: "backup"}); err == nil {
		t.Fatal("abort bypassed the new-work guard")
	}
}
