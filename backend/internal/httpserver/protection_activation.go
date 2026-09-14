package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"strconv"
	"strings"
	"time"
)

func (r *Router) continueProtectionPlanActivationAfterStorage(task store.Task) {
	if task.ProtectionPlanID == "" {
		return
	}
	if taskPayloadString(task.Payload, "activationRole") == "target" {
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(task.ProtectionPlanID)
	if err != nil {
		r.logger.Error("failed to load protection plan after storage sync", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		return
	}
	if !ok {
		return
	}
	if plan.Status != "activating_storage" {
		return
	}
	ready, warning, err := r.protectionPlanStorageBindingsReady(plan)
	if err != nil {
		r.logger.Error("failed to evaluate protection plan storage bindings", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan storage failed", "plan_id", plan.ID, "error", updateErr)
		}
		return
	}
	if !ready {
		if warning != "" {
			r.logger.Warn("protection plan storage binding is not ready", "plan_id", plan.ID, "task_id", task.ID, "warning", warning)
		}
		return
	}
	if attemptID := taskPayloadString(task.Payload, "activationAttempt"); attemptID != "" {
		ready, err := r.protectionPlanActivationStorageReady(plan.ID, attemptID)
		if err != nil {
			r.logger.Error("failed to evaluate protection plan storage activation tasks", "plan_id", plan.ID, "task_id", task.ID, "error", err)
			if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed"); updateErr != nil {
				r.logger.Error("failed to mark protection plan storage failed", "plan_id", plan.ID, "error", updateErr)
			}
			return
		}
		if !ready {
			return
		}
	}
	policy, shouldSchedule, err := r.protectionPlanSchedulePolicy(plan)
	if err != nil {
		r.logger.Error("failed to evaluate protection plan schedule policy", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan schedule failed", "plan_id", plan.ID, "error", updateErr)
		}
		return
	}
	if !shouldSchedule {
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "active"); err != nil {
			r.logger.Error("failed to mark protection plan active", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		}
		return
	}
	if err := r.enableProtectionPlanSchedule(plan, policy); err != nil {
		r.logger.Error("failed to enable platform schedule for protection plan", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan schedule failed", "plan_id", plan.ID, "error", updateErr)
		}
		return
	}
	if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "active"); err != nil {
		r.logger.Error("failed to mark protection plan active after enabling platform schedule", "plan_id", plan.ID, "task_id", task.ID, "error", err)
	}
}

func (r *Router) protectionPlanStorageBindingsReady(plan store.ProtectionPlan) (bool, string, error) {
	if plan.StorageRepoID == "" {
		return false, "storage repository is not set", nil
	}
	sourceReady, sourceMessage, err := r.clusterStorageBindingReady(plan.SourceClusterID, plan.StorageRepoID, plan.SourceClusterID)
	if err != nil {
		return false, "", err
	}
	if !sourceReady {
		if sourceMessage == "" {
			sourceMessage = "source cluster storage binding is not ready"
		}
		return false, sourceMessage, nil
	}
	if plan.TargetClusterID == "" || plan.TargetClusterID == plan.SourceClusterID {
		return true, "", nil
	}
	targetReady, targetMessage, err := r.clusterStorageBindingReady(plan.TargetClusterID, plan.StorageRepoID, plan.SourceClusterID)
	if err != nil {
		return false, "", err
	}
	if !targetReady {
		if targetMessage == "" {
			targetMessage = "target cluster storage binding is not ready"
		}
		return true, targetMessage, nil
	}
	return true, "", nil
}

func (r *Router) clusterStorageBindingReady(clusterID string, storageRepoID string, sourceClusterID string) (bool, string, error) {
	repo, ok, err := r.store.GetStorageRepository(storageRepoID)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "storage repository not found", nil
	}
	binding, ok, err := r.store.GetClusterStorageBinding(clusterID, storageRepoID, sourceClusterID)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "storage binding has not been configured", nil
	}
	expectedBSLName := storageDomainBSLName(repo, sourceClusterID)
	expectedPrefix := storageDomainPrefix(repo.TenantID, sourceClusterID)
	if binding.BSLName != expectedBSLName || binding.ObjectPrefix != expectedPrefix {
		return false, "storage binding uses an outdated backup storage location", nil
	}
	if strings.EqualFold(binding.Status, "ready") && binding.LastSuccessAt.After(repo.UpdatedAt.Add(-time.Second)) {
		return true, "", nil
	}
	if strings.EqualFold(binding.Status, "failed") {
		if binding.LastErrorMessage != "" {
			return false, binding.LastErrorMessage, nil
		}
		if binding.LastErrorCode != "" {
			return false, binding.LastErrorCode, nil
		}
		return false, "storage binding failed", nil
	}
	if strings.EqualFold(binding.Status, "configuring") {
		return false, "storage binding is being configured", nil
	}
	return false, "storage binding is not ready", nil
}

func (r *Router) markClusterStorageBindingReady(task store.Task) {
	repositoryID := taskPayloadString(task.Payload, "repositoryId")
	if task.ClusterID == "" || repositoryID == "" {
		return
	}
	sourceClusterID := taskPayloadString(task.Payload, "sourceClusterId")
	if sourceClusterID == "" {
		sourceClusterID = task.ClusterID
	}
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil || !ok {
		if err != nil {
			r.logger.Warn("failed to load storage repository while marking binding ready", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
		}
		return
	}
	now := time.Now().UTC()
	if !task.CompletedAt.IsZero() {
		now = task.CompletedAt
	}
	if _, ok, err := r.store.UpdateClusterStorageBindingStatus(store.ClusterStorageBindingStatusInput{
		ClusterID:       task.ClusterID,
		StorageRepoID:   repositoryID,
		SourceClusterID: sourceClusterID,
		Status:          "ready",
		RetryCount:      taskPayloadInt(task.Payload, "retryAttempt"),
		LastSyncedAt:    now,
		LastSuccessAt:   now,
		RepoUpdatedAt:   repo.UpdatedAt,
	}); err != nil {
		r.logger.Warn("failed to mark cluster storage binding ready", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
	} else if !ok {
		_, _ = r.store.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
			ClusterID:       task.ClusterID,
			StorageRepoID:   repositoryID,
			SourceClusterID: sourceClusterID,
			BSLName:         taskPayloadString(task.Payload, "name"),
			ObjectPrefix:    taskPayloadString(task.Payload, "objectPrefix"),
			Status:          "ready",
			RetryCount:      taskPayloadInt(task.Payload, "retryAttempt"),
			RepoUpdatedAt:   repo.UpdatedAt,
		})
		_, _, _ = r.store.UpdateClusterStorageBindingStatus(store.ClusterStorageBindingStatusInput{
			ClusterID:       task.ClusterID,
			StorageRepoID:   repositoryID,
			SourceClusterID: sourceClusterID,
			Status:          "ready",
			RetryCount:      taskPayloadInt(task.Payload, "retryAttempt"),
			LastSyncedAt:    now,
			LastSuccessAt:   now,
			RepoUpdatedAt:   repo.UpdatedAt,
		})
	}
}

func (r *Router) markClusterStorageBindingFailed(task store.Task, code string, message string) {
	repositoryID := taskPayloadString(task.Payload, "repositoryId")
	if task.ClusterID == "" || repositoryID == "" {
		return
	}
	sourceClusterID := taskPayloadString(task.Payload, "sourceClusterId")
	if sourceClusterID == "" {
		sourceClusterID = task.ClusterID
	}
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil {
		r.logger.Warn("failed to load storage repository while marking binding failed", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
		return
	}
	repoUpdatedAt := time.Time{}
	if ok {
		repoUpdatedAt = repo.UpdatedAt
	}
	if _, _, err := r.store.UpdateClusterStorageBindingStatus(store.ClusterStorageBindingStatusInput{
		ClusterID:        task.ClusterID,
		StorageRepoID:    repositoryID,
		SourceClusterID:  sourceClusterID,
		Status:           "failed",
		RetryCount:       taskPayloadInt(task.Payload, "retryAttempt"),
		LastSyncedAt:     time.Now().UTC(),
		LastErrorCode:    code,
		LastErrorMessage: message,
		RepoUpdatedAt:    repoUpdatedAt,
	}); err != nil {
		r.logger.Warn("failed to mark cluster storage binding failed", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
	}
}

func (r *Router) reconcileProtectionPlanActivationStates(clusterID string) {
	plans, err := r.store.ListProtectionPlans(clusterID)
	if err != nil {
		r.logger.Warn("failed to list protection plans for activation reconcile", "cluster_id", clusterID, "error", err)
		return
	}
	for _, plan := range plans {
		switch strings.ToLower(plan.Status) {
		case "activating_storage":
			r.reconcileProtectionPlanStorageActivation(plan)
		case "activating_schedule":
			r.reconcileProtectionPlanScheduleActivation(plan)
		}
	}
}

func (r *Router) reconcileProtectionPlanStorageActivation(plan store.ProtectionPlan) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to list tasks for storage activation reconcile", "plan_id", plan.ID, "error", err)
		return
	}
	latestAttempt := ""
	for _, task := range tasks {
		if task.ProtectionPlanID != plan.ID || task.Type != "storage-sync" {
			continue
		}
		attemptID := taskPayloadString(task.Payload, "activationAttempt")
		if attemptID == "" {
			continue
		}
		if latestAttempt == "" || task.CreatedAt.After(latestStorageAttemptCreatedAt(tasks, plan.ID, latestAttempt)) {
			latestAttempt = attemptID
		}
	}
	if latestAttempt == "" {
		return
	}
	for _, task := range tasks {
		if task.ProtectionPlanID != plan.ID || task.Type != "storage-sync" {
			continue
		}
		if taskPayloadString(task.Payload, "activationAttempt") != latestAttempt {
			continue
		}
		if isActiveTaskStatus(task.Status) && task.CreatedAt.Add(protectionPlanActivationTaskTimeout).Before(time.Now().UTC()) {
			message := "storage configuration timed out while waiting for agent response"
			failedTask, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       task.ID,
				Status:       "failed",
				Progress:     task.Progress,
				ErrorCode:    "STORAGE_SYNC_TIMEOUT",
				ErrorMessage: message,
				MarkDone:     true,
			})
			if err != nil {
				r.logger.Warn("failed to mark storage activation task timed out", "plan_id", plan.ID, "task_id", task.ID, "error", err)
				continue
			}
			r.markClusterStorageBindingFailed(failedTask, "STORAGE_SYNC_TIMEOUT", message)
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  task.ID,
				Level:   "error",
				Reason:  "storage_sync_timeout",
				Message: message,
				Payload: map[string]any{"timeoutSeconds": int(protectionPlanActivationTaskTimeout.Seconds())},
			})
			if r.retryStorageSyncTask(failedTask, message) {
				continue
			}
			if taskPayloadString(failedTask.Payload, "activationRole") == "target" {
				r.finishTargetStorageSyncFailed(failedTask)
			} else if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed"); err != nil {
				r.logger.Warn("failed to mark protection plan storage failed after timeout", "plan_id", plan.ID, "error", err)
			}
		}
	}
	latestTask := latestStorageTaskForAttempt(tasks, plan.ID, latestAttempt)
	if latestTask.ID != "" {
		if taskPayloadBool(latestTask.Payload, "reconfigureStorage") {
			r.finishProtectionPlanStorageReconfigure(latestTask)
		} else {
			r.continueProtectionPlanActivationAfterStorage(latestTask)
		}
	}
}

func latestStorageAttemptCreatedAt(tasks []store.Task, planID string, attemptID string) time.Time {
	var latest time.Time
	for _, task := range tasks {
		if task.ProtectionPlanID == planID && task.Type == "storage-sync" && taskPayloadString(task.Payload, "activationAttempt") == attemptID && task.CreatedAt.After(latest) {
			latest = task.CreatedAt
		}
	}
	return latest
}

func latestStorageTaskForAttempt(tasks []store.Task, planID string, attemptID string) store.Task {
	var latest store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "storage-sync" || taskPayloadString(task.Payload, "activationAttempt") != attemptID {
			continue
		}
		if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	return latest
}

func (r *Router) reconcileProtectionPlanScheduleActivation(plan store.ProtectionPlan) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to list tasks for schedule activation reconcile", "plan_id", plan.ID, "error", err)
		return
	}
	var latest store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != plan.ID || task.Type != "schedule-sync" {
			continue
		}
		if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	if latest.ID == "" {
		return
	}
	if isCompletedTaskStatus(latest.Status) {
		status := "active"
		if r.hasTargetStorageWarning(plan.ID) {
			status = "active_with_warning"
		}
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, status); err != nil {
			r.logger.Warn("failed to mark protection plan active during schedule reconcile", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
		}
		return
	}
	if strings.EqualFold(latest.Status, "failed") {
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); err != nil {
			r.logger.Warn("failed to mark protection plan schedule failed during reconcile", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
		}
		return
	}
	if isActiveTaskStatus(latest.Status) && latest.CreatedAt.Add(protectionPlanActivationTaskTimeout).Before(time.Now().UTC()) {
		message := "schedule configuration timed out while waiting for agent response"
		if _, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       latest.ID,
			Status:       "failed",
			Progress:     latest.Progress,
			ErrorCode:    "SCHEDULE_SYNC_TIMEOUT",
			ErrorMessage: message,
			MarkDone:     true,
		}); err != nil {
			r.logger.Warn("failed to mark schedule activation task timed out", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
			return
		}
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  latest.ID,
			Level:   "error",
			Reason:  "schedule_sync_timeout",
			Message: message,
			Payload: map[string]any{"timeoutSeconds": int(protectionPlanActivationTaskTimeout.Seconds())},
		})
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); err != nil {
			r.logger.Warn("failed to mark protection plan schedule failed after timeout", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
		}
	}
}

func (r *Router) finishProtectionPlanStorageReconfigure(task store.Task) {
	if task.ProtectionPlanID == "" {
		return
	}
	attemptID := taskPayloadString(task.Payload, "activationAttempt")
	if attemptID == "" {
		if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "active"); err != nil {
			r.logger.Error("failed to mark protection plan active after storage reconfigure", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		}
		return
	}
	ready, err := r.protectionPlanActivationStorageReady(task.ProtectionPlanID, attemptID)
	if err != nil {
		r.logger.Error("failed to evaluate protection plan storage reconfigure tasks", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "storage_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan storage failed", "plan_id", task.ProtectionPlanID, "error", updateErr)
		}
		return
	}
	if !ready {
		return
	}
	r.continueProtectionPlanActivationAfterStorage(task)
}

func (r *Router) protectionPlanActivationStorageReady(planID string, attemptID string) (bool, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return false, err
	}
	var sourceTask *store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "storage-sync" {
			continue
		}
		if taskPayloadString(task.Payload, "activationAttempt") != attemptID {
			continue
		}
		role := taskPayloadString(task.Payload, "activationRole")
		if role != "" && role != "source" {
			continue
		}
		if sourceTask == nil || task.CreatedAt.After(sourceTask.CreatedAt) {
			taskCopy := task
			sourceTask = &taskCopy
		}
	}
	if sourceTask == nil {
		return false, nil
	}
	switch strings.ToLower(sourceTask.Status) {
	case "succeeded", "completed", "success":
		return true, nil
	case "failed":
		message := sourceTask.ErrorMessage
		if message == "" {
			message = sourceTask.ErrorCode
		}
		if message == "" {
			message = "source storage sync task failed"
		}
		return false, errors.New(message)
	default:
		return false, nil
	}
}

func (r *Router) retryStorageSyncTask(task store.Task, failureMessage string) bool {
	attemptID := taskPayloadString(task.Payload, "activationAttempt")
	if attemptID == "" {
		return false
	}
	attempt := taskPayloadInt(task.Payload, "retryAttempt")
	if attempt <= 0 {
		attempt = 1
	}
	maxAttempts := taskPayloadInt(task.Payload, "maxAttempts")
	if maxAttempts <= 0 {
		maxAttempts = storageSyncMaxAttempts
	}
	if attempt >= maxAttempts {
		return false
	}
	repositoryID := taskPayloadString(task.Payload, "repositoryId")
	if repositoryID == "" {
		return false
	}
	role := taskPayloadString(task.Payload, "activationRole")
	reconfigure := taskPayloadBool(task.Payload, "reconfigureStorage")
	sourceClusterID := taskPayloadString(task.Payload, "sourceClusterId")
	if sourceClusterID == "" {
		sourceClusterID = task.ClusterID
	}
	nextTask, warning, err := r.dispatchStorageSyncTaskForPlanActivationAttempt(task.ClusterID, repositoryID, task.ProtectionPlanID, sourceClusterID, attemptID, role, reconfigure, attempt+1)
	if err != nil {
		r.logger.Error("failed to retry storage sync task", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "role", role, "attempt", attempt, "error", err)
		return false
	}
	message := fmt.Sprintf("Storage sync failed; retrying attempt %d/%d.", attempt+1, maxAttempts)
	if failureMessage != "" {
		message += " Last error: " + failureMessage
	}
	if warning != "" {
		message += " " + warning
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "warning",
		Reason:  "storage_sync_retry_scheduled",
		Message: message,
		Payload: map[string]any{
			"nextTaskId":     nextTask.ID,
			"retryAttempt":   attempt + 1,
			"maxAttempts":    maxAttempts,
			"activationRole": role,
		},
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  nextTask.ID,
		Level:   "info",
		Reason:  "storage_sync_retry",
		Message: fmt.Sprintf("Storage sync retry attempt %d/%d.", attempt+1, maxAttempts),
		Payload: map[string]any{
			"previousTaskId": task.ID,
			"retryAttempt":   attempt + 1,
			"maxAttempts":    maxAttempts,
			"activationRole": role,
		},
	})
	return true
}

func (r *Router) finishTargetStorageSyncFailed(task store.Task) {
	if task.ProtectionPlanID == "" {
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "warning",
		Reason:  "target_storage_warning",
		Message: "Target cluster storage configuration failed after automatic retries. Backup schedule is not blocked, but restore, drill, and takeover may be unavailable until storage is reconfigured.",
		Payload: map[string]any{
			"impact": []string{"restore_unavailable", "drill_unavailable", "takeover_unavailable"},
		},
	})
	plan, ok, err := r.store.GetProtectionPlan(task.ProtectionPlanID)
	if err != nil {
		r.logger.Error("failed to load protection plan after target storage failure", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		return
	}
	if !ok {
		return
	}
	switch strings.ToLower(plan.Status) {
	case "active", "active_with_warning":
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "active_with_warning"); err != nil {
			r.logger.Error("failed to mark protection plan active with warning", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		}
	case "activating_storage":
		r.continueProtectionPlanActivationAfterStorage(task)
	}
}

func (r *Router) hasTargetStorageWarning(planID string) bool {
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err == nil && ok && plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID && plan.StorageRepoID != "" {
		ready, _, readyErr := r.clusterStorageBindingReady(plan.TargetClusterID, plan.StorageRepoID, plan.SourceClusterID)
		if readyErr != nil {
			r.logger.Warn("failed to inspect target storage binding warning", "plan_id", planID, "error", readyErr)
		} else if !ready {
			return true
		}
	} else if err != nil {
		r.logger.Warn("failed to load protection plan for target storage warning", "plan_id", planID, "error", err)
	}
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to inspect target storage warning", "plan_id", planID, "error", err)
		return false
	}
	var latest *store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "storage-sync" {
			continue
		}
		if taskPayloadString(task.Payload, "activationRole") != "target" {
			continue
		}
		if latest == nil || task.CreatedAt.After(latest.CreatedAt) {
			taskCopy := task
			latest = &taskCopy
		}
	}
	if latest == nil {
		return false
	}
	return strings.EqualFold(latest.Status, "failed")
}

func taskPayloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(value)
		return n
	default:
		return 0
	}
}

func taskPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch value := payload[key].(type) {
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}

func taskPayloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	switch value := payload[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}

func mapInventoryNodes(nodes []protocol.NodeInventory) []store.ClusterNode {
	items := make([]store.ClusterNode, 0, len(nodes))
	for _, node := range nodes {
		role := node.Role
		if role == "" {
			role = "<none>"
		}
		status := node.Status
		if status == "" {
			status = "unknown"
		}
		items = append(items, store.ClusterNode{
			Name:           node.Name,
			Status:         status,
			Roles:          role,
			AgeSeconds:     node.AgeSeconds,
			KubeletVersion: node.KubeletVersion,
			Capacity:       node.Capacity,
		})
	}
	return items
}

func mapInventoryStorageClasses(storageClasses []protocol.StorageClassInventory) []store.ClusterStorageClass {
	items := make([]store.ClusterStorageClass, 0, len(storageClasses))
	for _, storageClass := range storageClasses {
		items = append(items, store.ClusterStorageClass{
			Name:                 storageClass.Name,
			Provisioner:          storageClass.Provisioner,
			ReclaimPolicy:        storageClass.ReclaimPolicy,
			VolumeBindingMode:    storageClass.VolumeBindingMode,
			AllowVolumeExpansion: storageClass.AllowVolumeExpansion,
			Default:              storageClass.Default,
			AgeSeconds:           storageClass.AgeSeconds,
		})
	}
	return items
}

func mapInventoryAPIResources(resources []protocol.APIResourceInventory) []store.ClusterAPIResource {
	items := make([]store.ClusterAPIResource, 0, len(resources))
	for _, resource := range resources {
		items = append(items, store.ClusterAPIResource{
			Group: resource.Group, Version: resource.Version, Resource: resource.Resource,
			Kind: resource.Kind, Namespaced: resource.Namespaced,
		})
	}
	return items
}

func mapInventoryNamespaceAPIs(resources []protocol.NamespaceAPIInventory) []store.ClusterNamespaceAPI {
	items := make([]store.ClusterNamespaceAPI, 0, len(resources))
	for _, resource := range resources {
		items = append(items, store.ClusterNamespaceAPI{
			Scope: resource.Scope, Namespace: resource.Namespace, Group: resource.Group, Version: resource.Version,
			Resource: resource.Resource, Kind: resource.Kind, Count: resource.Count,
		})
	}
	return items
}

func mapInventoryCapabilities(capabilities []protocol.NamedCapabilityInventory) []store.ClusterCapability {
	items := make([]store.ClusterCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		items = append(items, store.ClusterCapability{Type: capability.Type, Name: capability.Name, Driver: capability.Driver, Fields: capability.Fields})
	}
	return items
}
