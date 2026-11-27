package store

import (
	"encoding/json"
	"errors"
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestSnapshotBackupPublicationRequiresDurableEvidenceAndPreservesIt(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, DataProtection: DataProtectionSettings{Mode: "snapshot-crash-consistent"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, Type: "backup", CommandID: NewPublicID(), Payload: map[string]any{"sourceNamespace": app.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	input := completeResultInput(plan, task, "snapshot-backup")
	if _, err := repo.PublishBackupResult(input); !errors.Is(err, ErrBackupResultConflict) {
		t.Fatalf("native completion bypassed evidence: %v", err)
	}
	inventory := protocol.SnapshotBackupInventory{Namespaces: []string{app.Namespace}, Volumes: []protocol.ConsistencyVolume{{Namespace: app.Namespace, Name: "data", UID: "pvc", SnapshotClass: "class"}}}
	encoded, _ := json.Marshal(inventory)
	evidence := protocol.SnapshotBackupEvidence{Inventory: inventory, Snapshots: []protocol.ConsistencySnapshot{{Source: "kopia", VolumeUID: "pvc", DataUploadUID: "upload", Handle: "durable", Secured: true}}}
	result := map[string]any{"kind": "Backup", "name": input.VeleroBackupName, "namespace": "agent", "uid": "backup-uid", "phase": "Completed", "requestedConsistency": "snapshot-crash-consistent", "achievedConsistency": "snapshot-crash-consistent", "snapshotEvidence": evidence,
		"manifest": map[string]any{"metadata": map[string]any{"name": input.VeleroBackupName, "namespace": "agent", "uid": "backup-uid", "labels": map[string]string{"hypercdr.io/task-id": task.ID, "hypercdr.io/command-id": task.CommandID}, "annotations": map[string]string{protocol.SnapshotInventoryAnnotation: string(encoded)}}, "spec": map[string]any{"includedNamespaces": []string{app.Namespace}, "snapshotMoveData": true, "snapshotVolumes": true, "defaultVolumesToFsBackup": false}, "status": map[string]any{"phase": "Completed"}}}
	input.Metadata = map[string]any{"velero": result}
	wrong := input
	wrong.VeleroBackupName = "different-backup"
	if _, err := repo.PublishBackupResult(wrong); !errors.Is(err, ErrBackupResultConflict) {
		t.Fatalf("mismatched backup was published: %v", err)
	}
	point, err := repo.PublishBackupResult(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PublishBackupResult(input); err != nil {
		t.Fatalf("identical replay failed: %v", err)
	}
	for _, query := range []string{
		`update restore_points set metadata=metadata #- '{velero,snapshotEvidence}' where id=$1`,
		`update restore_points set metadata=jsonb_set(metadata,'{velero,achievedConsistency}','"application-consistent"') where id=$1`,
	} {
		if _, err := repo.db.Exec(query, point.ID); err == nil {
			t.Fatal("accepted snapshot evidence was mutable")
		}
	}
	evidence.Snapshots[0].Handle = "different"
	result["snapshotEvidence"] = evidence
	if _, err := repo.PublishBackupResult(input); !errors.Is(err, ErrBackupResultConflict) {
		t.Fatalf("conflicting evidence replay accepted: %v", err)
	}
}
