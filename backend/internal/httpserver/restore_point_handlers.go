package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (r *Router) listRestorePoints(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	filter := store.RestorePointFilter{
		ClusterID:        req.URL.Query().Get("clusterId"),
		AppID:            req.URL.Query().Get("appId"),
		ProtectionPlanID: req.URL.Query().Get("protectionPlanId"),
		Summary:          query.Get("view") == "summary",
	}
	if user, ok := requestUser(req); ok {
		filter.TenantID = user.TenantID
	}
	if pageSize, parseErr := strconv.Atoi(query.Get("pageSize")); parseErr == nil && pageSize > 0 {
		if pageSize > 500 {
			pageSize = 500
		}
		filter.Limit = pageSize
		if page, pageErr := strconv.Atoi(query.Get("page")); pageErr == nil && page > 1 {
			filter.Offset = (page - 1) * pageSize
		}
	}
	items, err := r.store.ListRestorePoints(filter)
	if err != nil {
		r.logger.Error("failed to list restore points", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_restore_points_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	selections := map[string]map[string][]store.RetentionReason{}
	inUse := map[string]bool{}
	for index := range items {
		point := &items[index]
		if point.Metadata == nil {
			point.Metadata = map[string]any{}
		}
		if point.ProtectionPlanID == "" {
			continue
		}
		selected, loaded := selections[point.ProtectionPlanID]
		if !loaded {
			tasks, taskErr := r.store.ListTasksFiltered(store.TaskFilter{TenantID: point.TenantID, ProtectionPlanID: point.ProtectionPlanID, Types: []string{"drill", "restore", "takeover"}})
			if taskErr == nil {
				for _, task := range tasks {
					if task.CompletedAt.IsZero() && isActiveTaskStatus(task.Status) {
						id := task.RestorePointID
						if id == "" {
							id = stringPayload(task.Payload, "restorePointId")
						}
						inUse[id] = true
					}
				}
			}
			plan, found, loadErr := r.store.GetProtectionPlan(point.ProtectionPlanID)
			if loadErr == nil && found {
				policy, found, loadErr := r.findPolicy(plan.PolicyID)
				if loadErr == nil && found {
					points, loadErr := r.store.ListRestorePoints(store.RestorePointFilter{TenantID: point.TenantID, ProtectionPlanID: plan.ID, IncludeDeleted: true})
					if loadErr == nil {
						selected, _ = store.RetentionSelections(points, policy.RetentionRules, policy.RetentionTimezone, time.Now().UTC())
					}
				}
			}
			selections[point.ProtectionPlanID] = selected
		}
		point.Metadata["retentionReasons"] = selected[point.ID]
		point.Metadata["inUse"] = inUse[point.ID]
	}
	items = enrichRestorePointStorageIncrements(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) deleteRestorePoints(w http.ResponseWriter, req *http.Request) {
	var body restorePointDeleteRequest
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	ids := append([]string{}, body.RestorePointIDs...)
	if body.RestorePointID != "" {
		ids = append(ids, body.RestorePointID)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "restore_point_id_required"})
		return
	}

	pointsByCluster := map[string][]store.RestorePoint{}
	for _, id := range ids {
		point, ok, err := r.store.GetRestorePoint(id)
		if err != nil {
			r.logger.Error("failed to get restore point for delete", "restore_point_id", id, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_restore_point_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found", "restorePointId": id})
			return
		}
		if !tenantVisible(req, point.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found", "restorePointId": id})
			return
		}
		if point.Status == "deleted" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_already_deleted", "restorePointId": id})
			return
		}
		if point.VeleroBackupName == "" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_backup_name_required", "restorePointId": id})
			return
		}
		if state, _ := point.Metadata["retentionState"].(string); state == "deleting" || state == "pending_delete" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_delete_in_progress", "restorePointId": id})
			return
		}
		key := point.SourceClusterID + "::" + point.ProtectionPlanID
		pointsByCluster[key] = append(pointsByCluster[key], point)
	}

	tasks := make([]store.Task, 0, len(pointsByCluster))
	if err := r.store.ClaimRestorePointDeletion(ids); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_protected", "message": err.Error()})
		return
	}
	warnings := []string{}
	for _, points := range pointsByCluster {
		clusterID := points[0].SourceClusterID
		task, warning, err := r.createRestorePointDeleteTask(clusterID, points)
		if err != nil {
			r.logger.Error("failed to create restore point delete task", "cluster_id", clusterID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_delete_task_failed"})
			return
		}
		tasks = append(tasks, task)
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}

	statusCode := http.StatusAccepted
	response := restorePointDeleteResponse{Tasks: tasks}
	if len(tasks) == 1 {
		response.Task = &tasks[0]
	}
	if len(warnings) > 0 {
		response.Warning = strings.Join(warnings, "; ")
	}
	writeJSON(w, statusCode, response)
}

type restorePointRetentionRequest struct {
	Retained *bool `json:"retained"`
}

func (r *Router) setRestorePointRetention(w http.ResponseWriter, req *http.Request) {
	var body restorePointRetentionRequest
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if body.Retained == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "retained_required"})
		return
	}
	point, found, err := r.store.GetRestorePoint(req.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_restore_point_failed"})
		return
	}
	if !found || !tenantVisible(req, point.TenantID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found"})
		return
	}
	if err := r.store.SetRestorePointRetention(point.ID, point.TenantID, *body.Retained); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_locked", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"retained": *body.Retained})
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
