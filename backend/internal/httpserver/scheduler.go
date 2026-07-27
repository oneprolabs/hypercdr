package httpserver

import (
	"log/slog"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

const schedulerTickInterval = 30 * time.Second

func (r *Router) startScheduler() {
	r.schedulerOnce.Do(func() {
		go r.schedulerLoop()
	})
}

func (r *Router) schedulerLoop() {
	ticker := time.NewTicker(schedulerTickInterval)
	defer ticker.Stop()
	for range ticker.C {
		r.runSchedulerTick(time.Now().UTC())
	}
}

func (r *Router) runSchedulerTick(now time.Time) {
	r.scheduleLogMaintenance(now)
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
	return nextPolicyFireAtInLocation(policy, after, time.UTC)
}

func nextPolicyFireAtInLocation(policy store.Policy, after time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	after = after.UTC()
	switch policy.ScheduleType {
	case "interval":
		value := policy.IntervalValue
		if value <= 0 {
			value = 1
		}
		switch policy.IntervalUnit {
		case "minute", "minutes":
			return after.Add(time.Duration(value) * time.Minute).Truncate(time.Second)
		case "hour", "hours", "":
			return after.Add(time.Duration(value) * time.Hour).Truncate(time.Second)
		default:
			return after.Add(time.Hour).Truncate(time.Second)
		}
	case "daily":
		localAfter := after.In(location)
		next := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), clampHour(policy.Hour), clampMinute(policy.Minute), 0, 0, location)
		if !next.After(localAfter) {
			next = next.AddDate(0, 0, 1)
		}
		return next.UTC()
	case "weekly":
		localAfter := after.In(location)
		target := time.Weekday(clampWeekday(policy.WeekDay))
		next := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), clampHour(policy.Hour), clampMinute(policy.Minute), 0, 0, location)
		days := (int(target) - int(localAfter.Weekday()) + 7) % 7
		next = next.AddDate(0, 0, days)
		if !next.After(localAfter) {
			next = next.AddDate(0, 0, 7)
		}
		return next.UTC()
	case "monthly":
		localAfter := after.In(location)
		day := clampMonthDay(policy.MonthDay)
		next := monthlyTime(localAfter.Year(), localAfter.Month(), day, clampHour(policy.Hour), clampMinute(policy.Minute), location)
		if !next.After(localAfter) {
			nextMonth := localAfter.AddDate(0, 1, 0)
			next = monthlyTime(nextMonth.Year(), nextMonth.Month(), day, clampHour(policy.Hour), clampMinute(policy.Minute), location)
		}
		return next.UTC()
	default:
		return after.Add(time.Hour).Truncate(time.Second)
	}
}

func monthlyTime(year int, month time.Month, day int, hour int, minute int, location *time.Location) time.Time {
	last := time.Date(year, month+1, 0, hour, minute, 0, 0, location).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, hour, minute, 0, 0, location)
}

func scheduleMatchesPolicy(next time.Time, policy store.Policy, location *time.Location) bool {
	if next.IsZero() {
		return false
	}
	if policy.ScheduleType == "interval" {
		return true
	}
	if location == nil {
		location = time.UTC
	}
	local := next.In(location)
	if local.Hour() != clampHour(policy.Hour) || local.Minute() != clampMinute(policy.Minute) {
		return false
	}
	switch policy.ScheduleType {
	case "daily":
		return true
	case "weekly":
		return local.Weekday() == time.Weekday(clampWeekday(policy.WeekDay))
	case "monthly":
		return local.Day() == monthlyTime(local.Year(), local.Month(), clampMonthDay(policy.MonthDay), local.Hour(), local.Minute(), location).Day()
	default:
		return false
	}
}

func errStorageRepositoryNotFound() error {
	return errString("storage repository not found")
}

type errString string

func (e errString) Error() string { return string(e) }
