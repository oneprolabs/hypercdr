package httpserver

import (
	"errors"
	"hypercdr-platform/platform/backend/internal/store"
	"time"
)

func (r *Router) reconcileRestorePointReplacements() {
	items, err := r.store.PendingRestorePointReplacements()
	if err != nil {
		r.logger.Error("failed to load pending restore points", "error", err)
		return
	}
	for _, item := range items {
		var point store.RestorePoint
		if item.PointID != "" {
			var found bool
			point, found, err = r.store.GetRestorePoint(item.PointID)
			if err != nil || !found {
				continue
			}
		}
		if item.PointID == "" || point.Status == "deleted" {
			if task, found, taskErr := r.store.GetTask(item.TaskID); taskErr == nil && found && task.Payload["backupResult"] == nil {
				item.Input.SizeMetricsV2 = mapFromAnyJSON(task.Payload["sizeMetricsV2"])
				if item.Input.Metadata == nil {
					item.Input.Metadata = map[string]any{}
				}
				item.Input.Metadata["sizeMetricsV2"] = item.Input.SizeMetricsV2
			}
			created, err := r.store.PublishBackupResult(item.Input)
			if err != nil {
				r.logger.Error("restore point publication pending", "task_id", item.TaskID, "error", err)
				continue
			}
			r.scheduleRestorePointContentIndex(created)
			_ = r.addTaskEventIfChanged(store.TaskEventInput{TaskID: item.TaskID, Level: "info", Reason: "restore_point_published", Message: "Old restore point deleted; new restore point published."})
			continue
		}
		if item.CleanupTaskID != "" {
			cleanup, found, err := r.store.GetTask(item.CleanupTaskID)
			if err != nil || !found {
				continue
			}
			if cleanup.Status == "succeeded" {
				r.finishRetentionCleanupTask(cleanup, map[string]any{"deleted": cleanup.Payload["deletedRestorePointIds"]})
				continue
			}
			if cleanup.Status != "failed" && cleanup.Status != "canceled" && cleanup.Status != "cancelled" {
				continue
			}
			r.markQuotaReplacementWarning(item.TaskID, "New backup data is ready, but deletion of the old restore point failed. The replacement remains locked; cleanup will retry after resolving the cluster error.")
			delay := 30 * time.Second * time.Duration(1<<min(item.Attempts, 6))
			if cleanup.CompletedAt.IsZero() || time.Since(cleanup.CompletedAt) < delay {
				continue
			}
		}
		cleanup, warning, err := r.createRestorePointDeleteTask(point.SourceClusterID, []store.RestorePoint{point})
		if err != nil {
			r.logger.Error("failed to start quota replacement", "task_id", item.TaskID, "error", err)
			r.markQuotaReplacementWarning(item.TaskID, err.Error())
			continue
		}
		if err := r.store.BeginRestorePointReplacement(item.TaskID, cleanup.ID); err != nil {
			r.logger.Error("failed to associate quota cleanup", "task_id", item.TaskID, "error", err)
			continue
		}
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: item.TaskID, Status: "finalizing", Progress: 100, Payload: map[string]any{"quotaStage": "deleting_old_point", "quotaCleanupTaskId": cleanup.ID}})
		if warning != "" {
			r.logger.Warn("quota cleanup waiting", "task_id", item.TaskID, "warning", warning)
		}
	}
}

func (r *Router) markQuotaReplacementWarning(taskID, message string) {
	task, found, err := r.store.GetTask(taskID)
	if err == nil && found && task.ErrorCode == "QUOTA_REPLACEMENT_PENDING" && task.ErrorMessage == message {
		return
	}
	r.markBackupTaskRetentionWarning(taskID, "QUOTA_REPLACEMENT_PENDING", message)
}

func (r *Router) deferRestorePointReplacement(taskID string, err error) bool {
	if !errors.Is(err, store.ErrRestorePointReplacementPending) {
		return false
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: taskID, Status: "finalizing", Progress: 100, Payload: map[string]any{"quotaStage": "data_ready"}})
	_ = r.addTaskEventIfChanged(store.TaskEventInput{TaskID: taskID, Level: "info", Reason: "quota_replacement", Message: "Backup data is ready. Waiting for old restore point deletion before publishing the new point."})
	return true
}
