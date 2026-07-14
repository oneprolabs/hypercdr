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

func (r *Router) reconcilePlatformSchedules(now time.Time) {
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
		if _, ok, err := r.store.GetProtectionPlanSchedule(plan.ID); err != nil {
			r.logger.Error("failed to get protection plan schedule during reconcile", "plan_id", plan.ID, "error", err)
			continue
		} else if ok {
			continue
		}
		if _, err := r.store.UpsertProtectionPlanSchedule(store.ProtectionPlanScheduleInput{
			ProtectionPlanID: plan.ID,
			NextFireAt:       nextPolicyFireAt(policy, now),
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
		ClusterID:        plan.SourceClusterID,
		AppID:            firstStringFromStrings(appIDs),
		ProtectionPlanID: plan.ID,
		SourceNamespace:  firstStringFromStrings(sourceNamespaces),
		SourceNamespaces: sourceNamespaces,
		Scope:            plan.ScopeType,
		LabelSelector:    plan.LabelSelector,
		StorageRepo:      storageName,
		ExcludeRules:     plan.ExcludeRules,
		Trigger:          "scheduled",
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
		next := time.Date(after.Year(), after.Month(), after.Day(), clampHour(policy.Hour), clampMinute(policy.Minute), 0, 0, time.UTC)
		if !next.After(after) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	case "weekly":
		target := time.Weekday(clampWeekday(policy.WeekDay))
		next := time.Date(after.Year(), after.Month(), after.Day(), clampHour(policy.Hour), clampMinute(policy.Minute), 0, 0, time.UTC)
		days := (int(target) - int(after.Weekday()) + 7) % 7
		next = next.AddDate(0, 0, days)
		if !next.After(after) {
			next = next.AddDate(0, 0, 7)
		}
		return next
	case "monthly":
		day := clampMonthDay(policy.MonthDay)
		next := monthlyTime(after.Year(), after.Month(), day, clampHour(policy.Hour), clampMinute(policy.Minute))
		if !next.After(after) {
			next = monthlyTime(after.AddDate(0, 1, 0).Year(), after.AddDate(0, 1, 0).Month(), day, clampHour(policy.Hour), clampMinute(policy.Minute))
		}
		return next
	default:
		return after.Add(time.Hour).Truncate(time.Second)
	}
}

func monthlyTime(year int, month time.Month, day int, hour int, minute int) time.Time {
	last := time.Date(year, month+1, 0, hour, minute, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}

func errStorageRepositoryNotFound() error {
	return errString("storage repository not found")
}

type errString string

func (e errString) Error() string { return string(e) }
