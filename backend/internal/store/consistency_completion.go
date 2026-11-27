package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func validatePublicationConsistency(tx *sql.Tx, task Task, input RestorePointInput) error {
	result, _ := input.Metadata["velero"].(map[string]any)
	if err := ValidateTaskConsistencyCompletion(task, result); err != nil {
		return err
	}
	if task.Payload["dataProtectionMode"] == "snapshot-crash-consistent" && input.VeleroBackupName != result["name"] {
		return fmt.Errorf("%w: snapshot restore point names a different verified backup", ErrBackupResultConflict)
	}
	if _, captured := task.Payload["dataProtectionSnapshotVersion"]; captured {
		return nil
	}
	if task.Payload["dataProtectionMode"] == "application-consistent" {
		return fmt.Errorf("%w: strict publication requires a creation-time consistency snapshot", ErrBackupResultConflict)
	}
	if input.ProtectionPlanID == "" {
		return nil
	}
	var raw []byte
	if err := tx.QueryRow(`select data_protection from protection_plans where id=$1`, input.ProtectionPlanID).Scan(&raw); err != nil {
		return err
	}
	var plan ProtectionPlan
	if err := decodePlanDataProtection(raw, &plan); err != nil {
		return err
	}
	if plan.DataProtection.Mode != "live-filesystem" {
		return fmt.Errorf("%w: strict publication requires a creation-time consistency snapshot", ErrBackupResultConflict)
	}
	return nil
}

func backupConsistencyEqual(previous, incoming map[string]any) bool {
	previousResult, _ := previous["velero"].(map[string]any)
	incomingResult, _ := incoming["velero"].(map[string]any)
	for _, key := range []string{"requestedConsistency", "achievedConsistency", "consistency", "snapshotEvidence"} {
		previousJSON, previousErr := canonicalConsistencyJSON(previousResult[key])
		incomingJSON, incomingErr := canonicalConsistencyJSON(incomingResult[key])
		if previousErr != nil || incomingErr != nil || !bytes.Equal(previousJSON, incomingJSON) {
			return false
		}
	}
	return true
}

func canonicalConsistencyJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func ValidateTaskConsistencyCompletion(task Task, result map[string]any) error {
	mode, hasMode := task.Payload["dataProtectionMode"]
	group, hasGroup := task.Payload["consistencyGroup"]
	if version, present := task.Payload["dataProtectionSnapshotVersion"]; present {
		encoded, err := json.Marshal(version)
		if err != nil || string(encoded) != "1" || !hasMode || !hasGroup || (mode != "live-filesystem" && mode != "snapshot-crash-consistent" && mode != "application-consistent") {
			return fmt.Errorf("%w: invalid task consistency snapshot", ErrBackupResultConflict)
		}
	}
	if (!hasMode || mode == "" || mode == "live-filesystem") && (!hasGroup || group == nil) {
		if result["consistency"] != nil || result["snapshotEvidence"] != nil || result["achievedConsistency"] == "application-consistent" || result["requestedConsistency"] == "application-consistent" || result["achievedConsistency"] == "snapshot-crash-consistent" || result["requestedConsistency"] == "snapshot-crash-consistent" {
			return fmt.Errorf("%w: application consistency was not requested by the stored task", ErrBackupResultConflict)
		}
		return nil
	}
	if mode == "snapshot-crash-consistent" && hasGroup && group == nil {
		if _, captured := task.Payload["dataProtectionSnapshotVersion"]; !captured {
			return fmt.Errorf("%w: snapshot publication requires a versioned task snapshot", ErrBackupResultConflict)
		}
		encoded, err := json.Marshal(task.Payload)
		if err != nil {
			return fmt.Errorf("%w: invalid snapshot command", ErrBackupResultConflict)
		}
		var command protocol.BackupCommand
		if err := json.Unmarshal(encoded, &command); err != nil {
			return fmt.Errorf("%w: invalid snapshot command", ErrBackupResultConflict)
		}
		if err := protocol.ValidateSnapshotBackupResult(protocol.TaskDispatchPayload{TaskID: task.ID, CommandID: task.CommandID, Type: task.Type, Backup: &command}, result); err != nil {
			return fmt.Errorf("%w: %v", ErrBackupResultConflict, err)
		}
		return nil
	}
	if mode != "application-consistent" || !hasGroup || group == nil || result["requestedConsistency"] != "application-consistent" || result["achievedConsistency"] != "application-consistent" {
		return fmt.Errorf("%w: strict consistency result is missing its requested and achieved mode", ErrBackupResultConflict)
	}
	if result["snapshotEvidence"] != nil {
		return fmt.Errorf("%w: application completion cannot mix ordinary snapshot evidence", ErrBackupResultConflict)
	}
	encodedGroup, err := json.Marshal(group)
	if err != nil {
		return fmt.Errorf("%w: invalid stored consistency group", ErrBackupResultConflict)
	}
	var expectedGroup protocol.ConsistencyGroup
	if err := json.Unmarshal(encodedGroup, &expectedGroup); err != nil {
		return fmt.Errorf("%w: invalid stored consistency group", ErrBackupResultConflict)
	}
	encodedRecord, err := json.Marshal(result["consistency"])
	if err != nil {
		return fmt.Errorf("%w: invalid consistency evidence", ErrBackupResultConflict)
	}
	var record protocol.ConsistencyRecord
	if err := json.Unmarshal(encodedRecord, &record); err != nil {
		return fmt.Errorf("%w: invalid consistency evidence", ErrBackupResultConflict)
	}
	encodedPayload, err := json.Marshal(task.Payload)
	if err != nil {
		return fmt.Errorf("%w: invalid task payload", ErrBackupResultConflict)
	}
	var command protocol.BackupCommand
	if err := json.Unmarshal(encodedPayload, &command); err != nil {
		return fmt.Errorf("%w: invalid backup command", ErrBackupResultConflict)
	}
	command.ConsistencyGroup = &expectedGroup
	taskCommand := protocol.TaskDispatchPayload{TaskID: task.ID, CommandID: task.CommandID, Type: task.Type, Backup: &command}
	if err := protocol.ValidateConsistencyTaskResult(taskCommand, record); err != nil {
		return fmt.Errorf("%w: %v", ErrBackupResultConflict, err)
	}
	return nil
}
