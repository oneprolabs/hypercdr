package httpserver

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestDrillCleanupSelectionSafety(t *testing.T) {
	repo := newTestStore(t)
	plan := testTenantPlan(t, repo, store.DefaultTenantID)
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub()}
	create := func(clusterID, namespace string) store.Task {
		t.Helper()
		task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plan.ID, Type: "drill", Status: "succeeded", Payload: map[string]any{"sourceNamespace": "app", "targetNamespace": namespace}})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	request := func(task store.Task) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/tasks/"+task.ID+"/cleanup-drill", nil)
		req.SetPathValue("id", task.ID)
		response := httptest.NewRecorder()
		router.cleanupDrillTask(response, req)
		return response
	}
	if response := request(create(plan.SourceClusterID, "app")); response.Code != 409 {
		t.Fatalf("source namespace must be protected: %d %s", response.Code, response.Body.String())
	}
	replaced := create(plan.TargetClusterID, "taken-over")
	if _, err := repo.CreateTask(store.TaskInput{ClusterID: plan.TargetClusterID, ProtectionPlanID: plan.ID, Type: "takeover", Status: "succeeded", Payload: map[string]any{"targetNamespace": "taken-over"}}); err != nil {
		t.Fatal(err)
	}
	if response := request(replaced); response.Code != 409 {
		t.Fatalf("old drill must not delete newer takeover resources: %d", response.Code)
	}
	old := create(plan.TargetClusterID, "app")
	current := create(plan.TargetClusterID, "app")
	if response := request(old); response.Code != 409 {
		t.Fatalf("stale drill should not delete a newer drill: %d %s", response.Code, response.Body.String())
	}
	response := request(current)
	if response.Code != 202 {
		t.Fatalf("cross-cluster original namespace should be cleanable: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Task store.Task `json:"task"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if namespaces, _ := body.Task.Payload["sourceNamespaces"].([]any); len(namespaces) != 0 {
		t.Fatal("source names from another cluster must not block target namespace cleanup")
	}
	_, err := repo.CreateTask(store.TaskInput{ClusterID: plan.TargetClusterID, ProtectionPlanID: plan.ID, Type: "drill", Payload: map[string]any{"targetNamespace": "app"}})
	if !errors.Is(err, store.ErrProtectionPlanBusy) {
		t.Fatalf("new drill must wait for namespace cleanup: %v", err)
	}
	if response := request(current); response.Code != 200 {
		t.Fatalf("repeat cleanup should reuse: %d", response.Code)
	}
}

func TestConcurrentDrillCleanupCreatesOneTask(t *testing.T) {
	repo := newTestStore(t)
	plan := testTenantPlan(t, repo, store.DefaultTenantID)
	drill, err := repo.CreateTask(store.TaskInput{ClusterID: plan.TargetClusterID, ProtectionPlanID: plan.ID, Type: "drill", Status: "failed", Payload: map[string]any{"sourceNamespace": "app", "targetNamespace": "app-drill"}})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 4
	wrapper := &concurrentAbortStore{Store: repo, ready: make(chan struct{}), workers: workers}
	router := &Router{store: wrapper, logger: slog.Default(), hub: newSessionHub()}
	var group sync.WaitGroup
	codes := make(chan int, workers)
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			req := httptest.NewRequest("POST", "/api/v1/tasks/"+drill.ID+"/cleanup-drill", nil)
			req.SetPathValue("id", drill.ID)
			response := httptest.NewRecorder()
			router.cleanupDrillTask(response, req)
			codes <- response.Code
		}()
	}
	group.Wait()
	close(codes)
	created := 0
	for code := range codes {
		if code == 202 {
			created++
		} else if code != 200 {
			t.Fatalf("unexpected response: %d", code)
		}
	}
	if created != 1 {
		t.Fatalf("created %d cleanup tasks", created)
	}
	tasks, err := repo.ListTasks(plan.TargetClusterID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, task := range tasks {
		if task.Type == "protection-cleanup" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("persisted %d cleanup tasks", count)
	}
}

func TestDrillCleanupNamespaceSubsetIsValidated(t *testing.T) {
	repo := newTestStore(t)
	plan := testTenantPlan(t, repo, store.DefaultTenantID)
	router := &Router{store: repo, logger: slog.Default(), hub: newSessionHub()}
	older, err := repo.CreateTask(store.TaskInput{ClusterID: plan.TargetClusterID, ProtectionPlanID: plan.ID, Type: "drill", Status: "succeeded", Payload: map[string]any{"targetNamespaces": map[string]string{"first": "shared", "second": "remaining"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTask(store.TaskInput{ClusterID: plan.TargetClusterID, ProtectionPlanID: plan.ID, Type: "drill", Status: "succeeded", Payload: map[string]any{"targetNamespace": "shared"}}); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		body   string
		status int
	}{
		{`{"namespaces":["unrelated"]}`, 400},
		{`{"namespaces":[]}`, 400},
		{`{}`, 409},
		{`{"namespaces":["remaining"]}`, 202},
		{`{"namespaces":["remaining"]}`, 200},
	} {
		req := httptest.NewRequest("POST", "/api/v1/tasks/"+older.ID+"/cleanup-drill", strings.NewReader(scenario.body))
		req.SetPathValue("id", older.ID)
		response := httptest.NewRecorder()
		router.cleanupDrillTask(response, req)
		if response.Code != scenario.status {
			t.Fatalf("%s: expected %d, got %d: %s", scenario.body, scenario.status, response.Code, response.Body.String())
		}
	}
}
