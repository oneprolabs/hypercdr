package httpserver

import (
	"errors"
	"net/http"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func supportsTaskAbort(taskType string) bool {
	switch taskType {
	case "backup", "restore", "drill", "takeover", "protection-cleanup", "retention-cleanup", "storage-sync", "schedule-sync":
		return true
	}
	return false
}

func (r *Router) abortTask(w http.ResponseWriter, req *http.Request) {
	target, found, err := r.findTaskByID("", req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "get_task_failed"})
		return
	}
	if !found {
		writeJSON(w, 404, map[string]any{"error": "task_not_found"})
		return
	}
	if !supportsTaskAbort(target.Type) || !isActiveTaskStatus(target.Status) || !target.CompletedAt.IsZero() {
		writeJSON(w, 409, map[string]any{"error": "task_not_abortable", "message": "This task is not active or does not support force abort."})
		return
	}
	if target.Payload["quotaStage"] == "data_ready" || target.Payload["quotaStage"] == "deleting_old_point" {
		writeJSON(w, 409, map[string]any{"error": "restore_point_replacement_in_progress", "message": "Restore point replacement must finish before this task can be aborted."})
		return
	}
	if target.Status == "canceling" {
		writeJSON(w, 200, map[string]any{"task": target, "reused": true})
		return
	}
	if !r.hub.supportsTaskAbort(target.ClusterID) {
		writeJSON(w, 409, map[string]any{"error": "task_abort_agent_unavailable", "message": "The task's Comm-agent must be online and upgraded before force abort."})
		return
	}
	commandID := store.NewPublicID()
	abort, err := r.store.CreateTask(store.TaskInput{
		ClusterID: target.ClusterID, AppID: target.AppID, ProtectionPlanID: target.ProtectionPlanID,
		Type: "task-abort", Status: "queued", CommandID: commandID,
		Payload: map[string]any{"targetTaskId": target.ID, "requestedBy": requestActor(req)},
	})
	if err != nil {
		if errors.Is(err, store.ErrProtectionPlanBusy) || errors.Is(err, store.ErrRestorePointLocked) {
			writeJSON(w, 409, map[string]any{"error": "task_not_abortable", "message": "The task completed or entered restore point replacement. Refresh its status."})
			return
		}
		writeJSON(w, 500, map[string]any{"error": "create_abort_task_failed"})
		return
	}
	if abort.CommandID != commandID {
		current, ok, loadErr := r.store.GetTask(target.ID)
		if loadErr != nil || !ok {
			writeJSON(w, 500, map[string]any{"error": "get_task_failed"})
			return
		}
		writeJSON(w, 200, map[string]any{"task": current, "cancelTask": abort, "reused": true})
		return
	}
	target, _, err = r.findTaskByID(target.ClusterID, target.ID)
	if err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: abort.ID, Status: "failed", ErrorCode: "ABORT_REQUEST_FAILED", ErrorMessage: err.Error(), MarkDone: true})
		writeJSON(w, 500, map[string]any{"error": "abort_request_failed"})
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: target.ID, Level: "warning", Reason: "abort_requested", Message: "Force abort requested by user.", Payload: map[string]any{"abortTaskId": abort.ID}})
	conn, online := r.hub.get(target.ClusterID)
	if !online {
		writeJSON(w, 202, map[string]any{"task": target, "cancelTask": abort, "warning": "Agent disconnected; force abort is queued."})
		return
	}
	if err := r.dispatchStoredTask(conn, abort); err != nil {
		writeJSON(w, 202, map[string]any{"task": target, "cancelTask": abort, "warning": "Force abort is queued for agent delivery."})
		return
	}
	writeJSON(w, 202, map[string]any{"task": target, "cancelTask": abort})
}

func (r *Router) finishTaskAbort(clusterID string, abort store.Task, completed protocol.TaskCompletedPayload) error {
	target, found, err := r.findTaskByID(clusterID, stringPayload(abort.Payload, "targetTaskId"))
	if err != nil {
		return err
	}
	if !found || !supportsTaskAbort(target.Type) {
		return errors.New("abort target task not found")
	}
	if target.CompletedAt.IsZero() && stringPayload(target.Payload, "abortTaskId") == abort.ID {
		target, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID: target.ID, Status: "failed", Progress: target.Progress, MarkDone: true,
			ErrorCode: "TASK_ABORTED_BY_USER", ErrorMessage: "Aborted by user.",
			Payload: map[string]any{"abortRequested": true, "abortConfirmed": true, "abortTaskId": abort.ID},
		})
		if err != nil {
			return err
		}
		if target.Type == "protection-cleanup" {
			r.markProtectionCleanupFailed(target, "Aborted by user. Completed deletions cannot be rolled back.")
		}
		if target.Type == "retention-cleanup" {
			r.markRetentionCleanupFailed(target, "Aborted by user.")
		}
		_ = r.addTaskEventIfChanged(store.TaskEventInput{TaskID: target.ID, Level: "warning", Reason: "TASK_ABORTED_BY_USER", Message: "Aborted by user. Completed changes have not been rolled back."})
	}
	_, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: abort.ID, Status: "succeeded", Progress: 100, Payload: taskCompletedPayloadPatch(completed), MarkStarted: true, MarkDone: true})
	return err
}

func (r *Router) markTaskAbortFailed(abort store.Task, message string) {
	target, found, err := r.findTaskByID(abort.ClusterID, stringPayload(abort.Payload, "targetTaskId"))
	if err != nil || !found || !target.CompletedAt.IsZero() || stringPayload(target.Payload, "abortTaskId") != abort.ID {
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID: target.ID, Status: "running", Progress: target.Progress,
		ErrorCode: "TASK_ABORT_FAILED", ErrorMessage: message,
		Payload: map[string]any{"abortFailed": true, "abortRequested": false},
	})
	_ = r.addTaskEventIfChanged(store.TaskEventInput{TaskID: target.ID, Level: "error", Reason: "TASK_ABORT_FAILED", Message: message})
}

func (r *Router) reconcileTaskAbortTimeouts(now time.Time) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return
	}
	for _, task := range tasks {
		if task.Type != "task-abort" || !isActiveTaskStatus(task.Status) || task.CreatedAt.IsZero() || now.Before(task.CreatedAt.Add(2*time.Minute)) {
			continue
		}
		message := "The agent did not confirm force abort within two minutes. Check agent connectivity and retry force abort."
		_, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "failed", MarkDone: true, ErrorCode: "TASK_ABORT_TIMEOUT", ErrorMessage: message})
		if err == nil {
			r.markTaskAbortFailed(task, message)
		}
	}
}
