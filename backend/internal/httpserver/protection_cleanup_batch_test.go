package httpserver

import (
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestProtectionCleanupTasksCompleteIgnoresHistoricalFailedRun(t *testing.T) {
	repo := store.NewMemoryStore()
	router := &Router{store: repo}
	plan := store.ProtectionPlan{ID: "plan-1", SourceClusterID: "source", TargetClusterID: "target"}
	inputs := []store.TaskInput{
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "failed", Payload: map[string]any{"cleanupMode": "source", "cleanupRunId": "old"}},
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "succeeded", Payload: map[string]any{"cleanupMode": "target", "cleanupRunId": "old"}},
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "succeeded", Payload: map[string]any{"cleanupMode": "source", "cleanupRunId": "new"}},
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "succeeded", Payload: map[string]any{"cleanupMode": "target", "cleanupRunId": "new"}},
	}
	for _, input := range inputs {
		if _, err := repo.CreateTask(input); err != nil {
			t.Fatal(err)
		}
	}
	complete, err := router.protectionCleanupTasksComplete(plan, "new")
	if err != nil || !complete {
		t.Fatalf("complete=%v err=%v, want successful current run", complete, err)
	}
}

func TestProtectionCleanupTasksCompleteLegacyTasksUseLatestAttempt(t *testing.T) {
	repo := store.NewMemoryStore()
	router := &Router{store: repo}
	plan := store.ProtectionPlan{ID: "plan-legacy", SourceClusterID: "source", TargetClusterID: "target"}
	for _, input := range []store.TaskInput{
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "failed", Payload: map[string]any{"cleanupMode": "source"}},
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "succeeded", Payload: map[string]any{"cleanupMode": "target"}},
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "succeeded", Payload: map[string]any{"cleanupMode": "source"}},
		{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "succeeded", Payload: map[string]any{"cleanupMode": "target"}},
	} {
		if _, err := repo.CreateTask(input); err != nil {
			t.Fatal(err)
		}
	}
	complete, err := router.protectionCleanupTasksComplete(plan, "")
	if err != nil || !complete {
		t.Fatalf("complete=%v err=%v, want latest legacy attempt successful", complete, err)
	}
}

func TestReconcileProtectionCleanupFinalizesSuccessfulTasks(t *testing.T) {
	repo := store.NewMemoryStore()
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: "source", TargetClusterID: "target", AppID: "app-1", Status: "cleanup_running"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"source", "target"} {
		if _, err := repo.CreateTask(store.TaskInput{ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "succeeded", Payload: map[string]any{"cleanupMode": mode}}); err != nil {
			t.Fatal(err)
		}
	}
	router := &Router{store: repo}
	router.reconcileProtectionCleanupPlans(plan.UpdatedAt)
	if _, ok, err := repo.GetProtectionPlan(plan.ID); err != nil || ok {
		t.Fatalf("plan remains after reconcile: ok=%v err=%v", ok, err)
	}
}
