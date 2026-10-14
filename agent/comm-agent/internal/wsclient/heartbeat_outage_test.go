package wsclient

import (
	"encoding/json"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/inventory"
)

type blockedInventory struct{ release <-chan struct{} }

func (b blockedInventory) Collect() (inventory.Snapshot, error) {
	<-b.release
	return inventory.Snapshot{}, nil
}

func TestHeartbeatContinuesWhileInventoryAPIIsBlocked(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	received := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		count := 0
		for count < 3 {
			var message map[string]any
			if err := conn.ReadJSON(&message); err != nil {
				break
			}
			if message["type"] == "agent.heartbeat" {
				count++
			}
		}
		received <- count
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := NewWithDependencies(config.Config{ClusterID: "test", HeartbeatInterval: 20 * time.Millisecond, StateDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, blockedInventory{release})
	client.agentRuntime = nil
	client.handover = nil
	client.conn = conn
	done := make(chan error, 1)
	go func() { done <- client.RunHeartbeat() }()
	select {
	case count := <-received:
		if count < 3 {
			t.Fatalf("only %d heartbeats while API blocked", count)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat blocked by inventory")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection closure did not stop heartbeat loop")
	}
}

func TestLedgerRestartResumesObservationWithoutRecreatingBackup(t *testing.T) {
	messages := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var message map[string]any
		if conn.ReadJSON(&message) == nil {
			messages <- message
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	ledger, err := newTaskLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	task := protocol.TaskDispatchPayload{TaskID: "recover-test", CommandID: "original-command", Type: "backup", Deadline: time.Now().Add(time.Minute)}
	if err := ledger.recordTask(task.TaskID, task.CommandID, "", task, taskLedgerObject{APIVersion: "velero.io/v1", Kind: "Backup", Namespace: "hypercdr-agent", Name: "existing-backup"}); err != nil {
		t.Fatal(err)
	}
	client := New(config.Config{ClusterID: "test", StateDir: dir}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.statusReader = &sequenceManifestStatusReader{results: []manifestStatusResult{{status: kube.ManifestStatus{Phase: "Completed"}}}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client.agentRuntime = nil
	client.handover = nil
	client.conn = conn
	client.recoverLedgerTasks()
	select {
	case message := <-messages:
		raw, _ := json.Marshal(message["payload"])
		var payload protocol.TaskCompletedPayload
		_ = json.Unmarshal(raw, &payload)
		if message["type"] != protocol.MessageAgentTaskCompleted || payload.TaskID != task.TaskID || payload.CommandID != task.CommandID {
			t.Fatalf("unexpected recovered result: %s", raw)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no immediate recovered terminal observation")
	}
}
