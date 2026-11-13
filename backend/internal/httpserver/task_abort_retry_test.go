package httpserver

import (
	"log/slog"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestAbortFailureCanRetryAndLateResultCannotOverrideNewAttempt(t *testing.T) {
	repo := newTestStore(t)
	plan := testTenantPlan(t, repo, store.DefaultTenantID)
	target, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: "restore", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	createAbort := func() store.Task {
		t.Helper()
		task, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: "task-abort", CommandID: store.NewPublicID(), Payload: map[string]any{"targetTaskId": target.ID}})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	first := createAbort()
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: first.ID, Status: "failed", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	router.markTaskAbortFailed(first, "retryable test failure")
	current, _, err := repo.GetTask(target.ID)
	if err != nil || current.Status != "running" || current.Payload["abortRequested"] != false {
		t.Fatalf("failed abort left target blocked: %+v %v", current, err)
	}
	second := createAbort()
	if first.ID == second.ID {
		t.Fatal("failed attempt was reused")
	}
	current, _, _ = repo.GetTask(target.ID)
	if current.Payload["abortFailed"] != false {
		t.Fatal("retry did not clear failure state")
	}
	if err := router.finishTaskAbort(plan.SourceClusterID, first, protocol.TaskCompletedPayload{Message: "late old result"}); err != nil {
		t.Fatal(err)
	}
	current, _, _ = repo.GetTask(target.ID)
	if current.Status != "canceling" || current.Payload["abortTaskId"] != second.ID || !current.CompletedAt.IsZero() {
		t.Fatalf("late success changed current attempt: %+v", current)
	}
	router.markTaskAbortFailed(first, "late old failure")
	current, _, _ = repo.GetTask(target.ID)
	if current.Status != "canceling" {
		t.Fatal("late failure changed current attempt")
	}
	if err := router.finishTaskAbort(plan.SourceClusterID, second, protocol.TaskCompletedPayload{Message: "stopped"}); err != nil {
		t.Fatal(err)
	}
	current, _, _ = repo.GetTask(target.ID)
	if current.Status != "failed" || current.ErrorCode != "TASK_ABORTED_BY_USER" || current.CompletedAt.IsZero() {
		t.Fatalf("current attempt did not finish: %+v", current)
	}
}
