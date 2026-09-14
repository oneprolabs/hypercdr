package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strconv"
	"strings"
)

func (r *Router) listRestorePoints(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	filter := store.RestorePointFilter{
		ClusterID:        req.URL.Query().Get("clusterId"),
		AppID:            req.URL.Query().Get("appId"),
		ProtectionPlanID: req.URL.Query().Get("protectionPlanId"),
		Summary:          query.Get("view") == "summary",
	}
	if user, ok := requestUser(req); ok && !user.SystemAdmin {
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
	items = enrichRestorePointStorageIncrements(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) deleteRestorePoints(w http.ResponseWriter, req *http.Request) {
	var body struct {
		RestorePointIDs []string `json:"restorePointIds"`
		RestorePointID  string   `json:"restorePointId"`
	}
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
		pointsByCluster[point.SourceClusterID] = append(pointsByCluster[point.SourceClusterID], point)
	}

	tasks := make([]store.Task, 0, len(pointsByCluster))
	warnings := []string{}
	for clusterID, points := range pointsByCluster {
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
	response := map[string]any{"tasks": tasks}
	if len(tasks) == 1 {
		response["task"] = tasks[0]
	}
	if len(warnings) > 0 {
		response["warning"] = strings.Join(warnings, "; ")
	}
	writeJSON(w, statusCode, response)
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
