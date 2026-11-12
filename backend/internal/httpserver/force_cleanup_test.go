package httpserver

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestForceCleanupCapabilityAndBusyGuards(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "force-cleanup")
	app := seedSchedulerApplication(t, repo, cluster.ID, "demo")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: actor.TenantID, SourceClusterID: cluster.ID, AppID: app.ID, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true}
	remove := func() *httptest.ResponseRecorder {
		request := tenantRequest(httptest.NewRequest("DELETE", "/api/v1/protection-plans/"+plan.ID+"?force=true", nil), actor)
		request.SetPathValue("id", plan.ID)
		response := httptest.NewRecorder()
		router.tenantGuard("plan", router.deleteProtectionPlan)(response, request)
		return response
	}
	if response := remove(); response.Code != 409 {
		t.Fatalf("unsupported force: %d %s", response.Code, response.Body.String())
	}
	unchanged, _, _ := repo.GetProtectionPlan(plan.ID)
	if unchanged.Status != "active" {
		t.Fatal("unsupported agent must not change the plan")
	}
	connection := &websocket.Conn{}
	router.hub.set(cluster.ID, connection)
	router.hub.setForceCleanupSupport(cluster.ID, connection, true)
	if _, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if response := remove(); response.Code != 409 {
		t.Fatalf("force bypassed active task guard: %d %s", response.Code, response.Body.String())
	}
	router.hub.set(cluster.ID, &websocket.Conn{})
	if router.hub.supportsForceCleanup(cluster.ID) {
		t.Fatal("reconnecting old agent inherited capability")
	}
}

func TestForceCleanupCommandPersistsAndRequiresCapableAgent(t *testing.T) {
	repo := newTestStore(t)
	cluster := seedSchedulerCluster(t, repo)
	app := seedSchedulerApplication(t, repo, cluster, "force-payload")
	storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{Name: "force-storage", Type: "s3", Endpoint: "http://minio:9000", Bucket: "bucket"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: cluster, AppID: app.ID, StorageRepoID: storage.ID, Status: "cleanup_running"})
	if err != nil {
		t.Fatal(err)
	}
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub(), workersClosing: true}
	task, _, err := router.createProtectionCleanupTask(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	if !boolPayload(task.Payload, "force") {
		t.Fatal("force flag not persisted")
	}
	if _, err := router.buildStoredTaskDispatch(task); err == nil {
		t.Fatal("unsupported agent accepted force command")
	}
	connection := &websocket.Conn{}
	router.hub.set(cluster, connection)
	router.hub.setForceCleanupSupport(cluster, connection, true)
	message, err := router.buildStoredTaskDispatch(task)
	if err != nil || !message.Payload.ProtectionCleanup.Force {
		t.Fatalf("force flag lost: %+v %v", message, err)
	}
	legacy := protectionCleanupCommandFromPayload(map[string]any{"planId": plan.ID})
	if legacy.Force {
		t.Fatal("legacy command must default to normal cleanup")
	}
}
