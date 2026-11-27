package httpserver

import (
	"fmt"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func (r *Router) attachConsistencyRestoreEvidence(task store.Task, point store.RestorePoint, command *protocol.RestoreCommand) error {
	result, _ := point.Metadata["velero"].(map[string]any)
	if result["requestedConsistency"] != "application-consistent" && result["achievedConsistency"] != "application-consistent" && result["consistency"] == nil {
		return nil
	}
	if point.TenantID != task.TenantID || point.ID != task.RestorePointID || point.VeleroBackupName != command.VeleroBackupName || point.BackupTaskID == "" {
		return fmt.Errorf("application-consistent restore point is not bound to this task")
	}
	if !r.hub.supportsConsistencyRestore(task.ClusterID) {
		return fmt.Errorf("destination Agent does not support verified application-consistent restore")
	}
	source, found, err := r.store.GetTask(point.BackupTaskID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("application-consistent source task is unavailable")
	}
	evidence, err := store.BuildConsistencyRestoreEvidence(point, source)
	if err != nil {
		return err
	}
	if evidence == nil {
		return fmt.Errorf("application-consistent restore point has no accepted evidence")
	}
	command.ConsistencyEvidence = evidence
	return protocol.ValidateConsistencyRestoreEvidence(*command)
}
