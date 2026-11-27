package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestCSIProtocolPersistsEvidenceBeforeAck(t *testing.T) {
	repo := newTestStore(t)
	handler := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	server := httptest.NewServer(handler)
	defer server.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := handler.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "consistency-protocol", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:]+"/ws/agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := conn.WriteJSON(protocol.Message[protocol.RegisterPayload]{Version: protocol.Version, Type: protocol.MessageAgentRegister, AgentID: "consistency-agent", Payload: protocol.RegisterPayload{InstallToken: token.Token, Cluster: protocol.ClusterSummary{Name: "isolated-consistency-protocol"}, Agent: protocol.AgentSummary{Version: "test"}, Velero: protocol.VeleroSummary{Status: "ready"}}}); err != nil {
		t.Fatal(err)
	}
	var registered protocol.Message[protocol.RegisterAcceptedPayload]
	if err := conn.ReadJSON(&registered); err != nil || registered.Type != protocol.MessagePlatformRegisterAccepted {
		t.Fatalf("registration failed: %v", err)
	}
	clusterID := registered.Payload.ClusterID
	app := seedSchedulerApplication(t, repo, clusterID, "isolated-consistency")
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active", DataProtection: store.DataProtectionSettings{Mode: "snapshot-crash-consistent"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plan.ID, Type: "backup", Status: "running", CommandID: store.NewPublicID(), Payload: map[string]any{"sourceNamespace": app.Namespace, "veleroBackupName": "consistent-backup"}})
	if err != nil {
		t.Fatal(err)
	}
	inventory := protocol.SnapshotBackupInventory{Namespaces: []string{app.Namespace}, Volumes: []protocol.ConsistencyVolume{{Namespace: app.Namespace, Name: "ledger", UID: "ledger-uid", SnapshotClass: "class"}, {Namespace: app.Namespace, Name: "commits", UID: "commits-uid", SnapshotClass: "class"}}}
	encodedInventory, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	record := protocol.SnapshotBackupEvidence{Inventory: inventory, Snapshots: []protocol.ConsistencySnapshot{{Source: "kopia", DataUploadUID: "upload-ledger", VolumeUID: "ledger-uid", Handle: "durable-ledger", Secured: true}, {Source: "kopia", DataUploadUID: "upload-commits", VolumeUID: "commits-uid", Handle: "durable-commits", Secured: true}}}
	send := func(messageID string, candidate protocol.SnapshotBackupEvidence, accepted bool) {
		t.Helper()
		payload := protocol.TaskCompletedPayload{AckRequired: true, TaskID: task.ID, CommandID: task.CommandID, Operation: "backup", Status: "succeeded", Progress: 100,
			Velero: map[string]any{"kind": "Backup", "name": "consistent-backup", "namespace": "agent", "uid": "backup-uid", "phase": "Completed", "requestedConsistency": "snapshot-crash-consistent", "achievedConsistency": "snapshot-crash-consistent", "snapshotEvidence": candidate,
				"manifest": map[string]any{"metadata": map[string]any{"name": "consistent-backup", "namespace": "agent", "uid": "backup-uid", "labels": map[string]string{"hypercdr.io/task-id": task.ID, "hypercdr.io/command-id": task.CommandID}, "annotations": map[string]string{protocol.SnapshotInventoryAnnotation: string(encodedInventory)}}, "spec": map[string]any{"includedNamespaces": []string{app.Namespace}, "snapshotMoveData": true, "snapshotVolumes": true, "defaultVolumesToFsBackup": false}, "status": map[string]any{"phase": "Completed"}}},
			SizeMetricsV2: &protocol.SizeMetricsV2{SchemaVersion: 2, Operation: "backup", Logical: protocol.SizeValueV2{Known: true, TotalBytes: 1024}, NewData: protocol.SizeValueV2{Known: true, TotalBytes: 0}}}
		if err := conn.WriteJSON(protocol.Message[protocol.TaskCompletedPayload]{Version: protocol.Version, Type: protocol.MessageAgentTaskCompleted, MessageKind: protocol.MessageKindEvent, MessageID: messageID, ClusterID: clusterID, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		for {
			var receipt protocol.Message[struct {
				AckMessageID string `json:"ackMessageId"`
				Persisted    bool   `json:"persisted"`
				Retryable    bool   `json:"retryable"`
			}]
			if err := conn.ReadJSON(&receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Payload.AckMessageID != messageID {
				continue
			}
			if accepted {
				if receipt.Type != protocol.MessagePlatformEventAck || !receipt.Payload.Persisted {
					t.Fatalf("valid evidence not durably acknowledged: %+v", receipt)
				}
			} else if receipt.Type != protocol.MessagePlatformEventError || receipt.Payload.Retryable {
				t.Fatalf("invalid evidence not permanently rejected: %+v", receipt)
			}
			break
		}
	}
	unsafe := record
	unsafe.Snapshots = nil
	send("unsafe-completion", unsafe, false)
	points, err := repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID})
	if err != nil || len(points) != 0 {
		t.Fatalf("unsafe result published a point: %v", err)
	}
	state, _, err := repo.GetTask(task.ID)
	if err != nil || state.Status != "running" {
		t.Fatalf("unsafe result finalized task: %v", err)
	}
	send("verified-completion", record, true)
	send("verified-completion", record, true)
	send("verified-completion-new-envelope", record, true)
	points, err = repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID})
	if err != nil || len(points) != 1 {
		t.Fatalf("publication cardinality: %d, %v", len(points), err)
	}
	state, _, err = repo.GetTask(task.ID)
	if err != nil || state.Status != "succeeded" || state.RestorePointID != points[0].ID {
		t.Fatalf("acknowledged result not persisted: %v", err)
	}
	encodedEvidence, err := json.Marshal(points[0].Metadata)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Velero struct {
			Evidence protocol.SnapshotBackupEvidence `json:"snapshotEvidence"`
		} `json:"velero"`
	}
	if err := json.Unmarshal(encodedEvidence, &persisted); err != nil || len(persisted.Velero.Evidence.Snapshots) != 2 {
		t.Fatalf("persisted CSI evidence missing: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	conn, _, err = websocket.DefaultDialer.Dial("ws"+server.URL[4:]+"/ws/agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := conn.WriteJSON(protocol.Message[protocol.RegisterPayload]{Version: protocol.Version, Type: protocol.MessageAgentRegister, ClusterID: clusterID, AgentID: "consistency-agent", Payload: protocol.RegisterPayload{AgentCredential: registered.Payload.AgentCredential, Cluster: protocol.ClusterSummary{Name: "isolated-consistency-protocol"}, Agent: protocol.AgentSummary{Version: "test"}, Velero: protocol.VeleroSummary{Status: "ready"}}}); err != nil {
		t.Fatal(err)
	}
	var reconnected protocol.Message[protocol.RegisterAcceptedPayload]
	if err := conn.ReadJSON(&reconnected); err != nil || reconnected.Type != protocol.MessagePlatformRegisterAccepted || reconnected.Payload.ClusterID != clusterID {
		t.Fatalf("credential reconnect failed: %v", err)
	}
	send("verified-completion", record, true)
	points, err = repo.ListRestorePoints(store.RestorePointFilter{ProtectionPlanID: plan.ID})
	if err != nil || len(points) != 1 || points[0].ID != state.RestorePointID {
		t.Fatalf("reconnect replay changed accepted publication: %v", err)
	}
	unsafe = record
	unsafe.Snapshots = append([]protocol.ConsistencySnapshot(nil), record.Snapshots...)
	unsafe.Snapshots[0].Handle = "conflicting-handle"
	send("conflicting-completion", unsafe, false)
}
