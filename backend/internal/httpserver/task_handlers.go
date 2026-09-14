package httpserver

import (
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func (r *Router) listTasks(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	filter := store.TaskFilter{ClusterID: query.Get("clusterId")}
	filter.Summary = query.Get("view") == "summary"
	if user, ok := requestUser(req); ok && !user.SystemAdmin {
		filter.TenantID = user.TenantID
	}
	for _, value := range strings.Split(query.Get("types"), ",") {
		if value = strings.TrimSpace(value); value != "" {
			filter.Types = append(filter.Types, value)
		}
	}
	for _, value := range strings.Split(query.Get("statuses"), ",") {
		if value = strings.TrimSpace(value); value != "" {
			filter.Statuses = append(filter.Statuses, value)
		}
	}
	if limit, parseErr := strconv.Atoi(query.Get("limit")); parseErr == nil && limit > 0 {
		if limit > 1000 {
			limit = 1000
		}
		filter.Limit = limit
	}
	items, err := r.store.ListTasksFiltered(filter)
	if err != nil {
		r.logger.Error("failed to list tasks", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_tasks_failed"})
		return
	}
	typeFilter := map[string]struct{}{}
	for _, taskType := range strings.Split(query.Get("types"), ",") {
		if taskType = strings.TrimSpace(taskType); taskType != "" {
			typeFilter[taskType] = struct{}{}
		}
	}
	visible := items[:0]
	for _, item := range items {
		_, typeAllowed := typeFilter[item.Type]
		if tenantVisible(req, item.TenantID) && (len(typeFilter) == 0 || typeAllowed) {
			visible = append(visible, item)
		}
	}
	items = visible
	items = r.enrichCleanupTaskRestorePointTimes(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) latestPlanTask(taskType string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		planID := req.PathValue("id")
		plan, ok, err := r.store.GetProtectionPlan(planID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
			return
		}
		if !ok || !tenantVisible(req, plan.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
			return
		}
		taskID := ""
		if taskType == "backup" {
			taskID = plan.LatestSyncTaskID
		}
		if taskID == "" {
			writeJSON(w, http.StatusOK, map[string]any{"task": nil})
			return
		}
		task, found, taskErr := r.store.GetTask(taskID)
		if taskErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_plan_task_failed"})
			return
		}
		if !found || task.ProtectionPlanID != plan.ID || task.Type != taskType {
			r.logger.Warn("protection plan latest task pointer is invalid", "plan_id", plan.ID, "task_id", taskID, "expected_type", taskType)
			// Recover from a stale pointer by selecting the newest task that is
			// still explicitly associated with this plan and operation type.
			items, listErr := r.store.ListTasksFiltered(store.TaskFilter{TenantID: plan.TenantID, ProtectionPlanID: plan.ID, Types: []string{taskType}, Limit: 200})
			if listErr == nil && len(items) > 0 {
				latest := items[0]
				for _, candidate := range items[1:] {
					if candidate.CreatedAt.After(latest.CreatedAt) {
						latest = candidate
					}
				}
				writeJSON(w, http.StatusOK, map[string]any{"task": latest, "recovered": true})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"task": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
	}
}

func (r *Router) latestPlanRecoveryTask(w http.ResponseWriter, req *http.Request) {
	planID := req.PathValue("id")
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
		return
	}
	if !ok || !tenantVisible(req, plan.TenantID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
		return
	}
	if plan.LatestRecoveryTaskID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"task": nil})
		return
	}
	task, found, taskErr := r.store.GetTask(plan.LatestRecoveryTaskID)
	if taskErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_plan_task_failed"})
		return
	}
	if !found || task.ProtectionPlanID != plan.ID || !slices.Contains([]string{"drill", "restore", "takeover"}, task.Type) {
		r.logger.Warn("protection plan latest recovery task pointer is invalid", "plan_id", plan.ID, "task_id", plan.LatestRecoveryTaskID)
		items, listErr := r.store.ListTasksFiltered(store.TaskFilter{TenantID: plan.TenantID, ProtectionPlanID: plan.ID, Types: []string{"drill", "restore", "takeover"}, Limit: 200})
		if listErr == nil && len(items) > 0 {
			latest := items[0]
			for _, candidate := range items[1:] {
				if candidate.CreatedAt.After(latest.CreatedAt) {
					latest = candidate
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"task": latest, "recovered": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (r *Router) getTask(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	item, ok, err := r.store.GetTask(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_task_failed"})
		return
	}
	if ok && tenantVisible(req, item.TenantID) {
		writeJSON(w, http.StatusOK, item)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "task_not_found"})
}

// enrichCleanupTaskRestorePointTimes keeps restore-point labels derivable for
// historical cleanup tasks after their restore-point records stop appearing in
// the normal active restore-point list. The UI converts this UTC instant using
// its currently selected timezone; no persisted display label is involved.
func (r *Router) enrichCleanupTaskRestorePointTimes(items []store.Task) []store.Task {
	for index := range items {
		if items[index].Type != "retention-cleanup" && items[index].Type != "protection-cleanup" {
			continue
		}
		rawPoints, ok := items[index].Payload["restorePoints"].([]any)
		if !ok || len(rawPoints) == 0 {
			continue
		}
		payload := make(map[string]any, len(items[index].Payload))
		for key, value := range items[index].Payload {
			payload[key] = value
		}
		points := append([]any(nil), rawPoints...)
		changed := false
		for pointIndex, rawPoint := range points {
			pointPayload, ok := rawPoint.(map[string]any)
			if !ok {
				continue
			}
			if taskCreatedAt, exists := pointPayload["taskCreatedAt"]; exists && taskCreatedAt != nil && strings.TrimSpace(fmt.Sprint(taskCreatedAt)) != "" {
				continue
			}
			id := strings.TrimSpace(fmt.Sprint(pointPayload["id"]))
			if id == "" {
				continue
			}
			point, found, err := r.store.GetRestorePoint(id)
			if err != nil || !found || point.TaskCreatedAt.IsZero() {
				continue
			}
			enriched := make(map[string]any, len(pointPayload)+1)
			for key, value := range pointPayload {
				enriched[key] = value
			}
			enriched["taskCreatedAt"] = point.TaskCreatedAt
			points[pointIndex] = enriched
			changed = true
		}
		if changed {
			payload["restorePoints"] = points
			items[index].Payload = payload
		}
	}
	return items
}

func (r *Router) listTaskEvents(w http.ResponseWriter, req *http.Request) {
	taskID := req.PathValue("id")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_id_required"})
		return
	}

	items, err := r.store.ListTaskEvents(taskID)
	if err != nil {
		r.logger.Error("failed to list task events", "task_id", taskID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_task_events_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) cancelTask(w http.ResponseWriter, req *http.Request) {
	taskID := req.PathValue("id")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_id_required"})
		return
	}
	task, ok, err := r.findTaskByID("", taskID)
	if err != nil {
		r.logger.Error("failed to load task for cancel", "task_id", taskID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_task_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "task_not_found"})
		return
	}
	if task.Type == "cluster-registration" {
		if !isActiveTaskStatus(task.Status) || !task.CompletedAt.IsZero() {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "task_not_active", "message": "This cluster registration task is no longer active."})
			return
		}
		if task.Status == "canceling" {
			writeJSON(w, http.StatusOK, map[string]any{"task": task, "reused": true})
			return
		}
		status, done := "canceling", false
		if task.Status == "queued" {
			status, done = "canceled", true
		}
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: status, Progress: task.Progress, ErrorCode: "REGISTRATION_CANCEL_REQUESTED", ErrorMessage: "Registration cancellation requested by user.", Payload: map[string]any{"stage": status}, MarkDone: done})
		_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "warning", Reason: "cancel_requested", Message: "Registration cancellation requested by user."})
		if done {
			sessionID := stringPayload(task.Payload, "sessionId")
			if cceRegistrationSessionIDPattern.MatchString(sessionID) {
				r.cceRegistrationMu.Lock()
				upload := r.cceRegistrationUploads[sessionID]
				delete(r.cceRegistrationUploads, sessionID)
				r.cceRegistrationMu.Unlock()
				if upload.Path != "" {
					_ = os.RemoveAll(filepath.Dir(upload.Path))
				}
			}
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task})
		return
	}
	if task.Type != "backup" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "task_cancel_unsupported", "message": "Only running sync tasks can be force stopped."})
		return
	}
	if !isActiveTaskStatus(task.Status) || !task.CompletedAt.IsZero() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "task_not_active", "message": "This sync task is no longer active."})
		return
	}
	if task.Status == "canceling" {
		writeJSON(w, http.StatusOK, map[string]any{"task": task, "warning": "Force stop is already in progress.", "reused": true})
		return
	}
	cancelCommandID := store.NewPublicID()
	cancelTask, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        task.ClusterID,
		AppID:            task.AppID,
		ProtectionPlanID: task.ProtectionPlanID,
		Type:             "backup-cancel",
		Status:           "queued",
		CommandID:        cancelCommandID,
		Payload: map[string]any{
			"requestedBy":      requestActor(req),
			"targetTaskId":     task.ID,
			"planId":           task.ProtectionPlanID,
			"veleroBackupName": stringPayload(task.Payload, "veleroBackupName"),
			"reason":           "user_requested",
		},
	})
	if err != nil {
		r.logger.Error("failed to create backup cancel task", "task_id", task.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_cancel_task_failed"})
		return
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       task.ID,
		Status:       "canceling",
		Progress:     task.Progress,
		ErrorCode:    "SYNC_CANCEL_REQUESTED",
		ErrorMessage: "Force stop requested by user.",
		Payload: map[string]any{
			"cancelTaskId": cancelTask.ID,
			"cancelReason": "user_requested",
		},
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "warning",
		Reason:  "cancel_requested",
		Message: "Force stop requested by user.",
		Payload: map[string]any{"cancelTaskId": cancelTask.ID},
	})
	conn, online := r.hub.get(task.ClusterID)
	if !online {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       cancelTask.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; force stop will be dispatched after reconnect",
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "cancelTask": cancelTask, "warning": "Agent is offline; force stop is queued."})
		return
	}
	if err := r.dispatchStoredTask(conn, cancelTask); err != nil {
		r.logger.Error("failed to dispatch backup cancel task", "task_id", task.ID, "cancel_task_id", cancelTask.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       cancelTask.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "cancelTask": cancelTask, "warning": "Force stop is queued."})
		return
	}
	cancelTask, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   cancelTask.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  cancelTask.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "Force stop dispatched to source cluster agent.",
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "cancelTask": cancelTask})
}

func (r *Router) cleanupDrillTask(w http.ResponseWriter, req *http.Request) {
	taskID := req.PathValue("id")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_id_required"})
		return
	}
	drill, ok, err := r.findTaskByID("", taskID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_task_failed", "message": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "task_not_found"})
		return
	}
	if drill.Type != "drill" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "drill_cleanup_unsupported", "message": "Only DR drill tasks can be cleaned up."})
		return
	}
	if isActiveTaskStatus(drill.Status) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "drill_still_running", "message": "Wait for the drill to finish before cleaning up its resources."})
		return
	}
	targetNamespaces := uniqueNonEmptyStrings(append(
		[]string{stringPayload(drill.Payload, "targetNamespace")},
		mapStringValues(stringMapPayload(drill.Payload, "targetNamespaces"))...,
	))
	sourceNamespaces := uniqueNonEmptyStrings(append(
		[]string{stringPayload(drill.Payload, "sourceNamespace")},
		stringSlicePayload(drill.Payload, "sourceNamespaces")...,
	))
	if len(targetNamespaces) == 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "drill_target_namespace_missing", "message": "The drill task does not record a target namespace."})
		return
	}
	for _, namespace := range targetNamespaces {
		if namespace == r.agentNamespaceForCluster(drill.ClusterID) || namespace == r.dataProtectionNamespaceForCluster(drill.ClusterID) || slices.Contains(sourceNamespaces, namespace) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "unsafe_drill_cleanup_target", "message": "Refusing to delete an agent or source namespace."})
			return
		}
	}
	items, err := r.store.ListTasks(drill.ClusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_tasks_failed", "message": err.Error()})
		return
	}
	for _, item := range items {
		if item.Type == "protection-cleanup" && stringPayload(item.Payload, "cleanupMode") == "drill" && stringPayload(item.Payload, "drillTaskId") == drill.ID {
			if item.Status == "succeeded" || isActiveTaskStatus(item.Status) {
				writeJSON(w, http.StatusOK, map[string]any{"task": item, "reused": true})
				return
			}
		}
	}
	cleanup, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        drill.ClusterID,
		ProtectionPlanID: drill.ProtectionPlanID,
		Type:             "protection-cleanup",
		Status:           "queued",
		CommandID:        store.NewPublicID(),
		Payload: map[string]any{
			"planId":           drill.ProtectionPlanID,
			"cleanupMode":      "drill",
			"drillTaskId":      drill.ID,
			"namespace":        r.dataProtectionNamespaceForCluster(drill.ClusterID),
			"sourceNamespaces": sourceNamespaces,
			"restoreNames": uniqueNonEmptyStrings([]string{
				stringPayload(drill.Payload, "veleroRestoreName"),
				stringPayload(drill.Payload, "veleroBackupName"),
			}),
			"drillNamespaces": targetNamespaces,
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_drill_cleanup_failed", "message": err.Error()})
		return
	}
	conn, connected := r.hub.get(cleanup.ClusterID)
	warning := ""
	if !connected {
		warning = "Target cluster agent is offline; drill cleanup remains queued."
	} else if err := r.dispatchStoredTask(conn, cleanup); err != nil {
		warning = "Drill cleanup was created but dispatch failed; it remains queued."
	} else {
		cleanup, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: cleanup.ID, Status: "dispatched", Progress: 0})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task": cleanup, "warning": warning})
}

func mapStringValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
