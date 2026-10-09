package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

func registrationTestRouter(t *testing.T, repo store.Store) *Router {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &Router{store: repo, cfg: config.Config{AgentNamespace: "hypercdr-agent"}, logger: slog.Default(), workerContext: ctx, cceRegistrationUploads: map[string]cceKubeconfigUpload{}}
	t.Cleanup(func() { cancel(); r.workers.Wait() })
	return r
}

func registrationTestUpload(t *testing.T, r *Router, actor store.User) string {
	t.Helper()
	id, err := secureRegistrationID()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(validCCEKubeconfig), 0600); err != nil {
		t.Fatal(err)
	}
	r.cceRegistrationUploads[id] = cceKubeconfigUpload{ID: id, TenantID: actor.TenantID, OwnerID: actor.ID, Path: path, ClusterType: "huaweicloud-cce", Contexts: []string{"internal"}, Inspected: map[string]bool{"internal": true}, ExpiresAt: time.Now().Add(time.Hour)}
	return id
}

func registrationTestStart(r *Router, actor store.User, body cceDirectInstallRequest) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	r.startCCEDirectRegistration(w, tenantRequest(httptest.NewRequest("POST", "/api/v1/cluster-registrations/cce/tasks", strings.NewReader(string(raw))), actor))
	return w
}

func TestRegistrationConcurrentRetriesReuseOneTaskAndSealedRequest(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	r := registrationTestRouter(t, repo)
	id := registrationTestUpload(t, r, actor)
	body := cceDirectInstallRequest{SessionID: id, Context: "internal", StorageClass: "csi-disk", IdempotencyKey: "concurrent-request-0001"}
	const clients = 20
	results := make(chan *httptest.ResponseRecorder, clients)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- registrationTestStart(r, actor, body) }()
	}
	close(start)
	wg.Wait()
	close(results)
	created := 0
	taskID := ""
	for w := range results {
		if w.Code != 200 && w.Code != 202 {
			t.Fatalf("retry: %d %s", w.Code, w.Body.String())
		}
		if w.Code == 202 {
			created++
		}
		var task store.Task
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if taskID == "" {
			taskID = task.ID
		}
		if task.ID != taskID {
			t.Fatalf("duplicate task: %s != %s", task.ID, taskID)
		}
	}
	if created != 1 {
		t.Fatalf("created %d tasks", created)
	}
	items, err := repo.ListTasksFiltered(store.TaskFilter{TenantID: actor.TenantID, Types: []string{"cluster-registration"}})
	if err != nil || len(items) != 1 {
		t.Fatalf("task count: %d %v", len(items), err)
	}
	requestPath := filepath.Join(filepath.Dir(r.cceRegistrationUploads[id].Path), "install-request.json")
	original, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	deleteReq := tenantRequest(httptest.NewRequest("DELETE", "/api/v1/cluster-registrations/cce/kubeconfigs/"+id, nil), actor)
	deleteReq.SetPathValue("id", id)
	deleted := httptest.NewRecorder()
	r.deleteCCEKubeconfig(deleted, deleteReq)
	if deleted.Code != 409 {
		t.Fatalf("active credentials deleted: %d %s", deleted.Code, deleted.Body.String())
	}
	changed := body
	changed.StorageClass = "other-storage"
	if w := registrationTestStart(r, actor, changed); w.Code != 409 {
		t.Fatalf("changed settings reused key: %d %s", w.Code, w.Body.String())
	}
	changed = body
	changed.IdempotencyKey = "concurrent-request-0002"
	if w := registrationTestStart(r, actor, changed); w.Code != 409 {
		t.Fatalf("session reused with different key: %d %s", w.Code, w.Body.String())
	}
	after, err := os.ReadFile(requestPath)
	if err != nil || string(after) != string(original) {
		t.Fatalf("rejected request overwrote sealed file: %v", err)
	}
	// A different process has no upload map; retries still acknowledge the
	// persisted exact request, but never dispatch or create it again.
	restarted := registrationTestRouter(t, repo)
	if w := registrationTestStart(restarted, actor, body); w.Code != 200 {
		t.Fatalf("restart retry: %d %s", w.Code, w.Body.String())
	}
	if _, _, err := repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: taskID, Status: "canceled", MarkDone: true}); err != nil {
		t.Fatal(err)
	}
	deleted = httptest.NewRecorder()
	r.deleteCCEKubeconfig(deleted, deleteReq)
	if deleted.Code != 204 {
		t.Fatalf("canceled credentials not removed: %d %s", deleted.Code, deleted.Body.String())
	}
}

func TestRegistrationRequestKeysAreUploaderAndTenantScopedWithoutHistoryLimit(t *testing.T) {
	repo := newTestStore(t)
	actor := testAdmin(t, repo)
	other, err := repo.CreateUser(actor.TenantID, "registration-other@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := repo.CreateTenant(store.TenantInput{Name: "registration-other-tenant", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := repo.CreateUser(tenant.ID, "foreign@example.com", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	r := registrationTestRouter(t, repo)
	bodies := map[string]cceDirectInstallRequest{}
	ids := map[string]string{}
	for _, user := range []store.User{actor, other, foreign} {
		body := cceDirectInstallRequest{SessionID: registrationTestUpload(t, r, user), Context: "internal", IdempotencyKey: "same-request-key-0001"}
		w := registrationTestStart(r, user, body)
		if w.Code != 202 {
			t.Fatalf("independent user key: %d %s", w.Code, w.Body.String())
		}
		var task store.Task
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if ids[task.ID] != "" {
			t.Fatal("different actors shared task")
		}
		ids[task.ID] = user.ID
		bodies[user.ID] = body
	}
	// More than the old 200-task lookup cap must not hide the original request.
	for i := 0; i < 205; i++ {
		if _, err := repo.CreateTask(store.TaskInput{TenantID: actor.TenantID, Type: "cluster-registration"}); err != nil {
			t.Fatal(err)
		}
	}
	if w := registrationTestStart(r, actor, bodies[actor.ID]); w.Code != 200 {
		t.Fatalf("history changed idempotency: %d %s", w.Code, w.Body.String())
	}
	w := registrationTestStart(r, other, bodies[actor.ID])
	if w.Code != 409 || strings.Contains(w.Body.String(), bodies[actor.ID].SessionID) {
		t.Fatalf("another uploader retrieved task: %d %s", w.Code, w.Body.String())
	}
	if w := registrationTestStart(r, store.User{}, bodies[actor.ID]); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing owner: %d", w.Code)
	}
}
