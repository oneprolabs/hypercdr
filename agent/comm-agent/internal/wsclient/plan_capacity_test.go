package wsclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type capacityTestReader struct {
	backupSizeStatsReader
	calls int
}

func (reader *capacityTestReader) CollectPlanCapacity(_ context.Context, _ string, plans []kube.PlanCapacityTarget) []kube.PlanCapacityResult {
	reader.calls++
	return []kube.PlanCapacityResult{{PlanID: plans[0].PlanID, CollectedAt: time.Now().UTC(), Stats: kube.PlanObjectStorageStats{MetadataBytes: 10, KopiaBytes: 100, TotalBytes: 110}}, {PlanID: plans[1].PlanID, CollectedAt: time.Now().UTC(), Error: errors.New("unavailable")}}
}

func TestCapacityResultReplayDoesNotRescan(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err == nil {
			accepted <- connection
		}
	}))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	peer := <-accepted
	defer peer.Close()
	_ = peer.SetReadDeadline(time.Now().Add(5 * time.Second))
	state := t.TempDir()
	box, err := newEventOutbox(state)
	if err != nil {
		t.Fatal(err)
	}
	reader := &capacityTestReader{}
	task := protocol.TaskDispatchPayload{TaskID: "task", CommandID: "command", Type: "plan-capacity", PlanCapacity: &protocol.PlanCapacityCommand{Namespace: "agent", Plans: []protocol.PlanCapacityTarget{{PlanID: "one", StorageLocation: "store", Namespaces: []string{"one"}}, {PlanID: "two", StorageLocation: "store", Namespaces: []string{"two"}}}}}
	client := &Client{cfg: config.Config{StateDir: state}, conn: connection, outbox: box, statsReader: reader}
	client.executePlanCapacityTask(task)
	var acknowledgment protocol.Message[protocol.TaskAcceptedPayload]
	if err := peer.ReadJSON(&acknowledgment); err != nil {
		t.Fatal(err)
	}
	var result protocol.Message[protocol.TaskCompletedPayload]
	if err := peer.ReadJSON(&result); err != nil {
		t.Fatal(err)
	}
	if result.Payload.PlanCapacity == nil || len(result.Payload.PlanCapacity.Plans) != 2 || result.Payload.PlanCapacity.Plans[1].Status != "failed" {
		t.Fatalf("invalid result %+v", result)
	}
	restarted := &Client{cfg: client.cfg, conn: connection, outbox: box, statsReader: reader}
	restarted.executePlanCapacityTask(task)
	if err := peer.ReadJSON(&result); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 {
		t.Fatal("replayed task rescanned object storage")
	}
}

type noPlanScanReader struct{ backupSizeStatsReader }

func (noPlanScanReader) GetPlanObjectStorageStats(context.Context, string, string, string, []string) (kube.PlanObjectStorageStats, error) {
	panic("backup completion must not scan plan storage")
}

func TestBackupCompletionDoesNotScanPlanCapacity(t *testing.T) {
	client := &Client{statsReader: noPlanScanReader{}}
	payload := map[string]any{"planId": "plan", "storageLocation": "store", "sourceNamespace": "demo"}
	client.attachBackupSizeStats(context.Background(), kube.AppliedObject{Kind: "Backup", Namespace: "agent", Name: "backup"}, payload)
	if _, ok := payload["planStorageSize"]; ok {
		t.Fatal("backup completion reported plan capacity")
	}
	if _, ok := payload["restorePointSize"]; !ok {
		t.Fatal("restore point size missing")
	}
}
