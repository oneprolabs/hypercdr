package httpserver

import (
	"log/slog"
	"time"

	"hypercdr-platform/platform/backend/internal/service/scheduling"
	"hypercdr-platform/platform/backend/internal/store"
)

const schedulerTickInterval = 30 * time.Second
const componentUpgradeVerificationTimeout = 10 * time.Minute
const recoveryTaskInactivityTimeout = 15 * time.Minute
const maintenanceTaskInactivityTimeout = 35 * time.Minute

func (r *Router) startScheduler() {
	r.schedulerOnce.Do(func() {
		r.workers.Add(1)
		go func() { defer r.workers.Done(); r.schedulerLoop() }()
	})
}

func (r *Router) schedulerLoop() {
	ticker := time.NewTicker(schedulerTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.workerContext.Done():
			return
		case now := <-ticker.C:
			if r.workerContext.Err() != nil {
				return
			}
			r.runSchedulerTick(now.UTC())
		}
	}
}

func (r *Router) runSchedulerTick(now time.Time) {
	if _, err := r.store.RunSchedulerExclusive(func() { r.runSchedulerTickLocked(now) }); err != nil {
		r.logger.Error("failed to acquire scheduler database lock", "error", err)
	}
}

func (r *Router) runSchedulerTickLocked(now time.Time) {
	r.scheduleLogMaintenance(now)
	r.reconcileComponentUpgradeTimeouts(now)
	r.reconcileAbandonedRecoveryTasks(now)
	r.reconcileAbandonedMaintenanceTasks(now)
	if frozen, err := r.store.HasCommunityMigrationFreeze(); err != nil {
		r.logger.Error("failed to check Community migration freeze", "error", err)
		return
	} else if frozen {
		return
	}
	r.reconcileProtectionCleanupPlans(now)
	if jobs, err := r.store.ListPlatformUpgradeJobs(); err == nil {
		for _, job := range jobs {
			if !isTerminalPlatformUpgradeStatus(job.Status) {
				return
			}
		}
	}
	r.reconcilePlatformSchedules(now)
	due, err := r.store.ListDueProtectionPlanSchedules(now)
	if err != nil {
		r.logger.Error("failed to list due protection plan schedules", "error", err)
		return
	}
	for _, schedule := range due {
		r.fireProtectionPlanSchedule(schedule, now)
	}
}

func (r *Router) reconcileAbandonedMaintenanceTasks(now time.Time) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to reconcile abandoned maintenance tasks", "error", err)
		return
	}
	for _, task := range tasks {
		if !maintenanceTaskTimedOut(task, now) {
			continue
		}
		message := "No cleanup status was received from the cluster agent for 35 minutes. The maintenance task was closed so it cannot indefinitely block cluster lifecycle operations. Review the task events and cluster resources before retrying."
		if _, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID: task.ID, Status: "failed", Progress: task.Progress, MarkDone: true,
			ErrorCode: "MAINTENANCE_STATUS_TIMEOUT", ErrorMessage: message,
		}); err != nil {
			r.logger.Warn("failed to expire abandoned maintenance task", "task_id", task.ID, "error", err)
			continue
		}
		_ = r.addTaskEventIfChanged(store.TaskEventInput{TaskID: task.ID, Level: "error", Reason: "maintenance_status_timeout", Message: message})
	}
}

func maintenanceTaskTimedOut(task store.Task, now time.Time) bool {
	if (task.Type != "protection-cleanup" && task.Type != "retention-cleanup") || !isActiveTaskStatus(task.Status) {
		return false
	}
	lastActivity := task.CreatedAt
	for _, candidate := range []time.Time{task.DispatchedAt, task.AcceptedAt, task.StartedAt} {
		if candidate.After(lastActivity) {
			lastActivity = candidate
		}
	}
	if raw, ok := task.Payload["lastStatusAt"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil && parsed.After(lastActivity) {
			lastActivity = parsed
		}
	}
	return !lastActivity.IsZero() && !now.Before(lastActivity.Add(maintenanceTaskInactivityTimeout))
}

func (r *Router) reconcileAbandonedRecoveryTasks(now time.Time) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to reconcile abandoned recovery tasks", "error", err)
		return
	}
	for _, task := range tasks {
		if !recoveryTaskTimedOut(task, now) {
			continue
		}
		message := "No restore status was received from the cluster agent for 15 minutes. The agent may have restarted or the Velero restore may no longer exist. Source data and backup data were preserved; check the agent and Velero logs, then retry the drill."
		if _, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID: task.ID, Status: "failed", Progress: task.Progress, MarkDone: true,
			ErrorCode: "RESTORE_STATUS_TIMEOUT", ErrorMessage: message,
		}); err != nil {
			r.logger.Warn("failed to expire abandoned recovery task", "task_id", task.ID, "error", err)
			continue
		}
		_ = r.addTaskEventIfChanged(store.TaskEventInput{
			TaskID: task.ID, Level: "error", Reason: "restore_status_timeout", Message: message,
		})
	}
}

func recoveryTaskTimedOut(task store.Task, now time.Time) bool {
	if (task.Type != "restore" && task.Type != "drill") || !isActiveTaskStatus(task.Status) {
		return false
	}
	raw, ok := task.Payload["lastStatusAt"].(string)
	if !ok {
		// Do not expire tasks created by platform versions that did not persist
		// activity timestamps.
		return false
	}
	lastActivity, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && !now.Before(lastActivity.Add(recoveryTaskInactivityTimeout))
}

func (r *Router) reconcileProtectionCleanupPlans(now time.Time) {
	plans, err := r.store.ListProtectionPlans("")
	if err != nil {
		r.logger.Warn("failed to list protection plans for cleanup reconcile", "error", err)
		return
	}
	for _, plan := range plans {
		if plan.Status != "cleanup_running" {
			continue
		}
		complete, err := r.protectionCleanupTasksComplete(plan, "")
		if err != nil {
			r.logger.Warn("failed to reconcile protection cleanup tasks", "plan_id", plan.ID, "error", err)
			continue
		}
		if complete {
			if _, _, err := r.store.CleanupProtectionPlanRecords(plan.ID); err != nil {
				r.logger.Error("failed to finalize reconciled protection cleanup", "plan_id", plan.ID, "error", err)
				_, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "cleanup_failed")
			}
			continue
		}
		// A cleanup that has made no terminal progress for ten minutes must not
		// remain an unexplained spinner forever.
		if !plan.UpdatedAt.IsZero() && now.After(plan.UpdatedAt.Add(10*time.Minute)) {
			_, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "cleanup_failed")
			r.logger.Warn("protection cleanup timed out during reconcile", "plan_id", plan.ID)
		}
	}
}

func (r *Router) reconcileComponentUpgradeTimeouts(now time.Time) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to reconcile component upgrade timeouts", "error", err)
		return
	}
	for _, task := range tasks {
		if !componentUpgradeTimedOut(task, now) {
			continue
		}
		component := "Comm Agent"
		if task.Type == "velero-upgrade" {
			component = "Velero"
		}
		message := component + " did not report the expected running image and version within 10 minutes. Check the workload rollout and image pull status in the managed cluster."
		if _, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID: task.ID, Status: "failed", Progress: task.Progress, MarkDone: true,
			ErrorCode: "UPGRADE_VERIFICATION_TIMEOUT", ErrorMessage: message,
		}); err != nil {
			r.logger.Warn("failed to expire component upgrade task", "task_id", task.ID, "error", err)
			continue
		}
		_ = r.addTaskEventIfChanged(store.TaskEventInput{
			TaskID: task.ID, Level: "error", Reason: "verification_timeout", Message: message,
		})
	}
}

func componentUpgradeTimedOut(task store.Task, now time.Time) bool {
	if (task.Type != "agent-upgrade" && task.Type != "velero-upgrade") || !isActiveTaskStatus(task.Status) {
		return false
	}
	started := task.StartedAt
	if started.IsZero() {
		started = task.AcceptedAt
	}
	if started.IsZero() {
		started = task.DispatchedAt
	}
	if started.IsZero() {
		started = task.CreatedAt
	}
	return !started.IsZero() && !now.Before(started.Add(componentUpgradeVerificationTimeout))
}

func isTerminalPlatformUpgradeStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled" || status == "rolled_back"
}

func (r *Router) reconcilePlatformSchedules(now time.Time) {
	location := time.UTC
	plans, err := r.store.ListProtectionPlans("")
	if err != nil {
		r.logger.Error("failed to list protection plans for schedule reconcile", "error", err)
		return
	}
	for _, plan := range plans {
		if !protectionPlanAllowsBackup(plan.Status) {
			continue
		}
		policy, shouldSchedule, err := r.protectionPlanSchedulePolicy(plan)
		if err != nil {
			r.logger.Error("failed to evaluate plan schedule during reconcile", "plan_id", plan.ID, "error", err)
			continue
		}
		if !shouldSchedule {
			continue
		}
		schedule, ok, err := r.store.GetProtectionPlanSchedule(plan.ID)
		if err != nil {
			r.logger.Error("failed to get protection plan schedule during reconcile", "plan_id", plan.ID, "error", err)
			continue
		}
		if ok && schedule.Enabled && scheduleMatchesPolicy(schedule.NextFireAt, policy, location) {
			continue
		}
		if _, err := r.store.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{
			ProtectionPlanID: plan.ID,
			NextFireAt:       nextPolicyFireAtInLocation(policy, now, location),
			Enabled:          true,
		}); err != nil {
			r.logger.Error("failed to initialize platform schedule for protection plan", "plan_id", plan.ID, "error", err)
		}
	}
}

func (r *Router) fireProtectionPlanSchedule(schedule store.ProtectionPlanSchedule, now time.Time) {
	plan, ok, err := r.store.GetProtectionPlan(schedule.ProtectionPlanID)
	if err != nil {
		r.logger.Error("failed to load scheduled protection plan", "plan_id", schedule.ProtectionPlanID, "error", err)
		return
	}
	if !ok {
		_ = r.store.DisableProtectionPlanSchedule(schedule.ProtectionPlanID)
		return
	}
	policy, shouldSchedule, err := r.protectionPlanSchedulePolicy(plan)
	if err != nil {
		r.logger.Error("failed to evaluate scheduled protection plan policy", "plan_id", plan.ID, "error", err)
		return
	}
	if !shouldSchedule || !protectionPlanAllowsBackup(plan.Status) {
		_ = r.store.DisableProtectionPlanSchedule(plan.ID)
		return
	}
	nextFireAt := nextPolicyFireAt(policy, now)
	if existing, ok, err := r.findActiveBackupTask(plan.SourceClusterID, plan.ID, "", ""); err != nil {
		r.logger.Error("failed to check active backup before scheduled dispatch", "plan_id", plan.ID, "error", err)
		return
	} else if ok {
		_, _, _ = r.store.MarkProtectionPlanScheduleFired(store.ProtectionPlanScheduleFiredInput{
			ProtectionPlanID: plan.ID,
			LastFiredAt:      now,
			NextFireAt:       nextFireAt,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  existing.ID,
			Level:   "info",
			Reason:  "scheduled_sync_skipped",
			Message: "Scheduled sync skipped because a backup is already running for this protection plan.",
		})
		return
	}
	task, err := r.createScheduledBackupTask(plan, policy, now)
	if err != nil {
		r.logger.Error("failed to create scheduled backup task", "plan_id", plan.ID, "error", err)
		return
	}
	_, _, _ = r.store.MarkProtectionPlanScheduleFired(store.ProtectionPlanScheduleFiredInput{
		ProtectionPlanID: plan.ID,
		LastFiredAt:      now,
		NextFireAt:       nextFireAt,
	})
	r.logger.Info("scheduled backup task created", slog.String("plan_id", plan.ID), slog.String("task_id", task.ID))
}

func (r *Router) createScheduledBackupTask(plan store.ProtectionPlan, policy store.Policy, now time.Time) (store.Task, error) {
	sourceNamespaces, appIDs, err := r.planSourceNamespaces(plan)
	if err != nil {
		return store.Task{}, err
	}
	repo, ok, err := r.store.GetStorageRepository(plan.StorageRepoID)
	if err != nil {
		return store.Task{}, err
	}
	if !ok {
		return store.Task{}, errStorageRepositoryNotFound()
	}
	storageName := storageDomainBSLName(repo, plan.SourceClusterID)
	request := backupTaskRequest{
		ClusterID:         plan.SourceClusterID,
		AppID:             firstStringFromStrings(appIDs),
		ProtectionPlanID:  plan.ID,
		SourceNamespace:   firstStringFromStrings(sourceNamespaces),
		SourceNamespaces:  sourceNamespaces,
		Scope:             plan.ScopeType,
		IncludedResources: plan.IncludedResources,
		ResourceSelection: plan.ResourceSelection,
		LabelSelector:     plan.LabelSelector,
		StorageRepo:       storageName,
		ExcludedResources: plan.ExcludedResources,
		Trigger:           "scheduled",
	}
	task, err := r.createPendingBackupTask(request, request.AppID)
	if err != nil {
		return store.Task{}, err
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "scheduled_sync_created",
		Message: "Scheduled sync task created.",
		Payload: map[string]any{
			"policyId":         policy.ID,
			"scheduledAt":      now,
			"sourceNamespaces": sourceNamespaces,
		},
	})
	go r.dispatchBackupTaskAfterStorageSync(task, storageName, plan.StorageRepoID, plan.SourceClusterID)
	return task, nil
}

func (r *Router) enableProtectionPlanSchedule(plan store.ProtectionPlan, policy store.Policy) error {
	next := nextPolicyFireAt(policy, time.Now().UTC())
	_, err := r.store.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{
		ProtectionPlanID: plan.ID,
		NextFireAt:       next,
		Enabled:          true,
	})
	return err
}

func nextPolicyFireAt(policy store.Policy, after time.Time) time.Time {
	return scheduling.NextPolicyFireAt(policy, after)
}
func nextPolicyFireAtInLocation(policy store.Policy, after time.Time, location *time.Location) time.Time {
	return scheduling.NextPolicyFireAtInLocation(policy, after, location)
}
func scheduleMatchesPolicy(next time.Time, policy store.Policy, location *time.Location) bool {
	return scheduling.ScheduleMatchesPolicy(next, policy, location)
}

func errStorageRepositoryNotFound() error {
	return errString("storage repository not found")
}

type errString string

func (e errString) Error() string { return string(e) }
