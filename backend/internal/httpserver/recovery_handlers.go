package httpserver

import (
	"context"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func (r *Router) createRestoreTask(w http.ResponseWriter, req *http.Request) {
	r.createRecoveryTask(w, req, "restore")
}

func (r *Router) createDrillTask(w http.ResponseWriter, req *http.Request) {
	r.createRecoveryTask(w, req, "drill")
}

func (r *Router) createTakeoverTask(w http.ResponseWriter, req *http.Request) {
	r.createRecoveryTask(w, req, "takeover")
}

func (r *Router) createRecoveryTask(w http.ResponseWriter, req *http.Request, taskType string) {
	var body struct {
		ClusterID                  string            `json:"clusterId"`
		ProtectionPlanID           string            `json:"protectionPlanId"`
		RestorePointID             string            `json:"restorePointId"`
		VeleroBackupName           string            `json:"veleroBackupName"`
		StorageRepo                string            `json:"storageRepo"`
		SourceNamespace            string            `json:"sourceNamespace"`
		SourceNamespaces           []string          `json:"sourceNamespaces"`
		TargetNamespace            string            `json:"targetNamespace"`
		TargetNamespaces           map[string]string `json:"targetNamespaces"`
		NamespaceMode              string            `json:"namespaceMode"`
		TargetMode                 string            `json:"targetMode"`
		RestoreMode                string            `json:"restoreMode"`
		ArtifactMode               string            `json:"artifactMode"`
		ConflictPolicy             string            `json:"conflictPolicy"`
		OriginalNamespaceConfirmed bool              `json:"originalNamespaceConfirmed"`
		IncludeClusterScoped       bool              `json:"includeClusterScoped"`
		UseTransforms              bool              `json:"useTransforms"`
		TransformPreset            string            `json:"transformPreset"`
		StorageProfileMode         string            `json:"storageProfileMode"`
		AlternateProfileID         string            `json:"alternateProfileId"`
		IncludedResources          []string          `json:"includedResources"`
		ExcludedResources          []string          `json:"excludedResources"`
		StorageClassMappings       map[string]string `json:"storageClassMappings"`
		ImageMappings              map[string]string `json:"imageMappings"`
		ServiceNodePortMappings    map[string]int    `json:"serviceNodePortMappings"`
		WaitForWorkloads           *bool             `json:"waitForWorkloads"`
		RunValidation              *bool             `json:"runValidation"`
		ForceStart                 bool              `json:"forceStart"`
		ContentCatalogLoaded       bool              `json:"contentCatalogLoaded"`
		PersistentDataExpected     bool              `json:"persistentDataExpected"`
		ReadinessExpectationsKnown bool              `json:"readinessExpectationsKnown"`
		RuntimeWorkloadsExpected   bool              `json:"runtimeWorkloadsExpected"`
		ExpectedPVCs               []string          `json:"expectedPvcs"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if body.ClusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if _, authenticated := requestUser(req); authenticated {
		if !r.clusterVisible(req, body.ClusterID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
			return
		}
	}
	storageRepoID := ""
	storageSourceClusterID := ""
	protectionPlanID := body.ProtectionPlanID
	var recoveryPlan store.ProtectionPlan
	var recoveryAppID string
	var recoveryStorageClasses []string
	if protectionPlanID != "" {
		plan, found, err := r.store.GetProtectionPlan(protectionPlanID)
		if err != nil {
			r.logger.Error("failed to get protection plan for recovery", "protection_plan_id", protectionPlanID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
			return
		}
		if !found || !tenantVisible(req, plan.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
			return
		}
		recoveryPlan = plan
		recoveryAppID = plan.AppID
	}
	if body.RestorePointID != "" {
		point, ok, err := r.store.GetRestorePoint(body.RestorePointID)
		if err != nil {
			r.logger.Error("failed to get restore point", "restore_point_id", body.RestorePointID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_restore_point_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found"})
			return
		}
		if !tenantVisible(req, point.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found"})
			return
		}
		if !strings.EqualFold(strings.TrimSpace(point.Status), "available") {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "restore_point_not_available",
				"message": "The selected restore point is no longer available. Refresh restore points and select an available recovery point.",
				"status":  point.Status,
			})
			return
		}
		if protectionPlanID != "" && point.ProtectionPlanID != "" && protectionPlanID != point.ProtectionPlanID {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_plan_mismatch", "message": "The selected restore point does not belong to the selected protection plan."})
			return
		}
		protectionPlanID = point.ProtectionPlanID
		if recoveryPlan.ID == "" && protectionPlanID != "" {
			plan, found, err := r.store.GetProtectionPlan(protectionPlanID)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
				return
			}
			if !found || !tenantVisible(req, plan.TenantID) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
				return
			}
			recoveryPlan, recoveryAppID = plan, plan.AppID
		}
		if point.AppID != "" {
			recoveryAppID = point.AppID
		}
		if body.VeleroBackupName == "" {
			body.VeleroBackupName = point.VeleroBackupName
		}
		if body.StorageRepo == "" {
			body.StorageRepo = point.BackupStorageName
		}
		storageRepoID = point.StorageRepoID
		storageSourceClusterID = point.SourceClusterID
		if storageRepoID != "" {
			if repo, ok, err := r.store.GetStorageRepository(storageRepoID); err == nil && ok {
				body.StorageRepo = storageDomainBSLName(repo, point.SourceClusterID)
			} else if err != nil {
				r.logger.Warn("failed to load restore point storage repository", "restore_point_id", point.ID, "repository_id", storageRepoID, "error", err)
			}
		}
		if body.SourceNamespace == "" {
			body.SourceNamespace = point.SourceNamespace
		}
		if len(body.SourceNamespaces) == 0 {
			body.SourceNamespaces = stringArrayFromAny(point.Metadata["includedNamespaces"])
		}
		if index, ready := restorePointContentIndex(point); ready && index.Status == "ready" && !index.Truncated {
			body.ReadinessExpectationsKnown = true
			body.RuntimeWorkloadsExpected, body.ExpectedPVCs = readinessExpectationsFromCatalog(index.Resources, body.SourceNamespaces)
			body.ContentCatalogLoaded = true
			recoveryStorageClasses = storageClassesFromCatalog(index.Resources, body.SourceNamespaces)
		}
		if body.TargetNamespace == "" {
			body.TargetNamespace = point.SourceNamespace
		}
	}
	if len(body.SourceNamespaces) == 0 && body.SourceNamespace != "" {
		body.SourceNamespaces = []string{body.SourceNamespace}
	}
	recoverySourceClusterID := recoveryPlan.SourceClusterID
	if recoverySourceClusterID == "" {
		recoverySourceClusterID = storageSourceClusterID
	}
	if recoverySourceClusterID != "" && !r.clustersDRCompatible(recoverySourceClusterID, body.ClusterID) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           "cluster_type_incompatible",
			"message":         "The selected target cluster type is incompatible with the recovery point source cluster. OpenShift requires an OpenShift target; Native Kubernetes and Huawei Cloud CCE can target each other.",
			"sourceClusterId": recoverySourceClusterID, "targetClusterId": body.ClusterID,
		})
		return
	}
	if body.VeleroBackupName == "" || body.SourceNamespace == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "velero_backup_name_and_source_namespace_required"})
		return
	}
	if body.TargetNamespace == "" {
		body.TargetNamespace = body.SourceNamespace
	}
	if body.RestoreMode == "" {
		body.RestoreMode = taskType
	}
	if body.RestoreMode != "full" && body.RestoreMode != "dataOnly" && body.RestoreMode != "restore" && body.RestoreMode != "drill" && body.RestoreMode != "takeover" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_restore_mode"})
		return
	}
	if body.ConflictPolicy == "" {
		body.ConflictPolicy = "none"
	}
	waitForWorkloads := body.WaitForWorkloads == nil || *body.WaitForWorkloads
	runValidation := body.RunValidation == nil || *body.RunValidation
	if body.RestoreMode == "dataOnly" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "data_only_restore_not_enabled"})
		return
	}
	if body.TargetNamespace == body.SourceNamespace && !body.OriginalNamespaceConfirmed {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "original_namespace_confirmation_required"})
		return
	}
	if body.TargetMode == "" {
		body.TargetMode = "same-namespace"
	}
	body.ConflictPolicy = recoveryConflictPolicy(taskType, body.NamespaceMode, body.SourceNamespace, body.TargetNamespace, body.ConflictPolicy)
	if taskType == "drill" {
		if err := r.validateServiceNodePortMappings(body.ClusterID, body.TargetNamespace, body.ServiceNodePortMappings); err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "service_nodeport_conflict", "message": err.Error()})
			return
		}
		activeTask, found, err := r.findActiveRecoveryTask("drill", body.ClusterID, body.SourceNamespace)
		if err != nil {
			r.logger.Error("failed to check active drill tasks", "cluster_id", body.ClusterID, "source_namespace", body.SourceNamespace, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "active_drill_check_failed"})
			return
		}
		if found {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "active_drill_task_exists",
				"message": "A drill task is already running for this application. Wait for it to finish before starting another drill.",
				"taskId":  activeTask.ID,
				"status":  activeTask.Status,
			})
			return
		}
	}
	if protectionPlanID == "" || body.RestorePointID == "" || recoveryAppID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "recovery_relationship_incomplete", "message": "A recovery task requires a protection plan, application, and restore point."})
		return
	}
	if recoveryPlan.ID != "" && recoveryPlan.AppID != "" && recoveryAppID != recoveryPlan.AppID && !slices.Contains(recoveryPlan.AppIDs, recoveryAppID) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_application_mismatch", "message": "The selected restore point does not belong to the selected protection plan application."})
		return
	}
	// Cross-cluster recovery cannot safely inherit the source StorageClass: a
	// target cluster may use a different provisioner (for example Longhorn vs
	// CCE csi-disk). Refuse an ambiguous request before creating a task so the
	// user can provide an explicit mapping in Advanced options instead of
	// receiving a misleading 2% stall later.
	if taskType == "drill" && recoveryPlan.SourceClusterID != "" && body.ClusterID != recoveryPlan.SourceClusterID && len(recoveryStorageClasses) > 0 {
		targetStorageClasses := map[string]struct{}{}
		if clusters, err := r.store.ListClusters(); err != nil {
			r.logger.Error("failed to load target StorageClasses for drill", "cluster_id", body.ClusterID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_target_storage_classes_failed"})
			return
		} else {
			for _, cluster := range clusters {
				if cluster.ID != body.ClusterID {
					continue
				}
				for _, storageClass := range cluster.StorageClasses {
					targetStorageClasses[storageClass.Name] = struct{}{}
				}
				break
			}
		}
		if body.StorageClassMappings == nil {
			body.StorageClassMappings = map[string]string{}
		}
		missingMappings := []string{}
		for _, sourceStorageClass := range recoveryStorageClasses {
			if strings.TrimSpace(body.StorageClassMappings[sourceStorageClass]) != "" {
				continue
			}
			if _, sameNameAvailable := targetStorageClasses[sourceStorageClass]; sameNameAvailable {
				body.StorageClassMappings[sourceStorageClass] = sourceStorageClass
				continue
			}
			missingMappings = append(missingMappings, sourceStorageClass)
		}
		if len(missingMappings) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":                 "storage_class_mapping_required",
				"message":               fmt.Sprintf("No same-name target StorageClass exists for %s. Open Advanced options and select a target StorageClass.", strings.Join(missingMappings, ", ")),
				"missingStorageClasses": missingMappings,
			})
			return
		}
	}
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        body.ClusterID,
		AppID:            recoveryAppID,
		ProtectionPlanID: protectionPlanID,
		RestorePointID:   body.RestorePointID,
		Type:             taskType,
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"requestedBy":                requestActor(req),
			"protectionPlanId":           protectionPlanID,
			"restorePointId":             body.RestorePointID,
			"veleroBackupName":           body.VeleroBackupName,
			"storageRepo":                body.StorageRepo,
			"sourceNamespace":            body.SourceNamespace,
			"sourceNamespaces":           body.SourceNamespaces,
			"targetNamespace":            body.TargetNamespace,
			"targetNamespaces":           body.TargetNamespaces,
			"namespaceMode":              body.NamespaceMode,
			"targetMode":                 body.TargetMode,
			"restoreMode":                body.RestoreMode,
			"artifactMode":               body.ArtifactMode,
			"conflictPolicy":             body.ConflictPolicy,
			"originalNamespaceConfirmed": body.OriginalNamespaceConfirmed,
			"includeClusterScoped":       body.IncludeClusterScoped,
			"useTransforms":              body.UseTransforms,
			"transformPreset":            body.TransformPreset,
			"storageProfileMode":         body.StorageProfileMode,
			"alternateProfileId":         body.AlternateProfileID,
			"includedResources":          body.IncludedResources,
			"excludedResources":          body.ExcludedResources,
			"storageClassMappings":       body.StorageClassMappings,
			"imageMappings":              body.ImageMappings,
			"serviceNodePortMappings":    body.ServiceNodePortMappings,
			"waitForWorkloads":           waitForWorkloads,
			"runValidation":              runValidation,
			"forceStart":                 body.ForceStart,
			"contentCatalogLoaded":       body.ContentCatalogLoaded,
			"persistentDataExpected":     body.PersistentDataExpected,
			"readinessExpectationsKnown": body.ReadinessExpectationsKnown,
			"runtimeWorkloadsExpected":   body.RuntimeWorkloadsExpected,
			"expectedPvcs":               body.ExpectedPVCs,
		},
	})
	if err != nil {
		r.logger.Error("failed to create recovery task", "task_type", taskType, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	go r.dispatchRecoveryTaskAfterStorageSync(task, body.StorageRepo, storageRepoID, storageSourceClusterID)
	writeJSON(w, http.StatusCreated, task)
}

var nodePortFieldPattern = regexp.MustCompile(`:(3[0-2][0-9]{3})/(?:TCP|UDP|SCTP)\b`)

func (r *Router) validateServiceNodePortMappings(clusterID, replacedNamespace string, mappings map[string]int) error {
	if len(mappings) == 0 {
		return nil
	}
	requested := map[int]string{}
	for key, port := range mappings {
		parts := strings.Split(key, "|")
		if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" {
			return fmt.Errorf("invalid Service port mapping %q", key)
		}
		servicePort, err := strconv.Atoi(parts[1])
		if err != nil || servicePort < 1 || servicePort > 65535 {
			return fmt.Errorf("invalid Service port in mapping %q", key)
		}
		protocol := strings.ToUpper(strings.TrimSpace(parts[2]))
		if protocol != "TCP" && protocol != "UDP" && protocol != "SCTP" {
			return fmt.Errorf("unsupported protocol %q for Service %s", parts[2], parts[0])
		}
		if port < 30000 || port > 32767 {
			return fmt.Errorf("NodePort %d for Service %s must be between 30000 and 32767", port, parts[0])
		}
		if previous := requested[port]; previous != "" {
			return fmt.Errorf("NodePort %d is requested by both %s and %s", port, previous, parts[0])
		}
		requested[port] = parts[0]
	}
	apps, err := r.store.ListApplications(clusterID)
	if err != nil {
		return fmt.Errorf("target cluster NodePort preflight failed: %w", err)
	}
	used := map[int]string{}
	for _, app := range apps {
		if app.Namespace == replacedNamespace {
			continue
		}
		collectNodePorts(app.ResourceSummary, app.Namespace, used)
	}
	for port, service := range requested {
		if owner := used[port]; owner != "" {
			return fmt.Errorf("NodePort %d requested for Service %s is already used by %s on the target cluster", port, service, owner)
		}
	}
	return nil
}

func collectNodePorts(value any, namespace string, result map[int]string) {
	switch typed := value.(type) {
	case map[string]any:
		name, _ := typed["name"].(string)
		ports := ""
		if fields, ok := typed["fields"].(map[string]any); ok {
			ports, _ = fields["PORT(S)"].(string)
		}
		if fields, ok := typed["fields"].(map[string]string); ok {
			ports = fields["PORT(S)"]
		}
		for _, match := range nodePortFieldPattern.FindAllStringSubmatch(ports, -1) {
			port, _ := strconv.Atoi(match[1])
			result[port] = namespace + "/" + firstNonEmptyString(name, "Service")
		}
		for _, child := range typed {
			collectNodePorts(child, namespace, result)
		}
	case []any:
		for _, child := range typed {
			collectNodePorts(child, namespace, result)
		}
	}
}

func recoveryConflictPolicy(taskType string, namespaceMode string, sourceNamespace string, targetNamespace string, requested string) string {
	if taskType != "drill" || targetNamespace == "" || targetNamespace == sourceNamespace {
		return requested
	}
	// A generated drill namespace is disposable sandbox state. Reusing it with
	// Velero's skip policy leaves objects from an earlier failed drill in place,
	// which also prevents resource/image modifiers from being applied on retry.
	// Legacy clients did not send namespaceMode, so recognize the conventional
	// generated name as well.
	if namespaceMode == "generated" || (namespaceMode == "" && targetNamespace == sourceNamespace+"-drill") {
		return "replace"
	}
	return requested
}

func (r *Router) findActiveRecoveryTask(taskType string, clusterID string, sourceNamespace string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.Type != taskType || !isActiveTaskStatus(task.Status) {
			continue
		}
		taskSourceNamespace := stringPayload(task.Payload, "sourceNamespace")
		if taskSourceNamespace == sourceNamespace {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) retryRecoveryTask(w http.ResponseWriter, req *http.Request) {
	original, ok, err := r.findTask(req.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_task_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "task_not_found"})
		return
	}
	if original.Type != "restore" && original.Type != "drill" && original.Type != "takeover" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_not_retriable"})
		return
	}
	if isActiveTaskStatus(original.Status) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "task_still_active"})
		return
	}
	payload := cloneStringAnyMap(original.Payload)
	payload["retryOfTaskId"] = original.ID
	payload["requestedBy"] = requestActor(req)
	task, err := r.store.CreateTask(store.TaskInput{ClusterID: original.ClusterID, AppID: original.AppID, ProtectionPlanID: original.ProtectionPlanID, RestorePointID: original.RestorePointID, Type: original.Type, Status: "queued", CommandID: store.NewPublicID(), Payload: payload})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "retry_task_create_failed"})
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "retry_created", Message: "Recovery retry created from task " + original.ID})
	go r.dispatchRecoveryTask(task)
	writeJSON(w, http.StatusCreated, task)
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+1)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func isActiveTaskStatus(status string) bool {
	switch strings.ToLower(status) {
	case "queued", "dispatched", "accepted", "running", "syncing", "finalizing", "canceling":
		return true
	default:
		return false
	}
}

func isCompletedTaskStatus(status string) bool {
	switch strings.ToLower(status) {
	case "succeeded", "completed", "success":
		return true
	default:
		return false
	}
}

func protectionPlanAllowsBackup(status string) bool {
	switch strings.ToLower(status) {
	case "active", "active_with_warning":
		return true
	default:
		return false
	}
}

func (r *Router) dispatchRecoveryTaskAfterStorageSync(task store.Task, storageName string, storageRepoID string, sourceClusterID string) {
	if storageRepoID == "" {
		r.dispatchRecoveryTask(task)
		return
	}
	if r.isStorageAlreadySynced(task.ClusterID, storageName, storageRepoID, sourceClusterID) {
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "storage_preflight_skipped",
			Message: "Storage location already configured on target cluster.",
		})
		r.dispatchRecoveryTask(task)
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "storage_preflight_started",
		Message: "Configuring storage...",
	})
	storageTask, err := r.ensureStorageSynced(context.Background(), task.ClusterID, storageName, storageRepoID, sourceClusterID)
	if err != nil {
		r.logger.Error("storage sync preflight failed before recovery dispatch", "cluster_id", task.ClusterID, "task_type", task.Type, "task_id", task.ID, "storage_repo", storageName, "storage_task_id", storageTask.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "failed",
			Progress:     0,
			ErrorCode:    "STORAGE_SYNC_FAILED",
			ErrorMessage: err.Error(),
			MarkDone:     true,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "error",
			Reason:  "storage_preflight_failed",
			Message: err.Error(),
			Payload: map[string]any{"storageTaskId": storageTask.ID},
		})
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "storage_preflight_succeeded",
		Message: recoveryDispatchMessage(task.Type),
		Payload: map[string]any{"storageTaskId": storageTask.ID},
	})
	r.dispatchRecoveryTask(task)
}

func (r *Router) dispatchRecoveryTask(task store.Task) {
	conn, ok := r.hub.get(task.ClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; recovery will be dispatched after reconnect",
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "warning",
			Reason:  "dispatch_waiting_agent",
			Message: recoveryDispatchMessage(task.Type),
		})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch recovery task after storage preflight", "task_type", task.Type, "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "warning",
			Reason:  "dispatch_failed",
			Message: recoveryDispatchMessage(task.Type),
		})
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: recoveryDispatchMessage(task.Type),
	})
}

func (r *Router) isStorageAlreadySynced(clusterID string, storageName string, repositoryID string, sourceClusterID string) bool {
	if repositoryID != "" {
		ready, _, err := r.clusterStorageBindingReady(clusterID, repositoryID, sourceClusterID)
		if err != nil {
			r.logger.Warn("failed to check cluster storage binding", "cluster_id", clusterID, "repository_id", repositoryID, "error", err)
			return false
		}
		return ready
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		r.logger.Warn("failed to check storage sync history", "cluster_id", clusterID, "storage_repo", storageName, "repository_id", repositoryID, "error", err)
		return false
	}
	for _, task := range tasks {
		if task.Type != "storage-sync" {
			continue
		}
		if !isCompletedTaskStatus(task.Status) {
			continue
		}
		matchesRepo := repositoryID != "" && taskPayloadString(task.Payload, "repositoryId") == repositoryID
		matchesName := storageName != "" && taskPayloadString(task.Payload, "name") == storageName
		if matchesRepo || matchesName {
			return true
		}
	}
	return false
}

func recoveryDispatchMessage(taskType string) string {
	switch taskType {
	case "drill":
		return "Dispatching drill task..."
	case "takeover":
		return "Dispatching takeover task..."
	default:
		return "Dispatching restore task..."
	}
}
