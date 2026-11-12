package wsclient

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type blockingCleanupApplier struct {
	forceCleanupApplier
	started   chan struct{}
	stopped   chan struct{}
	stopError error
}

func (applier *blockingCleanupApplier) CheckProtectionCleanupStorage(ctx context.Context, _, _ string) error {
	close(applier.started)
	<-ctx.Done()
	close(applier.stopped)
	return ctx.Err()
}

func (applier *blockingCleanupApplier) StopBackupDeletionRequests(context.Context, string, []string) error {
	select {
	case <-applier.stopped:
		return applier.stopError
	default:
		return errors.New("cleanup worker has not stopped")
	}
}

func TestAbortStopsBlockedCleanupAndPersistsAcrossRestart(t *testing.T) {
	for _, stopError := range []error{nil, errors.New("cluster API unavailable")} {
		t.Run(fmtAbortScenario(stopError), func(t *testing.T) {
			connections := make(chan *websocket.Conn, 1)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
				if err == nil {
					connections <- connection
				}
			}))
			defer server.Close()
			connection, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			peer := <-connections
			defer peer.Close()
			_ = peer.SetReadDeadline(time.Now().Add(3 * time.Second))
			stateDir := t.TempDir()
			box, err := newEventOutbox(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			ledger, err := newTaskLedger(stateDir)
			if err != nil {
				t.Fatal(err)
			}
			applier := &blockingCleanupApplier{started: make(chan struct{}), stopped: make(chan struct{}), stopError: stopError}
			client := &Client{cfg: config.Config{StateDir: stateDir}, conn: connection, outbox: box, ledger: ledger, applier: applier, logger: slog.Default()}
			target := protocol.TaskDispatchPayload{TaskID: "cleanup", CommandID: "cleanup-command", Type: "protection-cleanup", ProtectionCleanup: &protocol.ProtectionCleanupCommand{PlanID: "plan", StorageRepo: "storage", CleanupObjectStorage: true}}
			workerDone := make(chan struct{})
			go func() { defer close(workerDone); client.executeTask(target) }()
			select {
			case <-applier.started:
			case <-time.After(time.Second):
				t.Fatal("cleanup did not start")
			}
			started := time.Now()
			client.executeTask(protocol.TaskDispatchPayload{TaskID: "abort", CommandID: "abort-command", Type: "task-abort", TaskAbort: &target})
			if time.Since(started) > time.Second {
				t.Fatal("abort did not promptly interrupt preflight")
			}
			select {
			case <-workerDone:
			case <-time.After(time.Second):
				t.Fatal("cleanup worker remained active")
			}
			for {
				var message protocol.Message[map[string]any]
				if err := peer.ReadJSON(&message); err != nil {
					t.Fatal(err)
				}
				if message.Payload["taskId"] == "cleanup" && (message.Type == protocol.MessageAgentTaskCompleted || message.Type == protocol.MessageAgentTaskFailed) {
					t.Fatalf("aborted worker emitted a competing terminal result: %+v", message)
				}
				if message.Payload["taskId"] != "abort" || (message.Type != protocol.MessageAgentTaskCompleted && message.Type != protocol.MessageAgentTaskFailed) {
					continue
				}
				want := protocol.MessageAgentTaskCompleted
				if stopError != nil {
					want = protocol.MessageAgentTaskFailed
				}
				if message.Type != want {
					t.Fatalf("got %s, want %s", message.Type, want)
				}
				break
			}
			reloaded, err := newTaskLedger(stateDir)
			if err != nil || !reloaded.abortRequested(target.TaskID) {
				t.Fatalf("abort was not durable: %v", err)
			}
			if len(reloaded.recoverableRecords()) != 0 {
				t.Fatal("restart would resume an aborted cleanup")
			}
			if err := client.sendTaskCompleted(target, nil, "late success"); !errors.Is(err, context.Canceled) {
				t.Fatal("late success was not suppressed")
			}
			if len(applier.deleted) != 0 {
				t.Fatal("abort deleted business resources")
			}
		})
	}
}

func fmtAbortScenario(err error) string {
	if err != nil {
		return "background stop failure stays explicit"
	}
	return "successful stop"
}

func TestAbortDoesNotCancelOtherTaskContexts(t *testing.T) {
	client := &Client{logger: slog.Default()}
	first := protocol.TaskDispatchPayload{TaskID: "first", Type: "backup"}
	second := protocol.TaskDispatchPayload{TaskID: "second", Type: "drill"}
	client.runAbortableTask(first, func() {
		client.runAbortableTask(second, func() {
			client.taskRunMu.Lock()
			client.taskRuns[first.TaskID].cancel()
			client.taskRunMu.Unlock()
			if !errors.Is(client.taskContext(first).Err(), context.Canceled) {
				t.Fatal("target did not cancel")
			}
			if client.taskContext(second).Err() != nil {
				t.Fatal("other task was canceled")
			}
		})
	})
	if len(client.taskRuns) != 0 {
		t.Fatal("task contexts leaked")
	}
}

var _ kube.ManifestApplier = (*blockingCleanupApplier)(nil)
