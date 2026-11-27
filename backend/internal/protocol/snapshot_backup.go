package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
)

const SnapshotInventoryAnnotation = "hypercdr.io/snapshot-volume-inventory-v1"

type SnapshotBackupInventory struct {
	Namespaces []string            `json:"namespaces"`
	Volumes    []ConsistencyVolume `json:"volumes"`
}

type SnapshotBackupEvidence struct {
	Inventory SnapshotBackupInventory `json:"inventory"`
	Snapshots []ConsistencySnapshot   `json:"snapshots"`
}

func (inventory SnapshotBackupInventory) Validate(namespaces []string) error {
	if inventory.Volumes == nil {
		return fmt.Errorf("snapshot inventory must explicitly record the complete PVC list, including an empty list")
	}
	selected := map[string]bool{}
	for _, namespace := range namespaces {
		if namespace == "" || namespace == "*" || selected[namespace] {
			return fmt.Errorf("snapshot inventory requires explicit unique namespaces")
		}
		selected[namespace] = true
	}
	if len(selected) == 0 || len(inventory.Namespaces) != len(selected) {
		return fmt.Errorf("snapshot inventory namespace scope differs from task")
	}
	seenNamespaces := map[string]bool{}
	for _, namespace := range inventory.Namespaces {
		if !selected[namespace] || seenNamespaces[namespace] {
			return fmt.Errorf("snapshot inventory namespace scope differs from task")
		}
		seenNamespaces[namespace] = true
	}
	identities, names := map[string]bool{}, map[string]bool{}
	for _, volume := range inventory.Volumes {
		key := volume.Namespace + "/" + volume.Name
		if !selected[volume.Namespace] || volume.Name == "" || volume.UID == "" || volume.SnapshotClass == "" || identities[volume.UID] || names[key] {
			return fmt.Errorf("snapshot inventory contains invalid, duplicate or out-of-scope volume")
		}
		identities[volume.UID], names[key] = true, true
	}
	return nil
}

func ValidateSnapshotBackupResult(task TaskDispatchPayload, result map[string]any) error {
	if task.Type != "backup" || task.TaskID == "" || task.CommandID == "" || task.Backup == nil || task.Backup.DataProtectionMode != "snapshot-crash-consistent" || task.Backup.ConsistencyGroup != nil {
		return fmt.Errorf("snapshot completion requires an identified ordinary CSI backup task")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var completion struct {
		Kind        string                  `json:"kind"`
		Name        string                  `json:"name"`
		Namespace   string                  `json:"namespace"`
		UID         string                  `json:"uid"`
		Phase       string                  `json:"phase"`
		Requested   string                  `json:"requestedConsistency"`
		Achieved    string                  `json:"achievedConsistency"`
		Consistency json.RawMessage         `json:"consistency"`
		Evidence    *SnapshotBackupEvidence `json:"snapshotEvidence"`
		Manifest    struct {
			Metadata struct {
				Name        string            `json:"name"`
				Namespace   string            `json:"namespace"`
				UID         string            `json:"uid"`
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				Namespaces       []string `json:"includedNamespaces"`
				SnapshotMoveData bool     `json:"snapshotMoveData"`
				SnapshotVolumes  bool     `json:"snapshotVolumes"`
				FSB              bool     `json:"defaultVolumesToFsBackup"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(encoded, &completion); err != nil {
		return err
	}
	if completion.Kind != "Backup" || completion.Name == "" || completion.Namespace == "" || completion.UID == "" || completion.Phase != "Completed" || completion.Requested != "snapshot-crash-consistent" || completion.Achieved != "snapshot-crash-consistent" || completion.Evidence == nil || (len(completion.Consistency) != 0 && string(completion.Consistency) != "null") {
		return fmt.Errorf("snapshot completion lacks independent crash-consistency evidence or claims application consistency")
	}
	metadata, spec := completion.Manifest.Metadata, completion.Manifest.Spec
	if expected := task.Backup.SourceClusterID; expected != "" && metadata.Labels["hypercdr.io/source-cluster-id"] != expected {
		return fmt.Errorf("snapshot completion source cluster differs from dispatched task")
	}
	if metadata.Name != completion.Name || metadata.Namespace != completion.Namespace || metadata.UID != completion.UID || metadata.Labels["hypercdr.io/task-id"] != task.TaskID || metadata.Labels["hypercdr.io/command-id"] != task.CommandID || !spec.SnapshotMoveData || !spec.SnapshotVolumes || spec.FSB || completion.Manifest.Status.Phase != "Completed" {
		return fmt.Errorf("snapshot completion backup identity, configuration or phase mismatch")
	}
	if expected := task.Backup.VeleroBackupName; expected != "" && expected != completion.Name {
		return fmt.Errorf("snapshot completion backup name differs from dispatched task")
	}
	namespaces := task.Backup.SourceNamespaces
	if len(namespaces) == 0 && task.Backup.SourceNamespace != "" {
		namespaces = []string{task.Backup.SourceNamespace}
	}
	if err := completion.Evidence.Inventory.Validate(namespaces); err != nil {
		return err
	}
	if err := completion.Evidence.Inventory.Validate(spec.Namespaces); err != nil {
		return err
	}
	var captured SnapshotBackupInventory
	if err := json.Unmarshal([]byte(metadata.Annotations[SnapshotInventoryAnnotation]), &captured); err != nil || !reflect.DeepEqual(captured, completion.Evidence.Inventory) {
		return fmt.Errorf("snapshot completion inventory differs from captured backup annotation")
	}
	if len(completion.Evidence.Inventory.Volumes) == 0 {
		if len(completion.Evidence.Snapshots) != 0 {
			return fmt.Errorf("empty snapshot inventory has unexpected data snapshots")
		}
		return nil
	}
	record := ConsistencyRecord{Snapshots: completion.Evidence.Snapshots}
	for _, volume := range completion.Evidence.Inventory.Volumes {
		record.Volumes = append(record.Volumes, ConsistencyVolumeIdentity{Namespace: volume.Namespace, Name: volume.Name, UID: volume.UID})
	}
	return VerifyConsistencyDurableSnapshots(record)
}
