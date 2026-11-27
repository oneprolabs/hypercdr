package store

import (
	"encoding/json"
	"fmt"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func BuildConsistencyRestoreEvidence(point RestorePoint, task Task) (*protocol.ConsistencyRestoreEvidence, error) {
	if point.ID == "" || point.TenantID == "" || point.Status != "available" || task.TenantID != point.TenantID || task.ID != point.BackupTaskID || task.RestorePointID != point.ID || task.ClusterID != point.SourceClusterID || task.ProtectionPlanID != point.ProtectionPlanID || task.Type != "backup" || task.Status != "succeeded" || task.CommandID == "" || task.Payload["backupResultStatus"] != "accepted" {
		return nil, fmt.Errorf("%w: restore evidence requires the published source task", ErrBackupResultConflict)
	}
	encoded, err := json.Marshal(task.Payload["backupResult"])
	if err != nil {
		return nil, err
	}
	var accepted RestorePointInput
	if err := json.Unmarshal(encoded, &accepted); err != nil {
		return nil, err
	}
	if accepted.BackupTaskID != task.ID || accepted.SourceClusterID != point.SourceClusterID || accepted.ProtectionPlanID != point.ProtectionPlanID || accepted.VeleroBackupName != point.VeleroBackupName || !backupConsistencyEqual(accepted.Metadata, point.Metadata) {
		return nil, fmt.Errorf("%w: restore point differs from accepted backup", ErrBackupResultConflict)
	}
	result, _ := accepted.Metadata["velero"].(map[string]any)
	if err := ValidateTaskConsistencyCompletion(task, result); err != nil {
		return nil, err
	}
	if task.Payload["dataProtectionMode"] != "application-consistent" {
		return nil, nil
	}
	version, err := json.Marshal(task.Payload["dataProtectionSnapshotVersion"])
	if err != nil || string(version) != "1" {
		return nil, fmt.Errorf("%w: source task has no immutable consistency snapshot", ErrBackupResultConflict)
	}
	encoded, err = json.Marshal(task.Payload)
	if err != nil {
		return nil, err
	}
	var backup protocol.BackupCommand
	if err := json.Unmarshal(encoded, &backup); err != nil {
		return nil, err
	}
	if backup.VeleroBackupName != point.VeleroBackupName {
		return nil, fmt.Errorf("%w: source command names another backup", ErrBackupResultConflict)
	}
	encoded, err = json.Marshal(result["consistency"])
	if err != nil {
		return nil, err
	}
	var record protocol.ConsistencyRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		return nil, err
	}
	return &protocol.ConsistencyRestoreEvidence{TaskID: task.ID, CommandID: task.CommandID, Backup: backup, Record: record}, nil
}
