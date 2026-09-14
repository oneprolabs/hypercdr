package httpserver

import (
	"errors"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"slices"
	"strings"
	"time"
)

func (r *Router) listProtectionPlans(w http.ResponseWriter, req *http.Request) {
	clusterID := req.URL.Query().Get("clusterId")
	r.reconcileProtectionPlanActivationStates(clusterID)
	items, err := r.store.ListProtectionPlans(clusterID)
	if err != nil {
		r.logger.Error("failed to list protection plans", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_protection_plans_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	for index := range items {
		items[index].Status = protectionPlanBusinessStatus(items[index].Status)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func protectionPlanBusinessStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending_activation", "activating_storage", "activating_schedule", "configuring":
		return "configuring"
	case "active", "ready":
		return "ready"
	case "active_with_warning", "ready_with_warning":
		return "ready_with_warning"
	case "storage_failed", "schedule_failed", "configuration_failed":
		return "configuration_failed"
	case "cleanup_running", "cleaning":
		return "cleaning"
	case "cleanup_failed":
		return "cleanup_failed"
	default:
		return "configuring"
	}
}

func (r *Router) createProtectionPlan(w http.ResponseWriter, req *http.Request) {
	var input store.ProtectionPlanInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if actor, ok := requestUser(req); ok {
		input.TenantID = actor.TenantID
	}
	if input.TenantID != "" {
		clusters, _ := r.store.ListClusters()
		clusterAllowed := func(id string) bool {
			if id == "" {
				return true
			}
			for _, item := range clusters {
				if item.ID == id && item.TenantID == input.TenantID {
					return true
				}
			}
			return false
		}
		storageItem, storageFound, _ := r.store.GetStorageRepository(input.StorageRepoID)
		policyAllowed := input.PolicyID == ""
		if !policyAllowed {
			policies, _ := r.store.ListPolicies()
			for _, item := range policies {
				if item.ID == input.PolicyID && item.TenantID == input.TenantID {
					policyAllowed = true
					break
				}
			}
		}
		appsAllowed := true
		seenApps := map[string]bool{}
		for _, appID := range append(append([]string{}, input.AppIDs...), input.AppID) {
			if appID == "" || seenApps[appID] {
				continue
			}
			seenApps[appID] = true
			app, found, _ := r.store.GetApplication(appID)
			if !found || !clusterAllowed(app.ClusterID) {
				appsAllowed = false
				break
			}
		}
		if !clusterAllowed(input.SourceClusterID) || !clusterAllowed(input.TargetClusterID) || !storageFound || storageItem.TenantID != input.TenantID || !policyAllowed || !appsAllowed {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource_not_found", "message": "One or more selected resources are not available in this tenant."})
			return
		}
	}
	if input.SourceClusterID == "" || (input.AppID == "" && len(input.AppIDs) == 0) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "source_cluster_id_and_apps_required"})
		return
	}
	if input.TargetClusterID != "" && !r.clustersDRCompatible(input.SourceClusterID, input.TargetClusterID) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           "cluster_type_incompatible",
			"message":         "The source and target cluster types are incompatible for disaster recovery. OpenShift requires an OpenShift target; Native Kubernetes and Huawei Cloud CCE can target each other.",
			"sourceClusterId": input.SourceClusterID, "targetClusterId": input.TargetClusterID,
		})
		return
	}
	if input.StorageRepoID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "storage_repository_required"})
		return
	}
	if input.ScopeType == "" {
		input.ScopeType = "all"
	}
	if input.ScopeType != "all" && input.ScopeType != "filtered" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_scope_type", "allowed": []string{"all", "filtered"}})
		return
	}
	// A resource-type selection is meaningful only for one namespace. For a
	// multi-namespace plan, always retain Velero's unfiltered semantics even if
	// an older client submits a custom selection.
	selectedNamespaces := map[string]struct{}{}
	seenAppIDs := map[string]struct{}{}
	for _, appID := range append(append([]string{}, input.AppIDs...), input.AppID) {
		if appID == "" {
			continue
		}
		if _, seen := seenAppIDs[appID]; seen {
			continue
		}
		seenAppIDs[appID] = struct{}{}
		if app, found, _ := r.store.GetApplication(appID); found && app.Namespace != "" {
			if app.Namespace == r.agentNamespaceForCluster(input.SourceClusterID) || app.Namespace == r.dataProtectionNamespaceForCluster(input.SourceClusterID) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "reserved_namespace", "message": "The HyperCDR Agent namespace cannot be protected as an application."})
				return
			}
			selectedNamespaces[app.Namespace] = struct{}{}
		}
	}
	if len(selectedNamespaces) > 1 {
		input.ScopeType = "all"
		input.IncludedResources = nil
		input.ExcludedResources = nil
		input.LabelSelector = store.LabelSelector{}
		input.IncludeClusterScoped = false
		input.ResourceSelection = store.ResourceSelection{Mode: "all"}
	}
	if input.ResourceSelection.Mode == "" {
		input.ResourceSelection.Mode = "all"
	}
	if input.ResourceSelection.Mode != "all" && input.ResourceSelection.Mode != "custom" && input.ResourceSelection.Mode != "exclude" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_resource_selection_mode", "allowed": []string{"all", "custom", "exclude"}})
		return
	}
	if input.ResourceSelection.Mode == "custom" && len(input.ResourceSelection.NamespaceScoped) == 0 && len(input.ResourceSelection.ClusterScoped) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "resource_selection_required", "message": "Select at least one namespace-scoped or cluster-scoped resource type."})
		return
	}
	if input.AppID == "" && len(input.AppIDs) > 0 {
		input.AppID = input.AppIDs[0]
	}
	input.Status = "activating_storage"
	item, err := r.store.CreateProtectionPlan(input)
	if err != nil {
		var conflict *store.ApplicationAlreadyProtectedError
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "application_already_protected", "message": "An active protection plan already exists for this application.",
				"protectionPlanId": conflict.ProtectionPlanID, "applicationId": conflict.ApplicationID,
			})
			return
		}
		r.logger.Error("failed to create protection plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_protection_plan_failed"})
		return
	}
	// Creation is the transaction boundary: return the persisted Configuring
	// plan immediately. Storage and schedule activation must not mutate it to
	// Ready inside the Save request, otherwise the UI can never present a
	// truthful, monotonic Configuring -> Ready/Failed state transition.
	writeJSON(w, http.StatusCreated, protectionPlanActivationResponse(item, nil, ""))
	go r.activateNewProtectionPlan(item)
}

func (r *Router) activateNewProtectionPlan(item store.ProtectionPlan) {
	tasks, warning, err := r.dispatchProtectionPlanStorageActivation(item)
	if err != nil {
		r.logger.Error("failed to sync storage repository for protection plan", "plan_id", item.ID, "cluster_id", item.SourceClusterID, "repository_id", item.StorageRepoID, "error", err)
		_, _, _ = r.store.UpdateProtectionPlanStatus(item.ID, "storage_failed")
		return
	}
	if warning != "" {
		r.logger.Warn("storage repository sync queued with warning", "plan_id", item.ID, "cluster_id", item.SourceClusterID, "repository_id", item.StorageRepoID, "warning", warning)
	}
	if len(tasks) > 0 {
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID: tasks[0].ID, Level: "info", Reason: "protection_plan_saved",
			Message: "DR configuration was saved. Background configuration started.",
			Payload: map[string]any{"planId": item.ID},
		})
		if storageActivationTasksIncludeSource(tasks) {
			return
		}
	}
	r.continueProtectionPlanActivationAfterStorage(store.Task{
		ID: "storage-already-synced", ProtectionPlanID: item.ID,
		Status: "succeeded", CreatedAt: time.Now().UTC(),
	})
}

func (r *Router) activateProtectionPlan(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}
	item, ok, err := r.store.GetProtectionPlan(id)
	if err != nil {
		r.logger.Error("failed to load protection plan for activation", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
		return
	}
	switch item.Status {
	case "activating_storage", "activating_schedule":
		writeJSON(w, http.StatusAccepted, protectionPlanActivationResponse(item, nil, "activation is already in progress"))
		return
	}
	item, _, err = r.store.UpdateProtectionPlanStatus(item.ID, "activating_storage")
	if err != nil {
		r.logger.Error("failed to mark protection plan activating", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_protection_plan_failed"})
		return
	}
	if item.StorageRepoID == "" {
		item, _, _ = r.store.UpdateProtectionPlanStatus(item.ID, "storage_failed")
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "storage_repository_required"})
		return
	}
	tasks, warning, err := r.dispatchProtectionPlanStorageActivation(item)
	if err != nil {
		r.logger.Error("failed to dispatch protection plan activation", "id", id, "error", err)
		item, _, _ = r.store.UpdateProtectionPlanStatus(item.ID, "storage_failed")
		writeJSON(w, http.StatusAccepted, protectionPlanActivationResponse(item, nil, "storage sync dispatch failed: "+err.Error()))
		return
	}
	var activationTask *store.Task
	if len(tasks) > 0 {
		activationTask = &tasks[0]
		if !storageActivationTasksIncludeSource(tasks) {
			r.continueProtectionPlanActivationAfterStorage(store.Task{
				ID:               "source-storage-already-synced",
				ProtectionPlanID: item.ID,
				Status:           "succeeded",
				CreatedAt:        time.Now().UTC(),
			})
			if refreshed, ok, err := r.store.GetProtectionPlan(item.ID); err == nil && ok {
				item = refreshed
			}
		}
	} else {
		r.continueProtectionPlanActivationAfterStorage(store.Task{
			ID:               "storage-already-synced",
			ProtectionPlanID: item.ID,
			Status:           "succeeded",
			CreatedAt:        time.Now().UTC(),
		})
		if refreshed, ok, err := r.store.GetProtectionPlan(item.ID); err == nil && ok {
			item = refreshed
		}
	}
	writeJSON(w, http.StatusAccepted, protectionPlanActivationResponse(item, activationTask, warning))
}

func (r *Router) dispatchProtectionPlanStorageActivation(plan store.ProtectionPlan) ([]store.Task, string, error) {
	return r.dispatchProtectionPlanStorageTasks(plan, false)
}

func storageActivationTasksIncludeSource(tasks []store.Task) bool {
	for _, task := range tasks {
		role := taskPayloadString(task.Payload, "activationRole")
		if role == "" || role == "source" {
			return true
		}
	}
	return false
}

func (r *Router) reconfigureProtectionPlanStorage(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(id)
	if err != nil {
		r.logger.Error("failed to load protection plan for storage reconfigure", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
		return
	}
	if plan.StorageRepoID == "" {
		plan, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed")
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "storage_repository_required"})
		return
	}
	plan, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "activating_storage")
	tasks, warning, err := r.dispatchProtectionPlanStorageTasks(plan, true)
	if err != nil {
		r.logger.Error("failed to reconfigure protection plan storage", "id", id, "error", err)
		plan, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed")
		writeJSON(w, http.StatusAccepted, protectionPlanStorageReconfigureResponse(plan, tasks, "storage reconfigure dispatch failed: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusAccepted, protectionPlanStorageReconfigureResponse(plan, tasks, warning))
}

func protectionPlanStorageReconfigureResponse(plan store.ProtectionPlan, tasks []store.Task, warning string) map[string]any {
	response := protectionPlanActivationResponse(plan, nil, warning)
	response["storageTasks"] = nonNilSlice(tasks)
	return response
}

func (r *Router) dispatchProtectionPlanStorageTasks(plan store.ProtectionPlan, reconfigure bool) ([]store.Task, string, error) {
	attemptID := store.NewPublicID()
	targets := []struct {
		clusterID string
		role      string
	}{
		{clusterID: plan.SourceClusterID, role: "source"},
	}
	if plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID {
		targets = append(targets, struct {
			clusterID string
			role      string
		}{clusterID: plan.TargetClusterID, role: "target"})
	}
	tasks := make([]store.Task, 0, len(targets))
	warnings := []string{}
	for _, target := range targets {
		if !reconfigure {
			action, warning, err := r.storageBindingActivationAction(target.clusterID, plan.StorageRepoID, plan.SourceClusterID, target.role)
			if err != nil {
				return tasks, strings.Join(warnings, "; "), err
			}
			if warning != "" {
				warnings = append(warnings, target.role+" cluster: "+warning)
			}
			if action == "skip" {
				r.logger.Info("storage repository binding reused for protection plan activation", "plan_id", plan.ID, "cluster_id", target.clusterID, "repository_id", plan.StorageRepoID, "role", target.role)
				continue
			}
			if action == "wait" {
				r.logger.Info("storage repository binding already configuring for protection plan activation", "plan_id", plan.ID, "cluster_id", target.clusterID, "repository_id", plan.StorageRepoID, "role", target.role)
				continue
			}
		}
		task, warning, err := r.dispatchStorageSyncTaskForPlanActivationAttempt(target.clusterID, plan.StorageRepoID, plan.ID, plan.SourceClusterID, attemptID, target.role, reconfigure, 1)
		if err != nil {
			return tasks, strings.Join(warnings, "; "), err
		}
		tasks = append(tasks, task)
		if warning != "" {
			warnings = append(warnings, target.role+" cluster: "+warning)
		}
	}
	return tasks, strings.Join(warnings, "; "), nil
}

func (r *Router) storageBindingActivationAction(clusterID string, storageRepoID string, sourceClusterID string, role string) (string, string, error) {
	repo, ok, err := r.store.GetStorageRepository(storageRepoID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", errors.New("storage repository not found")
	}
	binding, ok, err := r.store.GetClusterStorageBinding(clusterID, storageRepoID, sourceClusterID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "dispatch", "", nil
	}
	expectedBSLName := storageDomainBSLName(repo, sourceClusterID)
	expectedPrefix := storageDomainPrefix(repo.TenantID, sourceClusterID)
	if binding.BSLName != expectedBSLName || binding.ObjectPrefix != expectedPrefix {
		return "dispatch", "", nil
	}
	if strings.EqualFold(binding.Status, "ready") && binding.LastSuccessAt.After(repo.UpdatedAt.Add(-time.Second)) {
		return "skip", "", nil
	}
	if strings.EqualFold(binding.Status, "configuring") {
		return "wait", "storage binding is already being configured", nil
	}
	if strings.EqualFold(binding.Status, "failed") {
		// A failed binding describes the previous activation attempt. An explicit
		// Retry must create and dispatch a fresh storage-sync task so a corrected
		// environmental problem (for example clock skew or credentials) can
		// recover. Returning the stored error here made the API acknowledge Retry
		// without ever contacting the agent.
		return "dispatch", "", nil
	}
	return "dispatch", "", nil
}

func protectionPlanActivationResponse(item store.ProtectionPlan, activationTask *store.Task, warning string) map[string]any {
	response := map[string]any{
		"id":                   item.ID,
		"tenantId":             item.TenantID,
		"sourceClusterId":      item.SourceClusterID,
		"appId":                item.AppID,
		"appIds":               nonNilSlice(item.AppIDs),
		"scopeType":            item.ScopeType,
		"includedResources":    nonNilSlice(item.IncludedResources),
		"resourceSelection":    item.ResourceSelection,
		"labelSelector":        item.LabelSelector,
		"includeClusterScoped": item.IncludeClusterScoped,
		"storageRepoId":        item.StorageRepoID,
		"policyId":             item.PolicyID,
		"targetClusterId":      item.TargetClusterID,
		"excludedResources":    nonNilSlice(item.ExcludedResources),
		"preHooks":             nonNilSlice(item.PreHooks),
		"postHooks":            nonNilSlice(item.PostHooks),
		"status":               protectionPlanBusinessStatus(item.Status),
		"createdAt":            item.CreatedAt,
		"updatedAt":            item.UpdatedAt,
	}
	if activationTask != nil {
		response["activationTask"] = activationTask
	}
	if warning != "" {
		response["warning"] = warning
	}
	return response
}

func (r *Router) deleteProtectionPlan(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(id)
	if err != nil {
		r.logger.Error("failed to load protection plan before cleanup", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "load_protection_plan_failed", "message": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
		return
	}
	plan, _, err = r.store.UpdateProtectionPlanStatus(id, "cleanup_running")
	if err != nil {
		r.logger.Error("failed to mark protection plan cleanup running", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "mark_cleanup_running_failed", "message": err.Error()})
		return
	}
	cleanupTask, cleanupWarning, err := r.createProtectionCleanupTask(plan)
	if err != nil {
		r.logger.Error("failed to create protection cleanup task", "error", err, "id", id)
		_, _, _ = r.store.UpdateProtectionPlanStatus(id, "cleanup_failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_protection_cleanup_failed", "message": err.Error()})
		return
	}
	response := protectionPlanActivationResponse(plan, nil, cleanupWarning)
	if cleanupTask.ID != "" {
		response["cleanupTask"] = cleanupTask
	}
	writeJSON(w, http.StatusOK, response)
}

func (r *Router) dispatchScheduleSyncTask(plan store.ProtectionPlan) (store.Task, string, error) {
	if plan.PolicyID == "" {
		return store.Task{}, "", nil
	}
	if existing, ok, err := r.existingScheduleSyncTask(plan.ID); err != nil {
		return store.Task{}, "", err
	} else if ok {
		return existing, "", nil
	}
	policy, ok, err := r.findPolicy(plan.PolicyID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok || policy.Status != "active" || policy.ScheduleType == "manual" {
		return store.Task{}, "", nil
	}
	cron, err := policyCron(policy)
	if err != nil {
		return store.Task{}, "", err
	}
	sourceNamespaces := []string{}
	appIDs := plan.AppIDs
	if len(appIDs) == 0 && plan.AppID != "" {
		appIDs = []string{plan.AppID}
	}
	for _, appID := range appIDs {
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return store.Task{}, "", err
		}
		if ok && app.Namespace != "" && !slices.Contains(sourceNamespaces, app.Namespace) {
			sourceNamespaces = append(sourceNamespaces, app.Namespace)
		}
	}
	if len(sourceNamespaces) == 0 {
		return store.Task{}, "", errors.New("protection plan has no application namespaces")
	}
	repo, ok, err := r.store.GetStorageRepository(plan.StorageRepoID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok {
		return store.Task{}, "", errors.New("storage repository not found")
	}
	storageName := storageDomainBSLName(repo, plan.SourceClusterID)
	commandID := store.NewPublicID()
	payload := map[string]any{
		"planId":                  plan.ID,
		"scheduleName":            scheduleNameForPlan(plan.ID),
		"cron":                    cron,
		"sourceNamespaces":        sourceNamespaces,
		"scope":                   plan.ScopeType,
		"includedResources":       plan.IncludedResources,
		"resourceSelection":       plan.ResourceSelection,
		"labelSelector":           plan.LabelSelector,
		"storageRepoId":           repo.ID,
		"storageRepo":             storageName,
		"storageRepoDisplayName":  repo.Name,
		"sourceClusterId":         plan.SourceClusterID,
		"objectPrefix":            storageDomainPrefix(plan.TenantID, plan.SourceClusterID),
		"includeClusterResources": plan.IncludeClusterScoped,
		"excludedResources":       plan.ExcludedResources,
		"retentionCount":          policy.RetentionCount,
	}
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: plan.ID,
		Type:             "schedule-sync",
		Status:           "queued",
		CommandID:        commandID,
		Payload:          payload,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	conn, ok := r.hub.get(plan.SourceClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; schedule sync will be dispatched after reconnect",
		})
		return task, "agent is offline; schedule sync remains queued", nil
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		return task, "schedule sync task created but dispatch failed", nil
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "velero schedule sync dispatched to agent",
	})
	return task, "", nil
}

func (r *Router) existingScheduleSyncTask(planID string) (store.Task, bool, error) {
	if planID == "" {
		return store.Task{}, false, nil
	}
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return store.Task{}, false, err
	}
	var latest store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "schedule-sync" {
			continue
		}
		if !isCompletedTaskStatus(task.Status) && !isActiveTaskStatus(task.Status) {
			continue
		}
		if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	if latest.ID == "" {
		return store.Task{}, false, nil
	}
	return latest, true, nil
}

func (r *Router) findPolicy(policyID string) (store.Policy, bool, error) {
	policies, err := r.store.ListPolicies()
	if err != nil {
		return store.Policy{}, false, err
	}
	for _, policy := range policies {
		if policy.ID == policyID {
			return policy, true, nil
		}
	}
	return store.Policy{}, false, nil
}

func (r *Router) protectionPlanSchedulePolicy(plan store.ProtectionPlan) (store.Policy, bool, error) {
	if plan.PolicyID == "" {
		return store.Policy{}, false, nil
	}
	policy, ok, err := r.findPolicy(plan.PolicyID)
	if err != nil {
		return store.Policy{}, false, err
	}
	if !ok {
		return store.Policy{}, false, errors.New("policy not found")
	}
	if policy.Status != "active" || policy.ScheduleType == "manual" {
		return policy, false, nil
	}
	if _, err := policyCron(policy); err != nil {
		return policy, false, err
	}
	return policy, true, nil
}

func policyCron(policy store.Policy) (string, error) {
	switch policy.ScheduleType {
	case "interval":
		value := policy.IntervalValue
		if value <= 0 {
			return "", errors.New("interval policy value must be greater than zero")
		}
		switch strings.ToLower(policy.IntervalUnit) {
		case "minute", "minutes":
			if value == 1 {
				return "* * * * *", nil
			}
			if value > 59 {
				return "", errors.New("minute interval must be between 1 and 59")
			}
			return fmt.Sprintf("*/%d * * * *", value), nil
		case "hour", "hours", "":
			if value == 1 {
				return "0 * * * *", nil
			}
			if value > 23 {
				return "", errors.New("hour interval must be between 1 and 23")
			}
			return fmt.Sprintf("0 */%d * * *", value), nil
		default:
			return "", errors.New("unsupported interval unit: " + policy.IntervalUnit)
		}
	case "daily":
		return fmt.Sprintf("%d %d * * *", clampMinute(policy.Minute), clampHour(policy.Hour)), nil
	case "weekly":
		return fmt.Sprintf("%d %d * * %d", clampMinute(policy.Minute), clampHour(policy.Hour), clampWeekday(policy.WeekDay)), nil
	case "monthly":
		return fmt.Sprintf("%d %d %d * *", clampMinute(policy.Minute), clampHour(policy.Hour), clampMonthDay(policy.MonthDay)), nil
	default:
		return "", errors.New("unsupported schedule type: " + policy.ScheduleType)
	}
}

func scheduleNameForPlan(planID string) string {
	return "hcdr-" + planUUIDNoDash(planID)
}

func manualBackupNameForPlan(planID string, runID string) string {
	return backupNameForPlan(planID, runID, "manual")
}

func backupNameForPlan(planID string, runID string, trigger string) string {
	base := scheduleNameForPlan(planID)
	if trigger == "manual" || trigger == "" {
		base += "-m"
	}
	base += "-" + time.Now().UTC().Format("20060102150405")
	suffix := strings.ReplaceAll(runID, "-", "")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix != "" {
		base += "-" + suffix
	}
	if len(base) > 63 {
		base = strings.Trim(base[:63], "-")
	}
	return base
}

func planUUIDNoDash(planID string) string {
	id := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(planID), "-", ""))
	if id == "" {
		return "unknown"
	}
	return id
}

func clampMinute(value int) int {
	if value < 0 || value > 59 {
		return 0
	}
	return value
}

func clampHour(value int) int {
	if value < 0 || value > 23 {
		return 0
	}
	return value
}

func clampWeekday(value int) int {
	if value < 0 || value > 6 {
		return 0
	}
	return value
}

func clampMonthDay(value int) int {
	if value < 1 || value > 31 {
		return 1
	}
	return value
}
