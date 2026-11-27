package httpserver

import (
	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestClusterSyncSettingsScopeAndValidation(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	own := testTenantCluster(t, repo, actor.TenantID, "sync-own")
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "sync-foreign", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := testTenantCluster(t, repo, tenant.ID, "sync-foreign")
	router := &Router{store: repo}
	for _, scenario := range []struct {
		method, cluster, body string
		status                int
	}{
		{"GET", own.ID, "", 200}, {"GET", foreign.ID, "", 404},
		{"PUT", foreign.ID, `{"maxSyncTasks":4,"nodeTransferConcurrency":4}`, 404},
		{"PUT", own.ID, `{"maxSyncTasks":0,"nodeTransferConcurrency":4}`, 400},
		{"PUT", own.ID, `{"maxSyncTasks":4.5,"nodeTransferConcurrency":4}`, 400},
		{"PUT", own.ID, `{"maxSyncTasks":4,"nodeTransferConcurrency":99}`, 400},
		{"PUT", own.ID, `{"maxSyncTasks":8,"nodeTransferConcurrency":6}`, 200},
	} {
		req := tenantRequest(httptest.NewRequest(scenario.method, "/api/v1/clusters/"+scenario.cluster+"/sync-settings", strings.NewReader(scenario.body)), actor)
		req.SetPathValue("id", scenario.cluster)
		response := httptest.NewRecorder()
		router.clusterSyncSettings(response, req)
		if response.Code != scenario.status {
			t.Fatalf("%+v: %d %s", scenario, response.Code, response.Body.String())
		}
	}
	if !requiresAdmin(httptest.NewRequest("PUT", "/api/v1/clusters/id/sync-settings", nil)) {
		t.Fatal("sync settings must require admin")
	}
}

func TestSyncQueueDispatchesFourAndResumesAfterCompletion(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "sync-queue")
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(w, req, nil)
		if err == nil {
			accepted <- connection
		}
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	connection := <-accepted
	defer connection.Close()
	router := &Router{store: repo, hub: newSessionHub(), logger: slog.Default()}
	router.hub.set(cluster.ID, connection)
	tasks := []store.Task{}
	for index := 0; index < 6; index++ {
		task, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, Type: "backup", CommandID: store.NewPublicID()})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	router.redispatchPendingSyncTasks(cluster.ID, connection)
	for index := 0; index < 4; index++ {
		var message protocol.Message[protocol.TaskDispatchPayload]
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := client.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Payload.TaskID != tasks[index].ID {
			t.Fatalf("dispatch order %d: %s", index, message.Payload.TaskID)
		}
	}
	for _, task := range tasks[4:] {
		current, _, err := repo.GetTask(task.ID)
		if err != nil || current.Status != "queued" || !current.StartedAt.IsZero() || !current.DispatchedAt.IsZero() {
			t.Fatalf("excess task dispatched: %+v %v", current, err)
		}
	}
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: tasks[0].ID, Status: "succeeded", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	router.redispatchPendingSyncTasks(cluster.ID, connection)
	var next protocol.Message[protocol.TaskDispatchPayload]
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := client.ReadJSON(&next); err != nil || next.Payload.TaskID != tasks[4].ID {
		t.Fatalf("queue did not resume: %+v %v", next, err)
	}
}

func TestSyncSettingsSameImageRequiresRolloutCompletion(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	cluster := testTenantCluster(t, repo, actor.TenantID, "sync-runtime")
	if err := repo.SaveClusterSyncSettings(cluster.ID, store.DefaultClusterSyncSettings()); err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, Type: "velero-upgrade", Status: "running", Payload: map[string]any{"syncSettingsApply": true, "image": "velero:same", "concurrentBackups": 4, "nodeAgentConcurrency": 4}})
	if err != nil {
		t.Fatal(err)
	}
	cluster.VeleroImage = "velero:same"
	cluster.VeleroServerReady = true
	cluster.VeleroNodeAgentDesired = 1
	cluster.VeleroNodeAgentReady = 1
	router := &Router{store: repo}
	router.completeVeleroUpgradeAfterHeartbeat(cluster)
	current, _, err := repo.GetTask(task.ID)
	if err != nil || !current.CompletedAt.IsZero() {
		t.Fatalf("same-image heartbeat falsely completed apply: %+v %v", current, err)
	}
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Payload: map[string]any{"syncRuntimeRolloutCompleted": true}}); err != nil {
		t.Fatal(err)
	}
	router.completeVeleroUpgradeAfterHeartbeat(cluster)
	current, _, err = repo.GetTask(task.ID)
	if err != nil || current.Status != "succeeded" {
		t.Fatalf("verified rollout not completed: %+v %v", current, err)
	}
	if componentUpgradeTimedOut(store.Task{Type: "backup", Status: "queued", CreatedAt: time.Now().Add(-24 * time.Hour)}, time.Now()) {
		t.Fatal("queued backup consumed upgrade timeout")
	}
}
