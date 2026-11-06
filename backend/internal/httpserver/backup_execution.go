package httpserver

import (
	"errors"
	"fmt"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func (r *Router) observeInventoryBackups(clusterID string, backups []map[string]any) {
	for _, backup := range backups {
		labels := mapPayload(backup, "labels")
		taskID := stringFromMap(labels, "hypercdr.io/task-id")
		if taskID == "" {
			continue
		}
		task, found, err := r.findTaskByID(clusterID, taskID)
		if err != nil || !found || task.Type != "backup" || !task.CompletedAt.IsZero() {
			continue
		}
		if task.ProtectionPlanID != stringFromMap(labels, "hypercdr.io/plan-id") {
			continue
		}
		if name := taskPayloadString(task.Payload, "veleroBackupName"); name != "" && name != stringFromMap(backup, "name") {
			continue
		}
		if _, err := r.recordBackupObservation(task, backup, stringFromMap(backup, "phase")); err != nil {
			r.logger.Error("failed to record backup inventory observation", "task_id", task.ID, "error", err)
		}
	}
}

func (r *Router) recordBackupObservation(task store.Task, details map[string]any, phase string) (store.Task, error) {
	updated, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID: task.ID,
		Payload: map[string]any{"backupObservation": map[string]any{
			"phase": phase, "details": details, "observedAt": time.Now().UTC(),
		}},
	})
	return updated, err
}

func backupCompletionMetrics(payload protocol.TaskCompletedPayload) map[string]any {
	metrics := mapFromAnyJSON(payload.SizeMetricsV2)
	if len(metrics) > 0 {
		return metrics
	}
	size := firstNonEmptyMap(mapFromAny(payload.Velero["restorePointSize"]), payload.Size, mapFromAny(payload.Velero["size"]))
	allKnown, _ := size["allIncrementalKnown"].(bool)
	if _, present := size["allIncrementalKnown"]; !present {
		if count, reported := size["volumeCount"]; reported {
			allKnown = int64FromAny(count) == int64FromAny(size["incrementalKnownCount"])
		}
	}
	_, uploadedKnown := size["uploadedBytes"]
	if !allKnown || !uploadedKnown || int64FromAny(size["totalBytes"]) <= 0 {
		return nil
	}
	return map[string]any{
		"schemaVersion": 2, "operation": "backup", "measurementStatus": "complete",
		"logical": map[string]any{"known": true, "totalBytes": size["totalBytes"], "volumeBytes": size["volumeBytes"], "metadataBytes": size["metadataBytes"]},
		"newData": map[string]any{"known": true, "totalBytes": size["uploadedBytes"], "volumeBytes": size["uploadedVolumeBytes"], "metadataBytes": size["uploadedMetadataBytes"]},
		"quality": map[string]any{"source": "v1CompletionAdapter"},
	}
}

func (r *Router) acceptBackupCompletion(task store.Task, payload protocol.TaskCompletedPayload) error {
	if payload.CommandID != "" && payload.CommandID != task.CommandID {
		return fmt.Errorf("%w: command does not match task", store.ErrBackupResultConflict)
	}
	if payload.Operation != "" && payload.Operation != "backup" {
		return fmt.Errorf("%w: operation does not match task", store.ErrBackupResultConflict)
	}
	if payload.Status != "" && payload.Status != "succeeded" {
		return fmt.Errorf("%w: outcome is not succeeded", store.ErrBackupResultConflict)
	}
	if stringFromMap(payload.Velero, "kind") != "Backup" || stringFromMap(payload.Velero, "name") == "" {
		return store.ErrBackupResultIncomplete
	}
	if phase := stringFromMap(payload.Velero, "phase"); phase != "" && phase != "Completed" {
		return fmt.Errorf("%w: backup resource is not completed", store.ErrBackupResultConflict)
	}
	if name := taskPayloadString(task.Payload, "veleroBackupName"); name != "" && name != stringFromMap(payload.Velero, "name") {
		return fmt.Errorf("%w: resource does not match task", store.ErrBackupResultConflict)
	}
	metrics := backupCompletionMetrics(payload)
	if !store.CompleteBackupMetrics(metrics) {
		_, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "finalizing", ErrorCode: "BACKUP_RESULT_INCOMPLETE", ErrorMessage: "Waiting for complete backup size measurements.", Payload: map[string]any{"backupResultStatus": "awaiting_measurements"}})
		if err != nil {
			return err
		}
		return store.ErrBackupResultIncomplete
	}
	patch := taskCompletedPayloadPatch(payload)
	patch["sizeMetricsV2"] = metrics
	previous := task.Payload
	task.Payload = make(map[string]any, len(previous)+len(patch))
	for key, value := range previous {
		task.Payload[key] = value
	}
	for key, value := range patch {
		task.Payload[key] = value
	}
	point, err := r.createRestorePointFromBackup(task, payload.Velero)
	if errors.Is(err, store.ErrRestorePointReplacementPending) {
		r.deferRestorePointReplacement(task.ID, err)
		return nil
	}
	if err != nil {
		return err
	}
	if point.ID == "" {
		return store.ErrBackupResultIncomplete
	}
	r.reconcileRetention(point.ProtectionPlanID, point.BackupTaskID)
	r.collectInitialPlanCapacityAfterBackup(point.ProtectionPlanID)
	return nil
}
