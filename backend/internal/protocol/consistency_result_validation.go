package protocol

import (
	"fmt"
	"time"
)

func ValidateConsistencyRestoreEvidence(command RestoreCommand) error {
	evidence := command.ConsistencyEvidence
	if evidence == nil {
		return nil
	}
	if evidence.CommandID == "" || evidence.Backup.VeleroBackupName == "" || evidence.Backup.VeleroBackupName != command.VeleroBackupName {
		return fmt.Errorf("consistency restore evidence is not bound to the selected backup")
	}
	if err := ValidateConsistencyTaskResult(TaskDispatchPayload{TaskID: evidence.TaskID, CommandID: evidence.CommandID, Type: "backup", Backup: &evidence.Backup}, evidence.Record); err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, namespace := range command.SourceNamespaces {
		selected[namespace] = true
	}
	if len(command.SourceNamespaces) == 0 && command.SourceNamespace != "" {
		selected[command.SourceNamespace] = true
	}
	for _, volume := range evidence.Record.Volumes {
		if !selected[volume.Namespace] {
			return fmt.Errorf("consistency restore must include the entire volume group")
		}
	}
	for _, workload := range evidence.Record.Workloads {
		if !selected[workload.Namespace] {
			return fmt.Errorf("consistency restore must include the entire workload group")
		}
	}
	if len(command.IncludedResources) != 0 || len(command.ExcludedResources) != 0 {
		return fmt.Errorf("filtered application-consistent restores are not supported")
	}
	return nil
}

func ValidateConsistencyTaskIdentity(task TaskDispatchPayload, record ConsistencyRecord) error {
	if task.Type != "backup" || task.Backup == nil || task.Backup.DataProtectionMode != "application-consistent" || task.Backup.ConsistencyGroup == nil {
		return fmt.Errorf("application-consistent backup command is required")
	}
	command := task.Backup
	namespaces := command.SourceNamespaces
	if len(namespaces) == 0 && command.SourceNamespace != "" {
		namespaces = []string{command.SourceNamespace}
	}
	group := command.ConsistencyGroup
	if err := group.Validate(namespaces); err != nil {
		return err
	}
	if task.TaskID == "" || record.ID != task.TaskID || record.GroupID != group.ID || record.Strategy != group.Strategy || record.ApplicationProfile != group.ApplicationProfile {
		return fmt.Errorf("consistency result does not match dispatched task and group")
	}
	if len(record.Volumes) != len(group.Volumes) || len(record.Workloads) != len(group.Workloads) {
		return fmt.Errorf("consistency result inventory differs from dispatched group")
	}
	volumes := map[string]ConsistencyVolumeIdentity{}
	for _, volume := range record.Volumes {
		if _, duplicate := volumes[volume.UID]; duplicate {
			return fmt.Errorf("duplicate result volume identity")
		}
		volumes[volume.UID] = volume
	}
	for _, expected := range group.Volumes {
		actual, found := volumes[expected.UID]
		if !found || actual.Namespace != expected.Namespace || actual.Name != expected.Name {
			return fmt.Errorf("consistency result volume does not match dispatched identity")
		}
	}
	workloads := map[string]ConsistencyWorkloadState{}
	for _, workload := range record.Workloads {
		if _, duplicate := workloads[workload.UID]; duplicate {
			return fmt.Errorf("duplicate result workload identity")
		}
		workloads[workload.UID] = workload
	}
	for _, expected := range group.Workloads {
		actual, found := workloads[expected.UID]
		if !found || actual.Namespace != expected.Namespace || actual.Name != expected.Name || actual.Kind != expected.Kind {
			return fmt.Errorf("consistency result workload does not match dispatched identity")
		}
	}
	return nil
}

func ValidateConsistencyTaskResult(task TaskDispatchPayload, record ConsistencyRecord) error {
	if err := ValidateConsistencyTaskIdentity(task, record); err != nil {
		return err
	}
	if record.Phase != "completed" || !record.Resumed || !record.LocksReleased || record.Failure != "" {
		return fmt.Errorf("consistency result is not a completed, resumed and unlocked run")
	}
	if err := VerifyConsistencyQuiescenceProof(record); err != nil {
		return err
	}
	if err := VerifyConsistencyResumeWindow(record, task.Backup.ConsistencyGroup.MaxPauseSeconds); err != nil {
		return err
	}
	return VerifyConsistencyDurableSnapshots(record)
}

func VerifyConsistencyResumeWindow(record ConsistencyRecord, maxPauseSeconds int) error {
	if record.ResumeRequestedAt.IsZero() && record.ResumeReadyDeadline.IsZero() {
		return nil
	}
	if maxPauseSeconds < 1 || maxPauseSeconds > 120 || record.StartedAt.IsZero() || record.ResumeRequestedAt.IsZero() || record.ResumeReadyDeadline.IsZero() || record.ResumeRequestedAt.Before(record.StartedAt) || !record.ResumeRequestedAt.Before(record.Deadline) || !record.ResumeReadyDeadline.After(record.Deadline) || record.ResumeReadyDeadline.After(record.StartedAt.Add(time.Duration(maxPauseSeconds)*time.Second)) {
		return fmt.Errorf("invalid consistency resume window or pause budget")
	}
	if record.QuiescenceProof == nil || record.QuiescenceProof.ObservedAt.After(record.ResumeRequestedAt) {
		return fmt.Errorf("resume request lacks a preceding quiescence proof")
	}
	return nil
}

func VerifyConsistencySnapshotInventory(record ConsistencyRecord) error {
	if len(record.Volumes) == 0 || len(record.Snapshots) != len(record.Volumes) {
		return fmt.Errorf("snapshot inventory does not cover every required volume")
	}
	secured := map[string]bool{}
	uploads := map[string]bool{}
	for _, snapshot := range record.Snapshots {
		if !snapshot.Secured || snapshot.Handle == "" || snapshot.VolumeUID == "" || snapshot.DataUploadUID == "" || uploads[snapshot.DataUploadUID] || (snapshot.Source != "csi" && snapshot.Source != "kopia") || secured[snapshot.VolumeUID] {
			return fmt.Errorf("snapshot source is not secured or is duplicated")
		}
		secured[snapshot.VolumeUID] = true
		uploads[snapshot.DataUploadUID] = true
	}
	identities := map[string]bool{}
	names := map[string]bool{}
	for _, volume := range record.Volumes {
		key := volume.Namespace + "/" + volume.Name
		if volume.Namespace == "" || volume.Name == "" || volume.UID == "" || identities[volume.UID] || names[key] {
			return fmt.Errorf("invalid or duplicate snapshot volume inventory")
		}
		identities[volume.UID], names[key] = true, true
		if !secured[volume.UID] {
			return fmt.Errorf("snapshot missing for PVC %s/%s", volume.Namespace, volume.Name)
		}
	}
	return nil
}

func VerifyConsistencyDurableSnapshots(record ConsistencyRecord) error {
	if err := VerifyConsistencySnapshotInventory(record); err != nil {
		return err
	}
	for _, snapshot := range record.Snapshots {
		if snapshot.Source != "kopia" {
			return fmt.Errorf("required volume has no verified durable repository snapshot")
		}
	}
	return nil
}
