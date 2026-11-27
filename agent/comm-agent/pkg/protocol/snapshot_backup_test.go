package protocol

import (
	"encoding/json"
	"testing"
)

func snapshotBackupFixture(t *testing.T) (TaskDispatchPayload, map[string]any) {
	t.Helper()
	task := TaskDispatchPayload{TaskID: "task", CommandID: "command", Type: "backup", Backup: &BackupCommand{SourceNamespace: "app", VeleroBackupName: "backup", DataProtectionMode: "snapshot-crash-consistent"}}
	inventory := SnapshotBackupInventory{Namespaces: []string{"app"}, Volumes: []ConsistencyVolume{{Namespace: "app", Name: "data", UID: "pvc", SnapshotClass: "class"}}}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"kind": "Backup", "name": "backup", "namespace": "agent", "uid": "backup-uid", "phase": "Completed", "requestedConsistency": "snapshot-crash-consistent", "achievedConsistency": "snapshot-crash-consistent",
		"snapshotEvidence": SnapshotBackupEvidence{Inventory: inventory, Snapshots: []ConsistencySnapshot{{Source: "kopia", VolumeUID: "pvc", DataUploadUID: "upload", Handle: "durable", Secured: true}}},
		"manifest": map[string]any{"metadata": map[string]any{"name": "backup", "namespace": "agent", "uid": "backup-uid", "labels": map[string]string{"hypercdr.io/task-id": "task", "hypercdr.io/command-id": "command"}, "annotations": map[string]string{SnapshotInventoryAnnotation: string(encoded)}},
			"spec": map[string]any{"includedNamespaces": []string{"app"}, "snapshotMoveData": true, "snapshotVolumes": true, "defaultVolumesToFsBackup": false}, "status": map[string]any{"phase": "Completed"}}}
	return task, result
}

func TestSnapshotBackupCompletionRequiresBoundDurableEvidence(t *testing.T) {
	for _, scenario := range []string{"valid", "missing-evidence", "application-claim", "application-record", "wrong-task", "wrong-command", "wrong-backup", "wrong-uid", "wrong-source", "fsb", "live-source", "missing-inventory", "missing-volume", "not-durable", "duplicate-upload"} {
		t.Run(scenario, func(t *testing.T) {
			task, result := snapshotBackupFixture(t)
			evidence := result["snapshotEvidence"].(SnapshotBackupEvidence)
			metadata := result["manifest"].(map[string]any)["metadata"].(map[string]any)
			spec := result["manifest"].(map[string]any)["spec"].(map[string]any)
			switch scenario {
			case "missing-evidence":
				delete(result, "snapshotEvidence")
			case "application-claim":
				result["achievedConsistency"] = "application-consistent"
			case "application-record":
				result["consistency"] = map[string]any{}
			case "wrong-task":
				task.TaskID = "other"
			case "wrong-command":
				task.CommandID = "other"
			case "wrong-backup":
				task.Backup.VeleroBackupName = "other"
			case "wrong-uid":
				result["uid"] = "other"
			case "wrong-source":
				task.Backup.SourceNamespace = "other"
			case "fsb":
				spec["defaultVolumesToFsBackup"] = true
			case "live-source":
				spec["snapshotMoveData"] = false
			case "missing-inventory":
				metadata["annotations"] = map[string]string{}
			case "missing-volume":
				evidence.Snapshots = nil
				result["snapshotEvidence"] = evidence
			case "not-durable":
				evidence.Snapshots[0].Source = "csi"
				result["snapshotEvidence"] = evidence
			case "duplicate-upload":
				evidence.Snapshots = append(evidence.Snapshots, evidence.Snapshots[0])
				result["snapshotEvidence"] = evidence
			}
			if err := ValidateSnapshotBackupResult(task, result); (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected snapshot admission: %v", err)
			}
		})
	}
}

func TestSnapshotInventoryRejectsScopeAndIdentityAmbiguity(t *testing.T) {
	for _, inventory := range []SnapshotBackupInventory{
		{Namespaces: []string{"*"}},
		{Namespaces: []string{"app", "app"}},
		{Namespaces: []string{"app"}, Volumes: []ConsistencyVolume{{Namespace: "app", Name: "data", UID: "pvc"}}},
		{Namespaces: []string{"app"}, Volumes: []ConsistencyVolume{{Namespace: "other", Name: "data", UID: "pvc", SnapshotClass: "class"}}},
	} {
		if err := inventory.Validate([]string{"app"}); err == nil {
			t.Fatalf("ambiguous inventory accepted: %+v", inventory)
		}
	}
}
