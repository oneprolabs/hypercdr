package httpserver

import (
	"errors"
	"net/http"

	"hypercdr-platform/platform/backend/internal/store"
)

type clusterSyncSettingsResponse struct {
	Settings store.ClusterSyncSettings `json:"settings"`
	Phase    string                    `json:"phase"`
	Task     *store.Task               `json:"task"`
}

func (r *Router) clusterSyncSettings(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_clusters_failed"})
		return
	}
	var cluster *store.Cluster
	for index := range clusters {
		if clusters[index].ID == clusterID && tenantVisible(req, clusters[index].TenantID) {
			cluster = &clusters[index]
			break
		}
	}
	if cluster == nil {
		writeJSON(w, 404, map[string]any{"error": "cluster_not_found"})
		return
	}
	if req.Method == http.MethodPut {
		var input store.ClusterSyncSettings
		if err := decodeJSON(req, &input); err != nil {
			writeJSON(w, 400, map[string]any{"error": "invalid_sync_settings"})
			return
		}
		if err := input.Validate(); err != nil {
			writeJSON(w, 400, map[string]any{"error": "invalid_sync_settings", "message": err.Error()})
			return
		}
		if cluster.ClusterType == "openshift" {
			writeJSON(w, 409, map[string]any{"error": "managed_runtime_required", "message": "Sync setting changes for OADP-managed clusters are not supported yet."})
			return
		}
		if err := r.store.SaveClusterSyncSettings(clusterID, input); err != nil {
			writeJSON(w, 500, map[string]any{"error": "save_sync_settings_failed"})
			return
		}
	}
	settings, err := r.store.GetClusterSyncSettings(clusterID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "sync_settings_failed"})
		return
	}
	response := clusterSyncSettingsResponse{Settings: settings, Phase: "pending"}
	if settings.AppliedMaxSyncTasks == settings.MaxSyncTasks && settings.AppliedNodeTransferConcurrency == settings.NodeTransferConcurrency {
		response.Phase = "applied"
	}
	if settings.ApplyTaskID != "" {
		if task, found, err := r.store.GetTask(settings.ApplyTaskID); err == nil && found {
			response.Task = &task
			if !isTerminalTaskStatus(task.Status) {
				response.Phase = "applying"
			} else if task.Status != "succeeded" {
				response.Phase = "failed"
			}
		}
	}
	writeJSON(w, 200, response)
}

func (r *Router) reconcileClusterSyncSettings() {
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("list clusters for sync settings", "error", err)
		return
	}
	for _, cluster := range clusters {
		conn, online := r.hub.get(cluster.ID)
		if !online || cluster.ClusterType == "openshift" {
			continue
		}
		settings, err := r.store.GetClusterSyncSettings(cluster.ID)
		if err != nil {
			continue
		}
		if !settings.Configured {
			continue
		}
		if settings.AppliedMaxSyncTasks == settings.MaxSyncTasks && settings.AppliedNodeTransferConcurrency == settings.NodeTransferConcurrency {
			continue
		}
		if settings.ApplyTaskID != "" {
			task, found, err := r.store.GetTask(settings.ApplyTaskID)
			if err != nil || (found && task.Status != "succeeded") {
				continue
			}
		}
		if err := r.store.EnsureClusterSyncSettings(cluster.ID); err != nil {
			continue
		}
		if cluster.VeleroImage == "" {
			continue
		}
		task, err := r.store.CreateTask(store.TaskInput{TenantID: cluster.TenantID, ClusterID: cluster.ID, Type: "velero-upgrade", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{
			"syncSettingsApply": true, "namespace": r.dataProtectionNamespaceForCluster(cluster.ID), "image": cluster.VeleroImage, "version": cluster.VeleroVersion,
			"deploymentName": "velero", "daemonSetName": "node-agent", "concurrentBackups": settings.MaxSyncTasks, "nodeAgentConcurrency": settings.NodeTransferConcurrency, "prepareQueueLength": 4,
		}})
		if err != nil {
			if !errors.Is(err, store.ErrCacheConfigurationBusy) {
				r.logger.Error("create sync settings apply task", "cluster_id", cluster.ID, "error", err)
			}
			continue
		}
		if err := r.dispatchStoredTask(conn, task); err != nil {
			r.logger.Error("dispatch sync settings apply task", "task_id", task.ID, "error", err)
			continue
		}
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "dispatched"})
	}
}
