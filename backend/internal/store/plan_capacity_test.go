package store

import (
	"testing"
	"time"
)

func TestPlanCapacityPreservesSuccessAndRejectsStaleOrForeignResults(t *testing.T) {
	repo := newTestStore(t)
	_, plan := seedProtectionPlan(t, repo)
	now := time.Now().UTC().Truncate(time.Second)
	good := map[string]any{"totalBytes": 123, "metadataBytes": 23, "kopiaBytes": 100, "collectedAt": now, "requestedAt": now, "status": "succeeded"}
	if err := repo.SavePlanCapacityResult(plan.ID, plan.SourceClusterID, plan.TenantID, good, now); err != nil {
		t.Fatal(err)
	}
	failed := map[string]any{"status": "failed", "errorMessage": "unavailable", "requestedAt": now.Add(time.Hour)}
	if err := repo.SavePlanCapacityResult(plan.ID, plan.SourceClusterID, plan.TenantID, failed, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePlanCapacityResult(plan.ID, plan.SourceClusterID, plan.TenantID, good, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePlanCapacityResult(plan.ID, NewPublicID(), plan.TenantID, map[string]any{"totalBytes": 999}, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpdateProtectionPlanStorageSize(plan.ID, map[string]any{"totalBytes": 999}); err != nil {
		t.Fatal(err)
	}
	got, _, err := repo.GetProtectionPlan(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanStorageSize["totalBytes"] != float64(123) || got.PlanStorageSize["status"] != "failed" || got.PlanStorageSize["collectedAt"] == nil {
		t.Fatalf("capacity overwritten: %+v", got.PlanStorageSize)
	}
}

func TestCapacityTasksRejectConcurrentAndRepeatedDailyRuns(t *testing.T) {
	repo := newTestStore(t)
	cluster, _ := seedPlanApplication(t, repo)
	input := TaskInput{ClusterID: cluster.ID, Type: "plan-capacity", Status: "queued", Payload: map[string]any{"scheduleDay": "2026-11-06", "planCapacity": map[string]any{"plans": []map[string]any{{"planId": "selected"}}}}}
	first, err := repo.CreateTask(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []TaskFilter{{ClusterID: cluster.ID, CapacityPlanID: "selected", Limit: 1}, {ClusterID: cluster.ID, CapacityScheduleDay: "2026-11-06", Limit: 1}} {
		tasks, err := repo.ListTasksFiltered(filter)
		if err != nil || len(tasks) != 1 || tasks[0].ID != first.ID {
			t.Fatalf("capacity task filter: %+v %v", tasks, err)
		}
	}
	input.Payload = map[string]any{}
	if _, err := repo.CreateTask(input); err == nil {
		t.Fatal("overlapping collection accepted")
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: first.ID, Status: "succeeded", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	input.Payload = map[string]any{"scheduleDay": "2026-11-06"}
	if _, err := repo.CreateTask(input); err == nil {
		t.Fatal("repeated daily collection accepted")
	}
	input.Payload = map[string]any{"scheduleDay": "2026-11-07"}
	if _, err := repo.CreateTask(input); err != nil {
		t.Fatal(err)
	}
}
