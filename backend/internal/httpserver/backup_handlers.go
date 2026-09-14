package httpserver

import (
	"context"
	"errors"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"slices"
)

func (r *Router) createBackupTask(w http.ResponseWriter, req *http.Request) {
	var body backupTaskRequest
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if body.ClusterID == "" || (body.SourceNamespace == "" && body.AppID == "" && body.ProtectionPlanID == "") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_and_target_required"})
		return
	}
	if body.Scope == "" {
		body.Scope = "all"
	}
	if body.StorageRepo == "" {
		body.StorageRepo = "default"
	}
	if body.Trigger == "" {
		body.Trigger = "manual"
	}
	body.RequestedBy = requestActor(req)

	// When a protection plan is provided, expand to one task per app on the plan.
	type planApp struct {
		appID                   string
		ns                      string
		storage                 string
		storageRepoID           string
		scope                   string
		includedResources       []string
		resourceSelection       store.ResourceSelection
		labelSelector           store.LabelSelector
		excludedResources       []string
		includeClusterResources bool
	}
	targets := []planApp{}
	seen := map[string]struct{}{}
	if body.ProtectionPlanID != "" {
		plan, ok, err := r.store.GetProtectionPlan(body.ProtectionPlanID)
		if err != nil {
			r.logger.Error("failed to load protection plan", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
			return
		}
		if !tenantVisible(req, plan.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
			return
		}
		if !protectionPlanAllowsBackup(plan.Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "protection_plan_not_active", "status": plan.Status})
			return
		}
		body.ClusterID = plan.SourceClusterID
		repo, hasRepo, _ := r.store.GetStorageRepository(plan.StorageRepoID)
		storageName := body.StorageRepo
		if hasRepo {
			storageName = storageDomainBSLName(repo, plan.SourceClusterID)
		}
		if body.AppID == "" && body.SourceNamespace == "" {
			sourceNamespaces := []string{}
			appIDs := plan.AppIDs
			if len(appIDs) == 0 && plan.AppID != "" {
				appIDs = []string{plan.AppID}
			}
			for _, appID := range appIDs {
				if appID == "" {
					continue
				}
				if _, dup := seen[appID]; dup {
					continue
				}
				app, ok, _ := r.store.GetApplication(appID)
				if !ok || app.Namespace == "" {
					continue
				}
				seen[appID] = struct{}{}
				if !slices.Contains(sourceNamespaces, app.Namespace) {
					sourceNamespaces = append(sourceNamespaces, app.Namespace)
				}
			}
			if len(sourceNamespaces) == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no_resolvable_applications"})
				return
			}
			body.ProtectionPlanID = plan.ID
			body.SourceNamespace = sourceNamespaces[0]
			body.SourceNamespaces = sourceNamespaces
			body.StorageRepo = storageName
			body.Scope = plan.ScopeType
			body.IncludedResources = plan.IncludedResources
			body.LabelSelector = plan.LabelSelector
			body.ExcludedResources = plan.ExcludedResources
			body.IncludeClusterResources = plan.IncludeClusterScoped
			if existing, ok, err := r.findActiveBackupTask(body.ClusterID, body.ProtectionPlanID, "", ""); err != nil {
				r.logger.Error("failed to check active plan backup task", "cluster_id", body.ClusterID, "protection_plan_id", body.ProtectionPlanID, "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "check_active_backup_failed"})
				return
			} else if ok {
				writeJSON(w, http.StatusOK, map[string]any{"task": existing, "warning": "Sync is already running.", "reused": true})
				return
			}
			task, err := r.createPendingBackupTask(body, "")
			if err != nil {
				r.logger.Error("failed to create plan backup task", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
				return
			}
			go r.dispatchBackupTaskAfterStorageSync(task, storageName, plan.StorageRepoID, plan.SourceClusterID)
			writeJSON(w, http.StatusCreated, map[string]any{"task": task})
			return
		}
		for _, appID := range plan.AppIDs {
			if appID == "" {
				continue
			}
			if _, dup := seen[appID]; dup {
				continue
			}
			app, ok, _ := r.store.GetApplication(appID)
			if !ok {
				continue
			}
			seen[appID] = struct{}{}
			targets = append(targets, planApp{
				appID:                   appID,
				ns:                      app.Namespace,
				storage:                 storageName,
				storageRepoID:           plan.StorageRepoID,
				scope:                   plan.ScopeType,
				includedResources:       plan.IncludedResources,
				resourceSelection:       plan.ResourceSelection,
				labelSelector:           plan.LabelSelector,
				excludedResources:       plan.ExcludedResources,
				includeClusterResources: plan.IncludeClusterScoped,
			})
		}
	} else {
		plan, ok, err := r.findActiveProtectionPlanForBackupTarget(body.ClusterID, body.AppID, body.SourceNamespace)
		if err != nil {
			r.logger.Error("failed to resolve protection plan for backup target", "cluster_id", body.ClusterID, "app_id", body.AppID, "namespace", body.SourceNamespace, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "resolve_protection_plan_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "protection_plan_required"})
			return
		}
		if !tenantVisible(req, plan.TenantID) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "protection_plan_required"})
			return
		}
		body.ProtectionPlanID = plan.ID
		body.ClusterID = plan.SourceClusterID
		repo, hasRepo, _ := r.store.GetStorageRepository(plan.StorageRepoID)
		storageName := body.StorageRepo
		if hasRepo {
			storageName = storageDomainBSLName(repo, plan.SourceClusterID)
		}
		appIDs := plan.AppIDs
		if len(appIDs) == 0 && plan.AppID != "" {
			appIDs = []string{plan.AppID}
		}
		for _, appID := range appIDs {
			if appID == "" {
				continue
			}
			app, ok, _ := r.store.GetApplication(appID)
			if !ok {
				continue
			}
			if body.AppID != "" && app.ID != body.AppID {
				continue
			}
			if body.SourceNamespace != "" && app.Namespace != body.SourceNamespace {
				continue
			}
			if _, dup := seen[app.ID]; dup {
				continue
			}
			seen[app.ID] = struct{}{}
			targets = append(targets, planApp{
				appID:                   app.ID,
				ns:                      app.Namespace,
				storage:                 storageName,
				storageRepoID:           plan.StorageRepoID,
				scope:                   plan.ScopeType,
				includedResources:       plan.IncludedResources,
				resourceSelection:       plan.ResourceSelection,
				labelSelector:           plan.LabelSelector,
				excludedResources:       plan.ExcludedResources,
				includeClusterResources: plan.IncludeClusterScoped,
			})
		}
	}
	if len(targets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no_resolvable_applications"})
		return
	}

	tasks := make([]store.Task, 0, len(targets))
	reused := 0
	for _, tgt := range targets {
		body.SourceNamespace = tgt.ns
		body.StorageRepo = tgt.storage
		body.Scope = tgt.scope
		body.IncludedResources = tgt.includedResources
		body.ResourceSelection = tgt.resourceSelection
		body.LabelSelector = tgt.labelSelector
		body.ExcludedResources = tgt.excludedResources
		body.IncludeClusterResources = tgt.includeClusterResources
		if existing, ok, err := r.findActiveBackupTask(body.ClusterID, body.ProtectionPlanID, tgt.appID, tgt.ns); err != nil {
			r.logger.Error("failed to check active backup tasks", "cluster_id", body.ClusterID, "protection_plan_id", body.ProtectionPlanID, "namespace", tgt.ns, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "check_active_backup_failed"})
			return
		} else if ok {
			reused++
			tasks = append(tasks, existing)
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  existing.ID,
				Level:   "info",
				Reason:  "sync_already_running",
				Message: "Sync is already running.",
			})
			continue
		}
		task, err := r.createPendingBackupTask(body, tgt.appID)
		if err != nil {
			r.logger.Error("failed to create backup task", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
			return
		}
		tasks = append(tasks, task)
		go r.dispatchBackupTaskAfterStorageSync(task, tgt.storage, tgt.storageRepoID, body.ClusterID)
	}
	statusCode := http.StatusCreated
	warning := ""
	if reused > 0 {
		warning = "Sync is already running."
		if reused == len(tasks) {
			statusCode = http.StatusOK
		}
	}
	if len(tasks) == 1 {
		response := map[string]any{"task": tasks[0]}
		if warning != "" {
			response["warning"] = warning
			response["reused"] = true
		}
		writeJSON(w, statusCode, response)
		return
	}
	response := map[string]any{"tasks": tasks}
	if warning != "" {
		response["warning"] = warning
		response["reused"] = reused
	}
	writeJSON(w, statusCode, response)
}

func (r *Router) findActiveProtectionPlanForBackupTarget(clusterID string, appID string, namespace string) (store.ProtectionPlan, bool, error) {
	if clusterID == "" {
		return store.ProtectionPlan{}, false, nil
	}
	if appID != "" {
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return store.ProtectionPlan{}, false, err
		}
		if ok && app.ClusterID == clusterID && namespace == "" {
			namespace = app.Namespace
		}
	}
	plans, err := r.store.ListProtectionPlans(clusterID)
	if err != nil {
		return store.ProtectionPlan{}, false, err
	}
	for _, plan := range plans {
		if !protectionPlanAllowsBackup(plan.Status) {
			continue
		}
		appIDs := plan.AppIDs
		if len(appIDs) == 0 && plan.AppID != "" {
			appIDs = []string{plan.AppID}
		}
		if appID != "" && slices.Contains(appIDs, appID) {
			return plan, true, nil
		}
		if namespace == "" {
			continue
		}
		for _, planAppID := range appIDs {
			app, ok, err := r.store.GetApplication(planAppID)
			if err != nil {
				return store.ProtectionPlan{}, false, err
			}
			if ok && app.ClusterID == clusterID && app.Namespace == namespace {
				return plan, true, nil
			}
		}
	}
	return store.ProtectionPlan{}, false, nil
}

func (r *Router) planSourceNamespaces(plan store.ProtectionPlan) ([]string, []string, error) {
	sourceNamespaces := []string{}
	appIDs := plan.AppIDs
	if len(appIDs) == 0 && plan.AppID != "" {
		appIDs = []string{plan.AppID}
	}
	resolvedAppIDs := []string{}
	for _, appID := range appIDs {
		if appID == "" || slices.Contains(resolvedAppIDs, appID) {
			continue
		}
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return nil, nil, err
		}
		if !ok || app.Namespace == "" {
			continue
		}
		resolvedAppIDs = append(resolvedAppIDs, appID)
		if !slices.Contains(sourceNamespaces, app.Namespace) {
			sourceNamespaces = append(sourceNamespaces, app.Namespace)
		}
	}
	if len(sourceNamespaces) == 0 {
		return nil, nil, errors.New("protection plan has no application namespaces")
	}
	return sourceNamespaces, resolvedAppIDs, nil
}

func (r *Router) findActiveBackupTask(clusterID string, protectionPlanID string, appID string, namespace string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.Type != "backup" || !isActiveTaskStatus(task.Status) {
			continue
		}
		if !task.CompletedAt.IsZero() {
			continue
		}
		if protectionPlanID != "" && task.ProtectionPlanID != protectionPlanID {
			continue
		}
		if appID != "" && task.AppID != "" && task.AppID != appID {
			continue
		}
		taskNamespace := taskPayloadString(task.Payload, "sourceNamespace")
		if namespace != "" && taskNamespace != "" && taskNamespace != namespace {
			continue
		}
		if protectionPlanID != "" || appID != "" || namespace != "" {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) createPendingBackupTask(body backupTaskRequest, appID string) (store.Task, error) {
	if body.ProtectionPlanID == "" {
		return store.Task{}, errors.New("backup task requires protection plan id")
	}
	if appID == "" {
		plan, ok, err := r.store.GetProtectionPlan(body.ProtectionPlanID)
		if err != nil {
			return store.Task{}, err
		}
		if !ok {
			return store.Task{}, errors.New("protection plan not found")
		}
		appID = plan.AppID
		if appID == "" && len(plan.AppIDs) == 1 {
			appID = plan.AppIDs[0]
		}
	}
	if appID == "" {
		return store.Task{}, errors.New("backup task requires application id")
	}
	commandID := store.NewPublicID()
	veleroBackupName := ""
	if body.ProtectionPlanID != "" {
		veleroBackupName = backupNameForPlan(body.ProtectionPlanID, commandID, body.Trigger)
	}
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        body.ClusterID,
		AppID:            appID,
		ProtectionPlanID: body.ProtectionPlanID,
		Type:             "backup",
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"sourceNamespace":         body.SourceNamespace,
			"sourceNamespaces":        body.SourceNamespaces,
			"scope":                   body.Scope,
			"includedResources":       body.IncludedResources,
			"resourceSelection":       body.ResourceSelection,
			"labelSelector":           body.LabelSelector,
			"storageRepo":             body.StorageRepo,
			"excludedResources":       body.ExcludedResources,
			"includeClusterResources": body.IncludeClusterResources,
			"veleroBackupName":        veleroBackupName,
			"trigger":                 body.Trigger,
			"scheduled":               body.Trigger == "scheduled",
			"requestedBy":             firstNonEmptyString(body.RequestedBy, "System"),
		},
	})
	if err != nil {
		return store.Task{}, err
	}
	return task, nil
}

func (r *Router) dispatchBackupTaskAfterStorageSync(task store.Task, storageName string, storageRepoID string, sourceClusterID string) {
	if storageRepoID == "" {
		r.dispatchBackupTask(task)
		return
	}
	if r.isStorageAlreadySynced(task.ClusterID, storageName, storageRepoID, sourceClusterID) {
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "storage_preflight_skipped",
			Message: "Storage location already configured on source cluster.",
		})
		r.dispatchBackupTask(task)
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
		r.logger.Error("storage sync preflight failed before backup dispatch", "cluster_id", task.ClusterID, "task_id", task.ID, "storage_repo", storageName, "storage_task_id", storageTask.ID, "error", err)
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
		Message: "Dispatching sync task...",
		Payload: map[string]any{"storageTaskId": storageTask.ID},
	})
	r.dispatchBackupTask(task)
}

func (r *Router) dispatchBackupTask(task store.Task) {
	conn, ok := r.hub.get(task.ClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; backup will be dispatched after reconnect",
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "warning",
			Reason:  "dispatch_waiting_agent",
			Message: "Dispatching sync task...",
		})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch backup task after storage preflight", "task_id", task.ID, "error", err)
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
			Message: "Dispatching sync task...",
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
		Message: "Dispatching sync task...",
	})
}
