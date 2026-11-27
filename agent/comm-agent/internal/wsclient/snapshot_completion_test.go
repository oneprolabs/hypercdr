package wsclient

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/config"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type snapshotCompletionReaderStub struct {
	kube.ManifestApplier
	result map[string]any
	err    error
}

func (reader snapshotCompletionReaderStub) ReadSnapshotBackupCompletion(context.Context, string, string) (map[string]any, error) {
	return reader.result, reader.err
}
func (reader snapshotCompletionReaderStub) PrepareSnapshotBackup(context.Context, []string) (map[string]string, error) {
	return nil, nil
}

func TestSnapshotAdvertisementRequiresConfiguredDurableStack(t *testing.T) {
	for _, scenario := range []string{"valid", "missing-reader", "no-ledger", "no-outbox", "dry-run", "oadp"} {
		t.Run(scenario, func(t *testing.T) {
			client := &Client{cfg: config.Config{ExecutorMode: "kubernetes"}, ledger: &taskLedger{}, outbox: &eventOutbox{}, applier: snapshotCompletionReaderStub{}}
			switch scenario {
			case "missing-reader":
				client.applier = nil
			case "no-ledger":
				client.ledger = nil
			case "no-outbox":
				client.outbox = nil
			case "dry-run":
				client.cfg.ExecutorMode = "dry-run"
			case "oadp":
				client.cfg.BackupBackend = "oadp"
			}
			if slices.Contains(client.agentCapabilities(), "snapshot-backup-evidence-v1") != (scenario == "valid") {
				t.Fatal("snapshot capability advertised without a working durable stack")
			}
		})
	}
}

func TestSnapshotCompletionUsesVerifiedIdentityAndKeepsMeasurements(t *testing.T) {
	inventory := protocol.SnapshotBackupInventory{Namespaces: []string{"app"}, Volumes: []protocol.ConsistencyVolume{}}
	verified := map[string]any{"kind": "Backup", "name": "backup", "namespace": "agent", "uid": "backup-uid", "phase": "Completed", "requestedConsistency": "snapshot-crash-consistent", "achievedConsistency": "snapshot-crash-consistent", "snapshotEvidence": protocol.SnapshotBackupEvidence{Inventory: inventory},
		"manifest": map[string]any{"metadata": map[string]any{"name": "backup", "namespace": "agent", "uid": "backup-uid", "labels": map[string]string{"hypercdr.io/task-id": "task", "hypercdr.io/command-id": "command"}, "annotations": map[string]string{protocol.SnapshotInventoryAnnotation: `{"namespaces":["app"],"volumes":[]}`}}, "spec": map[string]any{"includedNamespaces": []string{"app"}, "snapshotMoveData": true, "snapshotVolumes": true}, "status": map[string]any{"phase": "Completed"}}}
	task := protocol.TaskDispatchPayload{TaskID: "task", CommandID: "command", Type: "backup", Backup: &protocol.BackupCommand{SourceNamespace: "app", VeleroBackupName: "backup", DataProtectionMode: "snapshot-crash-consistent"}}
	base := map[string]any{"name": "backup", "namespace": "agent", "sizeBytes": 123}
	client := &Client{ledger: &taskLedger{}, applier: snapshotCompletionReaderStub{result: verified}}
	result, err := client.verifiedSnapshotCompletion(task, base)
	if err != nil || result["sizeBytes"] != 123 || result["uid"] != "backup-uid" {
		t.Fatalf("verification/measurement merge failed: %+v %v", result, err)
	}
	if base["snapshotEvidence"] != nil {
		t.Fatal("completion mutated caller payload")
	}
	client.applier = snapshotCompletionReaderStub{err: fmt.Errorf("DataUpload not completed")}
	if _, err := client.verifiedSnapshotCompletion(task, base); err == nil {
		t.Fatal("unproven snapshot reader result accepted")
	}
	client.applier = nil
	if _, err := client.verifiedSnapshotCompletion(task, base); err == nil {
		t.Fatal("native phase was accepted without verification reader")
	}
}
