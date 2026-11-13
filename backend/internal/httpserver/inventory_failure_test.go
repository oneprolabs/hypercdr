package httpserver

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

type failingInventoryStore struct {
	store.Store
	applied chan string
}

func (repo *failingInventoryStore) ApplyInventory(input store.InventoryInput) (store.Cluster, bool, error) {
	if input.Hash == "fail" {
		return store.Cluster{}, false, errors.New("injected inventory transaction failure")
	}
	repo.applied <- input.Hash
	return store.Cluster{}, false, errors.New("second injected inventory transaction failure")
}

func TestInventoryFailurePreservesAgentConnection(t *testing.T) {
	server, client := clusterContractSocket(t)
	repo := &failingInventoryStore{applied: make(chan string, 1)}
	router := &Router{store: repo, logger: slog.Default(), inventory: map[string]inventoryRequestStatus{}}
	router.setInventoryRequestStatus(inventoryRequestStatus{ClusterID: "cluster", RequestID: "request", Status: "pending"})
	done := make(chan struct{})
	go func() { defer close(done); router.readAgentMessages(server, "cluster") }()
	defer func() { client.Close(); <-done }()
	for _, hash := range []string{"fail", "next"} {
		requestID := ""
		if hash == "fail" {
			requestID = "request"
		}
		err := client.WriteJSON(protocol.Message[protocol.InventoryReportPayload]{
			Version: protocol.Version, MessageID: hash, MessageKind: protocol.MessageKindEvent,
			Type: protocol.MessageAgentInventoryReport, ClusterID: "cluster", AgentID: "agent", Timestamp: time.Now().UTC(),
			Payload: protocol.InventoryReportPayload{RequestID: requestID, InventoryHash: hash, Full: true},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	select {
	case hash := <-repo.applied:
		if hash != "next" {
			t.Fatalf("unexpected hash %q", hash)
		}
		status, ok := router.getInventoryRequestStatus("cluster", "request")
		if !ok || status.Status != "failed" || status.ErrorCode != "INVENTORY_APPLY_FAILED" {
			t.Fatalf("request did not fail promptly: %+v", status)
		}
	case <-done:
		t.Fatal("inventory error terminated the connection")
	case <-time.After(5 * time.Second):
		t.Fatal("subsequent inventory was not processed")
	}
}
