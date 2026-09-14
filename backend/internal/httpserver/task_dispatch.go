package httpserver

import (
	"context"
	"errors"
	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"strings"
	"time"
)

func (r *Router) waitForTaskSucceeded(ctx context.Context, taskID string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		task, ok, err := r.findTask(taskID)
		if err != nil {
			return err
		}
		if ok {
			switch strings.ToLower(task.Status) {
			case "succeeded", "completed", "success":
				return nil
			case "failed":
				message := task.ErrorMessage
				if message == "" {
					message = task.ErrorCode
				}
				if message == "" {
					message = "task failed"
				}
				return errors.New(message)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for storage sync task to complete")
		case <-ticker.C:
		}
	}
}

func (r *Router) findTask(taskID string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) redispatchPendingTasks(clusterID string, conn *websocket.Conn) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		r.logger.Error("failed to list tasks for redispatch", "cluster_id", clusterID, "error", err)
		return
	}
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "dispatched" {
			continue
		}
		if err := r.dispatchStoredTask(conn, task); err != nil {
			r.logger.Error("failed to redispatch task", "cluster_id", clusterID, "task_id", task.ID, "error", err)
			_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       task.ID,
				Status:       "queued",
				Progress:     task.Progress,
				ErrorCode:    "REDISPATCH_FAILED",
				ErrorMessage: err.Error(),
			})
			continue
		}
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:   task.ID,
			Status:   "dispatched",
			Progress: task.Progress,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "redispatched",
			Message: "task redispatched after agent reconnect",
		})
	}
}

func (r *Router) dispatchStoredTask(conn *websocket.Conn, task store.Task) error {
	r.taskDispatchMu.Lock()
	defer r.taskDispatchMu.Unlock()
	dispatch, err := r.buildStoredTaskDispatch(task)
	if err != nil {
		return err
	}
	return conn.WriteJSON(dispatch)
}

func (r *Router) dispatchControlPlaneHandover(ctx context.Context, clusterID, action, migrationID string, rollbackDeadline time.Time) error {
	if !supportedControlPlaneHandoverAction(action) {
		return errors.New("unsupported control-plane handover action")
	}
	if strings.TrimSpace(clusterID) == "" || strings.TrimSpace(migrationID) == "" {
		return errors.New("cluster ID and migration ID are required")
	}
	task, err := r.store.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "control-plane-handover", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{"action": action, "migrationId": migrationID, "rollbackDeadline": rollbackDeadline.Format(time.RFC3339Nano)}})
	if err != nil {
		return err
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		return errors.New("agent is not connected")
	}
	if err = r.dispatchStoredTask(conn, task); err != nil {
		return err
	}
	_, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "dispatched", Progress: 0})
	return err
}

func supportedControlPlaneHandoverAction(action string) bool {
	switch action {
	case "confirm", "commit", "rollback", "cleanup":
		return true
	default:
		return false
	}
}

func (r *Router) buildStoredTaskDispatch(task store.Task) (protocol.Message[protocol.TaskDispatchPayload], error) {
	commandID := task.CommandID
	if commandID == "" {
		commandID = store.NewPublicID()
	}
	payload := protocol.TaskDispatchPayload{
		TaskID:    task.ID,
		CommandID: commandID,
		Type:      task.Type,
		Deadline:  time.Now().UTC().Add(30 * time.Minute),
	}
	switch task.Type {
	case "storage-sync":
		repositoryID := stringPayload(task.Payload, "repositoryId")
		repo, ok, err := r.store.GetStorageRepository(repositoryID)
		if err != nil {
			return protocol.Message[protocol.TaskDispatchPayload]{}, err
		}
		if !ok {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("storage repository not found")
		}
		repo.Region = normalizedStorageRegion(repo.Type, repo.Region)
		name := stringPayload(task.Payload, "name")
		if name == "" {
			name = storageDomainBSLName(repo, stringPayload(task.Payload, "sourceClusterId"))
		}
		config := repo.Config
		if raw, ok := task.Payload["config"].(map[string]any); ok {
			config = raw
		}
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.StorageSync = &protocol.StorageSyncCommand{
			RepositoryID: repo.ID,
			Name:         name,
			Type:         repo.Type,
			Endpoint:     repo.Endpoint,
			Bucket:       repo.Bucket,
			Region:       repo.Region,
			TLSEnabled:   repo.TLSEnabled,
			SecretRef:    repo.SecretRef,
			Credentials:  storageCredentials(repo),
			Config:       config,
		}
	case "schedule-sync":
		command, err := r.scheduleSyncCommandFromPayload(task.Payload)
		if err != nil {
			return protocol.Message[protocol.TaskDispatchPayload]{}, err
		}
		payload.ScheduleSync = command
	case "backup":
		sourceNamespace := stringPayload(task.Payload, "sourceNamespace")
		sourceNamespaces := stringSlicePayload(task.Payload, "sourceNamespaces")
		if len(sourceNamespaces) == 0 && sourceNamespace != "" {
			sourceNamespaces = []string{sourceNamespace}
		}
		payload.Backup = &protocol.BackupCommand{
			PlanID:                  task.ProtectionPlanID,
			Trigger:                 firstNonEmptyString(stringPayload(task.Payload, "trigger"), "manual"),
			SourceClusterID:         task.ClusterID,
			SourceNamespace:         sourceNamespace,
			SourceNamespaces:        sourceNamespaces,
			VeleroBackupName:        stringPayload(task.Payload, "veleroBackupName"),
			Scope:                   stringPayload(task.Payload, "scope"),
			IncludedResources:       stringSlicePayload(task.Payload, "includedResources"),
			ResourceSelection:       protocolResourceSelection(resourceSelectionPayload(task.Payload)),
			LabelSelector:           protocolLabelSelector(labelSelectorPayload(task.Payload)),
			StorageRepo:             stringPayload(task.Payload, "storageRepo"),
			IncludeClusterResources: boolPayload(task.Payload, "includeClusterResources"),
			ExcludedResources:       stringSlicePayload(task.Payload, "excludedResources"),
			Hooks:                   protocol.HookSet{},
		}
	case "backup-cancel":
		targetTaskID := stringPayload(task.Payload, "targetTaskId")
		if targetTaskID == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("backup cancel target task id is required")
		}
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.BackupCancel = &protocol.BackupCancelCommand{
			PlanID:           firstNonEmptyString(stringPayload(task.Payload, "planId"), task.ProtectionPlanID),
			TargetTaskID:     targetTaskID,
			VeleroBackupName: stringPayload(task.Payload, "veleroBackupName"),
			Reason:           firstNonEmptyString(stringPayload(task.Payload, "reason"), "user_requested"),
		}
	case "retention-cleanup":
		command := retentionCleanupCommandFromPayload(task.Payload)
		if len(command.RestorePoints) == 0 {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("retention cleanup has no restore points")
		}
		payload.Deadline = time.Now().UTC().Add(30 * time.Minute)
		payload.RetentionCleanup = command
	case "protection-cleanup":
		command := protectionCleanupCommandFromPayload(task.Payload)
		if command.PlanID == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("protection cleanup plan id is required")
		}
		payload.Deadline = time.Now().UTC().Add(30 * time.Minute)
		payload.ProtectionCleanup = command
	case "agent-upgrade":
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.AgentUpgrade = &protocol.AgentUpgradeCommand{
			ClusterID:         stringPayload(task.Payload, "clusterId"),
			Namespace:         stringPayload(task.Payload, "namespace"),
			Image:             stringPayload(task.Payload, "image"),
			Version:           stringPayload(task.Payload, "version"),
			ExpectedDigest:    stringPayload(task.Payload, "expectedDigest"),
			DeploymentName:    stringPayload(task.Payload, "deploymentName"),
			ContainerName:     stringPayload(task.Payload, "containerName"),
			RolloutAnnotation: stringPayload(task.Payload, "rolloutAnnotation"),
		}
		if payload.AgentUpgrade.ClusterID == "" {
			payload.AgentUpgrade.ClusterID = task.ClusterID
		}
		if payload.AgentUpgrade.Namespace == "" {
			payload.AgentUpgrade.Namespace = r.agentNamespaceForCluster(task.ClusterID)
		}
		if payload.AgentUpgrade.Image == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("agent upgrade image is required")
		}
	case "velero-upgrade":
		payload.Deadline = time.Now().UTC().Add(15 * time.Minute)
		payload.VeleroUpgrade = &protocol.VeleroUpgradeCommand{
			OADP: boolPayload(task.Payload, "oadp"), CatalogImage: stringPayload(task.Payload, "catalogImage"),
			OADPPackage: stringPayload(task.Payload, "oadpPackage"), OADPChannel: stringPayload(task.Payload, "oadpChannel"),
			OADPTargetCSV: stringPayload(task.Payload, "oadpTargetCsv"),
			ClusterID:     task.ClusterID, Namespace: firstNonEmptyString(stringPayload(task.Payload, "namespace"), r.dataProtectionNamespaceForCluster(task.ClusterID)),
			Image: stringPayload(task.Payload, "image"), Version: stringPayload(task.Payload, "version"), ExpectedDigest: stringPayload(task.Payload, "expectedDigest"),
			DeploymentName: stringPayload(task.Payload, "deploymentName"), DaemonSetName: stringPayload(task.Payload, "daemonSetName"),
			AWSPluginImage: stringPayload(task.Payload, "awsPluginImage"), AzurePluginImage: stringPayload(task.Payload, "azurePluginImage"), GCPPluginImage: stringPayload(task.Payload, "gcpPluginImage"),
			ConcurrentBackups: int(int64FromAny(task.Payload["concurrentBackups"])), NodeAgentConcurrency: int(int64FromAny(task.Payload["nodeAgentConcurrency"])),
			PrepareQueueLength: int(int64FromAny(task.Payload["prepareQueueLength"])), CacheStorageClass: stringPayload(task.Payload, "cacheStorageClass"),
			CacheResidentThresholdMB: int(int64FromAny(task.Payload["cacheResidentThresholdMB"])),
			CacheLimitMB:             int(int64FromAny(task.Payload["cacheLimitMB"])),
			CRDsURL:                  stringPayload(task.Payload, "crdsUrl"),
		}
		if payload.VeleroUpgrade.Image == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("velero upgrade image is required")
		}
	case "restore", "drill", "takeover":
		sourceNamespace := stringPayload(task.Payload, "sourceNamespace")
		sourceNamespaces := stringSlicePayload(task.Payload, "sourceNamespaces")
		if len(sourceNamespaces) == 0 && sourceNamespace != "" {
			sourceNamespaces = []string{sourceNamespace}
		}
		payload.Restore = &protocol.RestoreCommand{
			RestorePointID:             stringPayload(task.Payload, "restorePointId"),
			VeleroBackupName:           stringPayload(task.Payload, "veleroBackupName"),
			StorageRepo:                stringPayload(task.Payload, "storageRepo"),
			SourceNamespace:            sourceNamespace,
			SourceNamespaces:           sourceNamespaces,
			TargetNamespace:            stringPayload(task.Payload, "targetNamespace"),
			TargetNamespaces:           stringMapPayload(task.Payload, "targetNamespaces"),
			TargetMode:                 stringPayload(task.Payload, "targetMode"),
			RestoreMode:                stringPayload(task.Payload, "restoreMode"),
			ArtifactMode:               stringPayload(task.Payload, "artifactMode"),
			ConflictPolicy:             stringPayload(task.Payload, "conflictPolicy"),
			IncludeClusterScoped:       boolPayload(task.Payload, "includeClusterScoped"),
			UseTransforms:              boolPayload(task.Payload, "useTransforms"),
			TransformPreset:            stringPayload(task.Payload, "transformPreset"),
			StorageProfileMode:         stringPayload(task.Payload, "storageProfileMode"),
			AlternateProfileID:         stringPayload(task.Payload, "alternateProfileId"),
			IncludedResources:          stringSlicePayload(task.Payload, "includedResources"),
			ExcludedResources:          stringSlicePayload(task.Payload, "excludedResources"),
			StorageClassMappings:       stringMapPayload(task.Payload, "storageClassMappings"),
			ImageMappings:              stringMapPayload(task.Payload, "imageMappings"),
			ServiceNodePortMappings:    intMapPayload(task.Payload, "serviceNodePortMappings"),
			WaitForWorkloads:           boolPayload(task.Payload, "waitForWorkloads"),
			RunValidation:              boolPayload(task.Payload, "runValidation"),
			ForceStart:                 boolPayload(task.Payload, "forceStart"),
			ContentCatalogLoaded:       boolPayload(task.Payload, "contentCatalogLoaded"),
			PersistentDataExpected:     boolPayload(task.Payload, "persistentDataExpected"),
			ReadinessExpectationsKnown: boolPayload(task.Payload, "readinessExpectationsKnown"),
			RuntimeWorkloadsExpected:   boolPayload(task.Payload, "runtimeWorkloadsExpected"),
			ExpectedPVCs:               stringSlicePayload(task.Payload, "expectedPvcs"),
		}
		if !payload.Restore.ReadinessExpectationsKnown && task.RestorePointID != "" {
			if point, ok, err := r.store.GetRestorePoint(task.RestorePointID); err == nil && ok {
				if index, ready := restorePointContentIndex(point); ready && index.Status == "ready" && !index.Truncated {
					payload.Restore.ReadinessExpectationsKnown = true
					payload.Restore.RuntimeWorkloadsExpected, payload.Restore.ExpectedPVCs = readinessExpectationsFromCatalog(index.Resources, sourceNamespaces)
					payload.Restore.ContentCatalogLoaded = true
				}
			}
		}
	case "unregister":
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.Unregister = &protocol.UnregisterCommand{
			ClusterID:       stringPayload(task.Payload, "clusterId"),
			Namespace:       stringPayload(task.Payload, "namespace"),
			DeleteVelero:    boolPayload(task.Payload, "deleteVelero"),
			DeleteNamespace: boolPayload(task.Payload, "deleteNamespace"),
			Reason:          stringPayload(task.Payload, "reason"),
		}
		if payload.Unregister.ClusterID == "" {
			payload.Unregister.ClusterID = task.ClusterID
		}
		if payload.Unregister.Namespace == "" {
			payload.Unregister.Namespace = r.agentNamespaceForCluster(task.ClusterID)
		}
	case "control-plane-handover":
		rollbackDeadline, err := time.Parse(time.RFC3339Nano, stringPayload(task.Payload, "rollbackDeadline"))
		if err != nil {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("control-plane handover rollback deadline is invalid")
		}
		payload.Deadline = rollbackDeadline
		payload.ControlPlaneHandover = &protocol.ControlPlaneHandoverCommand{
			Action:             stringPayload(task.Payload, "action"),
			MigrationID:        stringPayload(task.Payload, "migrationId"),
			TargetEndpoint:     stringPayload(task.Payload, "targetEndpoint"),
			TargetInstallToken: stringPayload(task.Payload, "targetInstallToken"),
			RollbackDeadline:   rollbackDeadline,
		}
		if payload.ControlPlaneHandover.Action == "" || payload.ControlPlaneHandover.MigrationID == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("control-plane handover action and migration ID are required")
		}
	default:
		return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("unsupported task type for redispatch")
	}
	return protocol.Message[protocol.TaskDispatchPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformTaskDispatch,
		ClusterID:   task.ClusterID,
		Timestamp:   time.Now().UTC(),
		Payload:     payload,
	}, nil
}
