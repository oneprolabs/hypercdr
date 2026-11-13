package httpserver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func (r *Router) clusterCacheSettings(w http.ResponseWriter, req *http.Request) {
	r.cacheSettingsMu.Lock()
	defer r.cacheSettingsMu.Unlock()
	clusterID := req.PathValue("id")
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_clusters_failed"})
		return
	}
	found := false
	var observed map[string]any
	for _, cluster := range clusters {
		if cluster.ID == clusterID && tenantVisible(req, cluster.TenantID) {
			found = true
			if cluster.ConnectionStatus == "online" && !cluster.LastSeenAt.IsZero() && time.Since(cluster.LastSeenAt) < 90*time.Second {
				observed = cluster.CacheState
			}
			break
		}
	}
	if !found {
		writeJSON(w, 404, map[string]any{"error": "cluster_not_found"})
		return
	}
	if req.Method == http.MethodPut {
		if task, ok, err := r.activeCacheUserTask(clusterID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "cache_task_check_failed"})
			return
		} else if ok {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cache_in_use", "message": "A sync or Drill task is running on this cluster. Finish it before changing cache settings.", "taskId": task.ID})
			return
		}
	}
	settings, err := r.store.GetClusterCacheSettings(clusterID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "cache_settings_failed"})
		return
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_tasks_failed"})
		return
	}
	var latest *store.Task
	var latestCleanup *store.Task
	for index := range tasks {
		if tasks[index].Type == "cache-settings" && (latest == nil || tasks[index].CreatedAt.After(latest.CreatedAt)) {
			latest = &tasks[index]
		}
		if tasks[index].Type == "cache-cleanup" && (latestCleanup == nil || tasks[index].CreatedAt.After(latestCleanup.CreatedAt)) {
			latestCleanup = &tasks[index]
		}
	}
	if req.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"settings": settings, "observed": observed, "task": latest})
		return
	}
	previousSettings := settings
	if observed["phase"] == "applied" && cacheObservationNewerThanTask(observed, latest) {
		raw, marshalErr := json.Marshal(observed)
		var actual store.ClusterCacheSettings
		if marshalErr != nil || json.Unmarshal(raw, &actual) != nil || actual.RootPath == "" || actual.IndexPath == "" || actual.DrillPath == "" || actual.IndexMaxGiB < 1 || actual.DrillMaxGiB < 1 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cache_observation_incomplete"})
			return
		}
		previousSettings = actual
	}
	clean := req.Method == http.MethodPost
	var retryPaths []string
	var retryTaskID string
	if clean && req.ContentLength != 0 {
		var input struct {
			TaskID string `json:"taskId"`
		}
		if err := decodeJSON(req, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_cleanup_retry"})
			return
		}
		retryTaskID = input.TaskID
		if retryTaskID != "" {
			latestCleanup = nil
			for index := range tasks {
				if tasks[index].ID == retryTaskID && tasks[index].Type == "cache-cleanup" {
					latestCleanup = &tasks[index]
					break
				}
			}
			if latestCleanup == nil {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "cleanup_task_not_found"})
				return
			}
			if latestCleanup.Status != "failed" {
				writeJSON(w, http.StatusConflict, map[string]any{"error": "cleanup_task_not_failed"})
				return
			}
		}
	}
	if clean && latestCleanup != nil {
		retryPaths = stringSlicePayload(latestCleanup.Payload, "cleanupPaths")
	}
	if !clean {
		if err := decodeJSON(req, &settings); err != nil || settings.RootPath == "" || settings.IndexMaxGiB < 1 || settings.IndexMaxGiB > 1024 || settings.DrillMaxGiB < 1 || settings.DrillMaxGiB > 4096 {
			writeJSON(w, 400, map[string]any{"error": "invalid_cache_settings", "message": "Enter a cache path and valid cache sizes."})
			return
		}
	}
	for _, task := range tasks {
		if (task.Type == "cache-settings" || task.Type == "cache-cleanup") && !isTerminalTaskStatus(task.Status) {
			if task.Type == "cache-cleanup" && task.Payload["transition"] != nil && !clean {
				continue
			}
			writeJSON(w, 409, map[string]any{"error": "cache_task_active"})
			return
		}
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		writeJSON(w, 409, map[string]any{"error": "agent_offline"})
		return
	}
	cleanupPaths := cacheCleanupPaths(previousSettings, settings)
	if len(retryPaths) > 0 {
		cleanupPaths = retryPaths
	}
	cleanupOnly := clean
	if cleanupOnly {
		if len(cleanupPaths) == 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cleanup_paths_missing"})
			return
		}
		var retryTransition any
		if latestCleanup != nil {
			retryTransition = latestCleanup.Payload["transition"]
		}
		if retryTransition == nil && !cacheCleanupPathsInactive(cleanupPaths, settings) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cleanup_path_in_use", "message": "The selected directory is currently enabled for caching."})
			return
		}
		cleanupTask, createErr := r.store.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "cache-cleanup", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{"cleanupPaths": cleanupPaths, "transition": retryTransition, "retryOfTaskId": retryTaskID, "namespace": r.dataProtectionNamespaceForCluster(clusterID), "requestedBy": requestActor(req)}})
		if createErr != nil {
			writeJSON(w, 500, map[string]any{"error": "create_cleanup_task_failed"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"task": cleanupTask})
		r.startWorker(func() {
			r.dispatchComponentUpgrade(conn, cleanupTask, "Cache cleanup task dispatched to cluster agent")
		})
		return
	}
	var transition *protocol.CacheSettingsTransition
	if len(cleanupPaths) > 0 {
		if !cacheCleanupPathsInactive(cleanupPaths, settings) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cache_paths_overlap", "message": "The old cache directory overlaps an enabled cache directory. Choose independent cache directories."})
			return
		}
		nodes := stringSlicePayload(observed, "nodes")
		if len(nodes) == 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cache_nodes_unavailable", "message": "Cache node information is unavailable. Please retry after the agent reports its nodes."})
			return
		}
		previousRaw, _ := json.Marshal(previousSettings)
		var previous protocol.CacheConfigurationSnapshot
		if err := json.Unmarshal(previousRaw, &previous); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "cache_snapshot_failed"})
			return
		}
		transition = &protocol.CacheSettingsTransition{IsolationID: fmt.Sprintf("%x", sha256.Sum256([]byte(store.NewPublicID()))), Nodes: nodes, Paths: cleanupPaths, Previous: previous}
	}
	settingsTask, err := r.store.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "cache-settings", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{"rootPath": settings.RootPath, "indexPath": settings.IndexPath, "drillPath": settings.DrillPath, "indexEnabled": settings.IndexEnabled, "indexMaxGiB": settings.IndexMaxGiB, "drillEnabled": settings.DrillEnabled, "drillMaxGiB": settings.DrillMaxGiB, "clean": false, "cleanupPaths": cleanupPaths, "transition": transition, "namespace": r.dataProtectionNamespaceForCluster(clusterID), "requestedBy": requestActor(req)}})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "create_task_failed"})
		return
	}
	if !cleanupOnly {
		if err := r.store.SaveClusterCacheSettings(clusterID, settings); err != nil {
			_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: settingsTask.ID, Status: "failed", ErrorCode: "CACHE_SETTINGS_SAVE_FAILED", ErrorMessage: err.Error()})
			writeJSON(w, 500, map[string]any{"error": "cache_settings_save_failed"})
			return
		}
	}
	writeJSON(w, 202, map[string]any{"settings": settings, "task": settingsTask})
	r.startWorker(func() { r.dispatchComponentUpgrade(conn, settingsTask, "Cache settings dispatched to cluster agent") })
}

func cacheObservationNewerThanTask(observed map[string]any, task *store.Task) bool {
	if task == nil || task.Status != "succeeded" {
		return true
	}
	observedAt, err := time.Parse(time.RFC3339Nano, stringPayload(observed, "observedAt"))
	return err == nil && observedAt.After(task.CompletedAt)
}

func cacheCleanupPathsInactive(paths []string, settings store.ClusterCacheSettings) bool {
	for _, path := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
			return false
		}
		for _, cache := range []struct {
			enabled bool
			path    string
		}{{settings.IndexEnabled, settings.IndexPath}, {settings.DrillEnabled, settings.DrillPath}} {
			if !cache.enabled || cache.path == "" {
				continue
			}
			activePath := filepath.Clean(cache.path)
			if path == activePath || strings.HasPrefix(path, activePath+"/") || strings.HasPrefix(activePath, path+"/") {
				return false
			}
		}
	}
	return true
}

func (r *Router) activeCacheUserTask(clusterID string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	blocked := map[string]bool{"backup": true, "storage-sync": true, "schedule-sync": true, "restore": true, "drill": true, "takeover": true, "failback": true}
	for _, task := range tasks {
		if blocked[task.Type] && !isTerminalTaskStatus(task.Status) {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) cacheSettingsTaskActive(clusterID string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.Type == "cache-settings" && !isTerminalTaskStatus(task.Status) {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func cacheCleanupPaths(previous, requested store.ClusterCacheSettings) []string {
	paths := make([]string, 0, 2)
	if previous.IndexPath != "" && (previous.IndexPath != requested.IndexPath || (previous.IndexEnabled && !requested.IndexEnabled)) {
		paths = append(paths, previous.IndexPath)
	}
	if previous.DrillPath != "" && (previous.DrillPath != requested.DrillPath || (previous.DrillEnabled && !requested.DrillEnabled)) {
		paths = append(paths, previous.DrillPath)
	}
	independent := make([]string, 0, len(paths))
	for index, path := range paths {
		covered := false
		for otherIndex, other := range paths {
			if index != otherIndex && (strings.HasPrefix(path, other+"/") || (path == other && otherIndex < index)) {
				covered = true
				break
			}
		}
		if !covered {
			independent = append(independent, path)
		}
	}
	return independent
}
