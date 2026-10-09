package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestUnregisterCleanupShutdownAndRestartCannotSkipPreflight(t *testing.T) {
	repo := newTestStore(t)
	cluster := testTenantCluster(t, repo, store.DefaultTenantID, "unregister-restart")
	repository, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: cluster.TenantID, Name: "unregister-storage", Type: "S3", Bucket: "test"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, Type: "unregister", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{"cleanupObjectStorage": true, "storageRepositoryIds": []string{repository.ID}, "namespace": "hypercdr-agent"}})
	if err != nil {
		t.Fatal(err)
	}
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
	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	entered := make(chan struct{}, 1)
	var calls atomic.Int64
	cleanObjectStoragePrefix = func(ctx context.Context, repository store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		return objectStorageCleanupResult{}, ctx.Err()
	}
	old := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	closeRouter := func(h *managedRouter) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	defer closeRouter(old)
	old.router.hub.set(cluster.ID, conn)
	old.router.redispatchPendingTasks(cluster.ID, conn)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup not started")
	}
	// A second reconnect while the cleanup is blocked must not start another worker.
	old.router.redispatchPendingTasks(cluster.ID, conn)
	if calls.Load() != 1 {
		t.Fatal("duplicate cleanup worker")
	}
	closeRouter(old)
	persisted, found, err := repo.GetTask(task.ID)
	if err != nil || !found || persisted.Status != "queued" || boolPayload(persisted.Payload, "unregisterPreflightCompleted") || !persisted.CompletedAt.IsZero() {
		t.Fatalf("shutdown lost resumable task: %#v %v", persisted, err)
	}
	if err := old.router.dispatchStoredTask(conn, persisted); err == nil {
		t.Fatal("uninstall bypassed incomplete cleanup")
	}
	// Also emulate a hard process loss, where the last durable status was running.
	persisted, _, err = repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "running", Progress: 10})
	if err != nil {
		t.Fatal(err)
	}
	cleanObjectStoragePrefix = func(ctx context.Context, repository store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
		calls.Add(1)
		if ctx.Err() != nil {
			return objectStorageCleanupResult{}, ctx.Err()
		}
		return objectStorageCleanupResult{RepositoryID: repository.ID, Prefix: prefix}, nil
	}
	next := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	defer closeRouter(next)
	next.router.hub.set(cluster.ID, conn)
	next.router.redispatchPendingTasks(cluster.ID, conn)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	var dispatch protocol.Message[protocol.TaskDispatchPayload]
	if err := client.ReadJSON(&dispatch); err != nil {
		t.Fatal(err)
	}
	if dispatch.Payload.Type != "unregister" || calls.Load() != 2 {
		t.Fatalf("restart did not finish preflight: %s calls=%d", dispatch.Payload.Type, calls.Load())
	}
	closeRouter(next)
	persisted, found, err = repo.GetTask(task.ID)
	if err != nil || !found || !boolPayload(persisted.Payload, "unregisterPreflightCompleted") || persisted.Status != "dispatched" {
		t.Fatalf("completion boundary not persisted: %#v %v", persisted, err)
	}
	// A later reconnect uses the durable completion marker rather than deleting twice.
	last := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	defer closeRouter(last)
	last.router.hub.set(cluster.ID, conn)
	last.router.redispatchPendingTasks(cluster.ID, conn)
	if err := client.ReadJSON(&dispatch); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("completed cleanup repeated after handoff")
	}
}

func TestUnregisterCleanupFailureNeverDispatchesUninstall(t *testing.T) {
	repo := newTestStore(t)
	cluster := testTenantCluster(t, repo, store.DefaultTenantID, "cleanup-failure")
	repository, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: cluster.TenantID, Name: "failed-storage", Type: "S3", Bucket: "test"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: cluster.ID, Type: "unregister", Status: "queued", Payload: map[string]any{"cleanupObjectStorage": true, "storageRepositoryIds": []string{repository.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	cleanObjectStoragePrefix = func(context.Context, store.StorageRepository, string) (objectStorageCleanupResult, error) {
		return objectStorageCleanupResult{}, errors.New("object store unavailable")
	}
	r := &Router{store: repo, hub: newSessionHub(), logger: slog.Default()}
	r.cleanupAndDispatchUnregister(task, []string{repository.ID}, false)
	persisted, found, err := repo.GetTask(task.ID)
	if err != nil || !found || persisted.Status != "failed" || persisted.ErrorCode != "OBJECT_STORAGE_CLEANUP_FAILED" || boolPayload(persisted.Payload, "unregisterPreflightCompleted") {
		t.Fatalf("failure not preserved: %#v %v", persisted, err)
	}
}

func TestUnregisterCleanupValidatesAllRepositoryReferencesBeforeDeleting(t *testing.T) {
	repo := newTestStore(t)
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "foreign-cleanup", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	cluster := testTenantCluster(t, repo, store.DefaultTenantID, "cleanup-references")
	own, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: cluster.TenantID, Name: "own-cleanup", Type: "S3"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := repo.CreateStorageRepository(store.StorageRepositoryInput{TenantID: tenant.ID, Name: "foreign-cleanup", Type: "S3"})
	if err != nil {
		t.Fatal(err)
	}
	previous := cleanObjectStoragePrefix
	defer func() { cleanObjectStoragePrefix = previous }()
	calls := 0
	cleanObjectStoragePrefix = func(context.Context, store.StorageRepository, string) (objectStorageCleanupResult, error) {
		calls++
		return objectStorageCleanupResult{}, nil
	}
	r := &Router{store: repo}
	for _, id := range []string{foreign.ID, store.NewPublicID()} {
		if _, err := r.cleanupClusterObjectStorageRepositories(context.Background(), cluster.ID, []string{own.ID, id}); err == nil {
			t.Fatal("invalid repository accepted")
		}
		if calls != 0 {
			t.Fatal("partially deleted storage before validating the reference set")
		}
	}
}
