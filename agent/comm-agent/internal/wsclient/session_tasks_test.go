package wsclient

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestDisconnectedSessionStopsTaskObserversBeforeReplacement(t *testing.T) {
	ledger, err := newTaskLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{ledger: ledger}
	task := protocol.TaskDispatchPayload{TaskID: "drill", Type: "drill"}
	if err := ledger.recordTask(task.TaskID, "command", "plan", task, taskLedgerObject{Kind: "Restore", Name: "restore"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		client.runAbortableTask(task, func() {
			close(started)
			<-client.taskContext(task).Done()
		})
		close(finished)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("task did not start")
	}
	stopped := make(chan struct{})
	go func() { client.stopTaskRuns(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("disconnected task observer did not stop")
	}
	<-finished
	if ledger.abortRequested(task.TaskID) || ledger.isTerminalAcked(task.TaskID) || len(ledger.recoverableRecords()) != 1 {
		t.Fatal("disconnect must preserve the task for ledger recovery without marking it aborted or complete")
	}
	client.runAbortableTask(task, func() { t.Error("closed session started another task observer") })
	client.taskRunMu.Lock()
	defer client.taskRunMu.Unlock()
	if len(client.taskRuns) != 0 {
		t.Fatal("old task observers remain registered")
	}
}

func TestTerminalResultSurvivesDisconnectedSession(t *testing.T) {
	accepted := make(chan *websocket.Conn, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err == nil {
			accepted <- connection
		}
	}))
	defer server.Close()
	directory := t.TempDir()
	box, err := newEventOutbox(directory)
	if err != nil {
		t.Fatal(err)
	}
	connection, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	peer := <-accepted
	defer peer.Close()
	connection.Close()
	client := &Client{conn: connection, outbox: box}
	task := protocol.TaskDispatchPayload{TaskID: "drill", CommandID: "command", Type: "drill"}
	if err := client.sendTaskCompleted(task, nil, "ready"); err == nil {
		t.Fatal("closed connection unexpectedly accepted result")
	}
	reopened, err := newEventOutbox(directory)
	if err != nil || len(reopened.list()) != 1 {
		t.Fatalf("terminal result was not persisted: %v", err)
	}
	replacement, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	reconnectedPeer := <-accepted
	defer reconnectedPeer.Close()
	reconnectedPeer.SetReadDeadline(time.Now().Add(time.Second))
	restarted := &Client{conn: replacement, outbox: reopened, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	restarted.resendPendingEvents()
	var result protocol.Message[protocol.TaskCompletedPayload]
	if err := reconnectedPeer.ReadJSON(&result); err != nil {
		t.Fatal(err)
	}
	if result.Type != protocol.MessageAgentTaskCompleted || result.Payload.TaskID != task.TaskID || result.Payload.Status != "succeeded" {
		t.Fatalf("unexpected replayed result: %+v", result)
	}
	payload := protocol.EventAckPayload{AckMessageID: result.MessageID, AckType: result.Type, TaskID: task.TaskID, Persisted: true}
	if _, terminal, err := reopened.acknowledge(payload); err != nil || !terminal || len(reopened.list()) != 0 {
		t.Fatalf("replayed result was not acknowledged: terminal=%v err=%v", terminal, err)
	}
}
