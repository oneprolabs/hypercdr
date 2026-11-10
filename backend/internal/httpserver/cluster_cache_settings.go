package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
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
	for index := range tasks {
		if tasks[index].Type == "cache-settings" && (latest == nil || tasks[index].CreatedAt.After(latest.CreatedAt)) {
			latest = &tasks[index]
		}
	}
	if req.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"settings": settings, "observed": observed, "task": latest})
		return
	}
	clean := req.Method == http.MethodPost
	if !clean {
		if err := decodeJSON(req, &settings); err != nil || settings.RootPath == "" || settings.IndexMaxGiB < 1 || settings.IndexMaxGiB > 1024 || settings.DrillMaxGiB < 1 || settings.DrillMaxGiB > 4096 {
			writeJSON(w, 400, map[string]any{"error": "invalid_cache_settings", "message": "Enter a cache path and valid cache sizes."})
			return
		}
	}
	for _, task := range tasks {
		if task.Type == "cache-settings" && !isTerminalTaskStatus(task.Status) {
			writeJSON(w, 409, map[string]any{"error": "cache_task_active"})
			return
		}
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		writeJSON(w, 409, map[string]any{"error": "agent_offline"})
		return
	}
	task, err := r.store.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "cache-settings", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{"rootPath": settings.RootPath, "indexPath": settings.IndexPath, "drillPath": settings.DrillPath, "indexEnabled": settings.IndexEnabled, "indexMaxGiB": settings.IndexMaxGiB, "drillEnabled": settings.DrillEnabled, "drillMaxGiB": settings.DrillMaxGiB, "clean": clean, "namespace": r.dataProtectionNamespaceForCluster(clusterID), "requestedBy": requestActor(req)}})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "create_task_failed"})
		return
	}
	if !clean {
		if err := r.store.SaveClusterCacheSettings(clusterID, settings); err != nil {
			_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "failed", ErrorCode: "CACHE_SETTINGS_SAVE_FAILED", ErrorMessage: err.Error()})
			writeJSON(w, 500, map[string]any{"error": "cache_settings_save_failed"})
			return
		}
	}
	writeJSON(w, 202, map[string]any{"settings": settings, "task": task})
	r.startWorker(func() { r.dispatchComponentUpgrade(conn, task, "Cache settings dispatched to cluster agent") })
}
