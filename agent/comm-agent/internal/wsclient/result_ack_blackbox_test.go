package wsclient

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestResultACKBlackBoxDoesNotConfuseObservationAndResult(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, req, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	peer := <-accepted
	defer peer.Close()
	peer.SetReadDeadline(time.Now().Add(4 * time.Second))
	directory := t.TempDir()
	box, err := newEventOutbox(directory)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := newTaskLedger(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.recordTask("task", "command", "plan", protocol.TaskDispatchPayload{TaskID: "task", CommandID: "command", Type: "backup"}, taskLedgerObject{Kind: "Backup", Name: "backup"}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct{ messageID, eventType string }{{"observation", protocol.MessageAgentVeleroEvent}, {"result", protocol.MessageAgentTaskCompleted}} {
		if err := box.add(protocol.Message[map[string]any]{MessageID: event.messageID, Type: event.eventType, Payload: map[string]any{"taskId": "task", "commandId": "command"}}); err != nil {
			t.Fatal(err)
		}
	}
	client := &Client{cfg: config.Config{ClusterID: "cluster", AgentID: "agent"}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), conn: conn, outbox: box, ledger: ledger, contentReader: &recordingBackupContentReader{}}
	done := make(chan error, 1)
	go func() { done <- client.readMessages() }()
	defer func() {
		conn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("Agent read loop did not stop")
		}
	}()
	acknowledge := func(messageID, eventType, taskID string) {
		t.Helper()
		if err := peer.WriteJSON(protocol.Message[protocol.EventAckPayload]{Type: protocol.MessagePlatformEventAck, Payload: protocol.EventAckPayload{AckMessageID: messageID, AckType: eventType, TaskID: taskID, Persisted: true}}); err != nil {
			t.Fatal(err)
		}
		if err := peer.WriteJSON(protocol.Message[protocol.BackupContentRequestPayload]{Type: protocol.MessagePlatformBackupContentRequest, Payload: protocol.BackupContentRequestPayload{RequestID: "barrier", VeleroBackupName: "backup"}}); err != nil {
			t.Fatal(err)
		}
		var response protocol.Message[protocol.BackupContentReportPayload]
		if err := peer.ReadJSON(&response); err != nil || response.Type != protocol.MessageAgentBackupContentReport {
			t.Fatalf("barrier failed: %v %v", response, err)
		}
	}
	for _, invalid := range []protocol.Message[protocol.EventAckPayload]{
		{Version: "v2", Type: protocol.MessagePlatformEventAck},
		{ClusterID: "other-cluster", Type: protocol.MessagePlatformEventAck},
		{MessageKind: protocol.MessageKindRequest, Type: protocol.MessagePlatformEventAck},
	} {
		invalid.Payload = protocol.EventAckPayload{AckMessageID: "result", AckType: protocol.MessageAgentTaskCompleted, TaskID: "task", Persisted: true}
		if err := peer.WriteJSON(invalid); err != nil {
			t.Fatal(err)
		}
	}
	acknowledge("observation", protocol.MessageAgentVeleroEvent, "")
	if ledger.isTerminalAcked("task") {
		t.Fatal("observation ACK confirmed final result")
	}
	reopened, err := newEventOutbox(directory)
	if err != nil || len(reopened.list()) != 1 {
		t.Fatalf("result not recoverable after restart: %v", err)
	}
	acknowledge("result", protocol.MessageAgentTaskCompleted, "task")
	if !ledger.isTerminalAcked("task") || len(box.list()) != 0 {
		t.Fatal("final result was not acknowledged")
	}
}
