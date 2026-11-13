package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

type concurrentAbortStore struct {
	store.Store
	readers atomic.Int32
	ready   chan struct{}
	workers int32
}

func (repo *concurrentAbortStore) ListTasks(clusterID string) ([]store.Task, error) {
	items, err := repo.Store.ListTasks(clusterID)
	reader := repo.readers.Add(1)
	if reader == repo.workers {
		close(repo.ready)
	}
	if reader <= repo.workers {
		<-repo.ready
	}
	return items, err
}

func TestConcurrentForceAbortDispatchesOnce(t *testing.T) {
	for _, taskType := range []string{"backup", "drill", "restore", "protection-cleanup"} {
		t.Run(taskType, func(t *testing.T) {
			repo := newTestStore(t)
			plan := testTenantPlan(t, repo, store.DefaultTenantID)
			target, err := repo.CreateTask(store.TaskInput{ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Type: taskType, Status: "running", CommandID: store.NewPublicID(), Payload: map[string]any{"planId": plan.ID}})
			if err != nil {
				t.Fatal(err)
			}
			const workers = 4
			wrapped := &concurrentAbortStore{Store: repo, ready: make(chan struct{}), workers: workers}
			server, peer := clusterContractSocket(t)
			router := &Router{store: wrapped, logger: slog.Default(), hub: newSessionHub()}
			router.hub.set(plan.SourceClusterID, server)
			router.hub.setTaskAbortSupport(plan.SourceClusterID, server, true)
			var group sync.WaitGroup
			results := make(chan *httptest.ResponseRecorder, workers)
			for index := 0; index < workers; index++ {
				group.Add(1)
				go func() {
					defer group.Done()
					req := httptest.NewRequest("POST", "/api/v1/tasks/"+target.ID+"/abort", nil)
					req.SetPathValue("id", target.ID)
					response := httptest.NewRecorder()
					router.abortTask(response, req)
					results <- response
				}()
			}
			group.Wait()
			close(results)
			accepted, reused := 0, 0
			for result := range results {
				if result.Code == 202 {
					accepted++
				} else if result.Code == 200 {
					var body map[string]any
					if err := json.Unmarshal(result.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					if body["reused"] != true {
						t.Fatal("missing reuse indicator")
					}
					reused++
				} else {
					t.Fatalf("abort: %d %s", result.Code, result.Body.String())
				}
			}
			if accepted != 1 || reused != workers-1 {
				t.Fatalf("accepted=%d reused=%d", accepted, reused)
			}
			if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			var dispatch protocol.Message[protocol.TaskDispatchPayload]
			if err := peer.ReadJSON(&dispatch); err != nil {
				t.Fatal(err)
			}
			if dispatch.Payload.TaskAbort == nil || dispatch.Payload.TaskAbort.TaskID != target.ID {
				t.Fatal("wrong target")
			}
			if err := peer.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if err := peer.ReadJSON(&dispatch); err == nil {
				t.Fatal("duplicate dispatch")
			}
			tasks, err := repo.ListTasks(plan.SourceClusterID)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, task := range tasks {
				if task.Type == "task-abort" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("abort child count=%d", count)
			}
		})
	}
}
