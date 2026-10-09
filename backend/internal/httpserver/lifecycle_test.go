package httpserver

import (
	"context"
	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestWorkerAdmissionCannotRaceShutdown(t *testing.T) {
	workerContext, stop := context.WithCancel(context.Background())
	r := &Router{workerContext: workerContext, stopWorkers: stop}
	h := &managedRouter{router: r}
	var started, exited atomic.Int64
	var callers sync.WaitGroup
	for i := 0; i < 100; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			r.startWorker(func() { started.Add(1); <-r.workerDone(); exited.Add(1) })
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	callers.Wait()
	if started.Load() != exited.Load() {
		t.Fatal("Close returned before admitted workers exited")
	}
	if r.startWorker(func() { t.Error("worker admitted after shutdown") }) {
		t.Fatal("admission remained open")
	}
}

func TestShutdownCancelsStoragePreflightWithoutFailingPersistedTask(t *testing.T) {
	for _, taskType := range []string{"backup", "restore"} {
		t.Run(taskType, func(t *testing.T) {
			repo := newTestStore(t)
			source := testTenantCluster(t, repo, store.DefaultTenantID, "shutdown-source")
			app := seedSchedulerApplication(t, repo, source.ID, "demo")
			storage, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: store.DefaultTenantID, Name: "shutdown-storage", Type: "S3", Endpoint: "localhost:9000", Bucket: "test"})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{TenantID: store.DefaultTenantID, SourceClusterID: source.ID, AppID: app.ID, StorageRepoID: storage.ID, Status: "ready"})
			if err != nil {
				t.Fatal(err)
			}
			task, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, AppID: plan.AppID, ProtectionPlanID: plan.ID, Type: taskType, Status: "queued", CommandID: store.NewPublicID()})
			if err != nil {
				t.Fatal(err)
			}
			h := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			defer h.Close(ctx)
			accepted := make(chan *websocket.Conn, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, req, nil)
				if err == nil {
					accepted <- conn
				}
			}))
			defer server.Close()
			client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			conn := <-accepted
			defer conn.Close()
			h.router.hub.set(plan.SourceClusterID, conn)
			h.router.startWorker(func() {
				if taskType == "backup" {
					h.router.dispatchBackupTaskAfterStorageSync(task, storage.Name, storage.ID, plan.SourceClusterID)
				} else {
					h.router.dispatchRecoveryTaskAfterStorageSync(task, storage.Name, storage.ID, plan.SourceClusterID)
				}
			})
			client.SetReadDeadline(time.Now().Add(3 * time.Second))
			var dispatch protocol.Message[protocol.TaskDispatchPayload]
			if err := client.ReadJSON(&dispatch); err != nil {
				t.Fatal(err)
			}
			if dispatch.Payload.Type != "storage-sync" {
				t.Fatalf("wrong preflight %s", dispatch.Payload.Type)
			}
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
			persisted, found, err := repo.GetTask(task.ID)
			if err != nil || !found {
				t.Fatalf("task missing: %v", err)
			}
			if persisted.Status != "queued" || persisted.ErrorCode == "STORAGE_SYNC_FAILED" {
				t.Fatalf("shutdown incorrectly failed task: %#v", persisted)
			}
			if taskType == "backup" {
				next := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
				nextCtx, nextCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer nextCancel()
				defer next.Close(nextCtx)
				next.router.hub.set(plan.SourceClusterID, conn)
				next.router.redispatchPendingTasks(plan.SourceClusterID, conn)
				client.SetReadDeadline(time.Now().Add(3 * time.Second))
				if err := client.ReadJSON(&dispatch); err != nil {
					t.Fatal(err)
				}
				if dispatch.Payload.Type != "storage-sync" {
					t.Fatalf("restart bypassed preflight: %s", dispatch.Payload.Type)
				}
				// The previous process may also have a queued/dispatched storage-sync task.
				// Confirm that reconnect did not mark the parent backup dispatched.
				persisted, _, err = repo.GetTask(task.ID)
				if err != nil || persisted.Status != "queued" {
					t.Fatalf("parent dispatched before storage readiness: %#v %v", persisted, err)
				}
				if err := next.Close(nextCtx); err != nil {
					t.Fatal(err)
				}
			}

		})
	}
}

func TestRouterBackgroundWorkersStopBeforeStoreClose(t *testing.T) {
	repo := newTestStore(t)
	handler := NewRouter(config.Config{RegistrationSessionDir: t.TempDir()}, slog.Default(), repo)
	closer, ok := handler.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("router has no lifecycle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := closer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := closer.Close(ctx); err != nil {
		t.Fatal("shutdown not idempotent", err)
	}
}

func TestKubeconfigUploadOwnerCannotBeImpersonatedWithinTenant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &Router{cceRegistrationUploads: map[string]cceKubeconfigUpload{
		"upload": {ID: "upload", TenantID: store.DefaultTenantID, OwnerID: "owner", Path: path, ExpiresAt: time.Now().Add(time.Minute)},
	}}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster-registrations/cce/kubeconfigs/upload", nil)
	req.SetPathValue("id", "upload")
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "other", TenantID: store.DefaultTenantID}))
	w := httptest.NewRecorder()
	r.deleteCCEKubeconfig(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner status %d", w.Code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("non-owner deleted upload", err)
	}
	req = req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, store.User{ID: "owner", TenantID: store.DefaultTenantID}))
	w = httptest.NewRecorder()
	r.deleteCCEKubeconfig(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("owner status %d", w.Code)
	}
}

func TestShutdownDrainsRegisteredAndUnregisteredAgentSockets(t *testing.T) {
	repo := newTestStore(t)
	old := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	server := httptest.NewServer(old)
	defer server.Close()
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "handoff", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	dial := func(url string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+url[4:]+"/ws/agent", nil)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	agent := dial(server.URL)
	defer agent.Close()
	halfRegistered := dial(server.URL)
	defer halfRegistered.Close()
	registration := protocol.Message[protocol.RegisterPayload]{Version: protocol.Version, Type: protocol.MessageAgentRegister, AgentID: "handoff-agent", Payload: protocol.RegisterPayload{InstallToken: token.Token, Cluster: protocol.ClusterSummary{Name: "handoff-cluster"}, Agent: protocol.AgentSummary{Version: "test"}, Velero: protocol.VeleroSummary{Status: "ready"}}}
	if err := agent.WriteJSON(registration); err != nil {
		t.Fatal(err)
	}
	var accepted protocol.Message[protocol.RegisterAcceptedPayload]
	if err := agent.ReadJSON(&accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Type != protocol.MessagePlatformRegisterAccepted {
		t.Fatalf("registration rejected: %s", accepted.Type)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{agent, halfRegistered} {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Fatal("old socket still live after Close")
		}
	}
	if old.router.hub.has(accepted.Payload.ClusterID) {
		t.Fatal("old process retained agent session")
	}
	clusters, err := repo.ListClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].ConnectionStatus != "offline" {
		t.Fatalf("handler not fully drained: %#v", clusters)
	}
	_, response, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:]+"/ws/agent", nil)
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("shutdown accepted another websocket")
	}
	if response != nil {
		response.Body.Close()
	}
	// The replacement uses the existing durable credential, never another install token.
	next := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	nextServer := httptest.NewServer(next)
	defer nextServer.Close()
	defer next.Close(ctx)
	replacement := dial(nextServer.URL)
	defer replacement.Close()
	registration.ClusterID = accepted.Payload.ClusterID
	registration.Payload.InstallToken = ""
	registration.Payload.AgentCredential = accepted.Payload.AgentCredential
	if err := replacement.WriteJSON(registration); err != nil {
		t.Fatal(err)
	}
	if err := replacement.ReadJSON(&accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Type != protocol.MessagePlatformRegisterAccepted {
		t.Fatalf("handoff credential rejected: %s", accepted.Type)
	}
	if err := next.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
