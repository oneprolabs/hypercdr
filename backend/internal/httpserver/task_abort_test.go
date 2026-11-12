package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestTaskAbortCompletionFailsTargetAndReleasesCleanupState(t *testing.T) {
	repo := newTestStore(t)
	plan := testTenantPlan(t, repo, store.DefaultTenantID)
	_, _, _ = repo.UpdateProtectionPlanStatus(plan.ID, "cleanup_running")
	target, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "canceling", CommandID: store.NewPublicID(), Payload: map[string]any{"planId": plan.ID, "abortRequested": true}})
	if err != nil {
		t.Fatal(err)
	}
	abort, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: "task-abort", Status: "accepted", Payload: map[string]any{"targetTaskId": target.ID}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default()}
	if err := router.finishTaskAbort(plan.SourceClusterID, abort, protocol.TaskCompletedPayload{Message: "stopped"}); err != nil {
		t.Fatal(err)
	}
	updated, _, _ := router.findTaskByID(plan.SourceClusterID, target.ID)
	if updated.Status != "failed" || updated.ErrorCode != "TASK_ABORTED_BY_USER" || updated.CompletedAt.IsZero() {
		t.Fatalf("incorrect target state: %+v", updated)
	}
	updatedPlan, _, _ := repo.GetProtectionPlan(plan.ID)
	if updatedPlan.Status != "cleanup_failed" {
		t.Fatalf("plan still blocked: %s", updatedPlan.Status)
	}
	if err := router.finishTaskAbort(plan.SourceClusterID, abort, protocol.TaskCompletedPayload{Message: "duplicate"}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskAbortRequiresCapabilityAndReusesPendingRequest(t *testing.T) {
	repo := newTestStore(t)
	plan := testTenantPlan(t, repo, store.DefaultTenantID)
	target, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: "running", CommandID: store.NewPublicID(), Payload: map[string]any{"planId": plan.ID}})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub()}
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/tasks/"+target.ID+"/abort", nil)
		req.SetPathValue("id", target.ID)
		response := httptest.NewRecorder()
		router.abortTask(response, req)
		return response
	}
	if result := request(); result.Code != 409 {
		t.Fatalf("old agent accepted abort: %d", result.Code)
	}
	unchanged, _, _ := router.findTaskByID(plan.SourceClusterID, target.ID)
	if unchanged.Status != "running" {
		t.Fatal("unsupported request changed task state")
	}
	connections := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, req, nil)
		if err == nil {
			connections <- connection
		}
	}))
	defer server.Close()
	peer, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	connection := <-connections
	defer connection.Close()
	router.hub.set(plan.SourceClusterID, connection)
	router.hub.setTaskAbortSupport(plan.SourceClusterID, connection, true)
	if result := request(); result.Code != 202 {
		t.Fatalf("abort failed: %d %s", result.Code, result.Body.String())
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	var dispatch protocol.Message[protocol.TaskDispatchPayload]
	if err := peer.ReadJSON(&dispatch); err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.TaskAbort == nil || dispatch.Payload.TaskAbort.TaskID != target.ID {
		t.Fatal("wrong abort target")
	}
	if result := request(); result.Code != 200 {
		t.Fatalf("request was not reused: %d", result.Code)
	}
	tasks, _ := repo.ListTasks(plan.SourceClusterID)
	count := 0
	for _, task := range tasks {
		if task.Type == "task-abort" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("created %d abort requests", count)
	}
	router.hub.set(plan.SourceClusterID, &websocket.Conn{})
	if router.hub.supportsTaskAbort(plan.SourceClusterID) {
		t.Fatal("capability survived reconnect")
	}
}

func TestReconcileCleanupFailureImmediatelyAndMaintenanceTimeoutUpdatesPlan(t *testing.T) {
	for _, status := range []string{"failed", "running"} {
		t.Run(status, func(t *testing.T) {
			repo := newTestStore(t)
			plan := testTenantPlan(t, repo, store.DefaultTenantID)
			_, _, _ = repo.UpdateProtectionPlanStatus(plan.ID, "cleanup_running")
			task, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: "protection-cleanup", Status: status, Payload: map[string]any{"planId": plan.ID}})
			if err != nil {
				t.Fatal(err)
			}
			router := &Router{store: repo, logger: slog.Default()}
			if status == "running" {
				router.reconcileAbandonedMaintenanceTasks(task.CreatedAt.Add(36 * time.Minute))
			} else {
				router.reconcileProtectionCleanupPlans(time.Now())
			}
			updated, _, _ := repo.GetProtectionPlan(plan.ID)
			if updated.Status != "cleanup_failed" {
				t.Fatalf("failed task left plan cleaning: %s", updated.Status)
			}
		})
	}
}
