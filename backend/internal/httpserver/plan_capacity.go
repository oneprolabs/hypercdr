package httpserver

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"net/http"
	"slices"
	"sort"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

type capacityTaskResponse struct {
	Task   store.Task `json:"task"`
	Reused bool       `json:"reused"`
}

type capacityStateResponse struct {
	Size map[string]any `json:"size"`
	Task *store.Task    `json:"task"`
}

func (r *Router) getPlanCapacity(w http.ResponseWriter, req *http.Request) {
	plan, ok, err := r.store.GetProtectionPlan(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "load_plan_failed"})
		return
	}
	if !ok || !tenantVisible(req, plan.TenantID) {
		writeJSON(w, 404, map[string]any{"error": "plan_not_found"})
		return
	}
	tasks, err := r.store.ListTasksFiltered(store.TaskFilter{TenantID: plan.TenantID, ClusterID: plan.SourceClusterID, Types: []string{"plan-capacity"}, CapacityPlanID: plan.ID, Limit: 1})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "load_capacity_tasks_failed"})
		return
	}
	response := capacityStateResponse{Size: plan.PlanStorageSize}
	for _, task := range tasks {
		command, err := capacityCommand(task.Payload)
		if err != nil {
			continue
		}
		for _, target := range command.Plans {
			if target.PlanID == plan.ID {
				response.Task = &task
				break
			}
		}
		if response.Task != nil {
			break
		}
	}
	writeJSON(w, 200, response)
}

func capacityCommand(payload map[string]any) (protocol.PlanCapacityCommand, error) {
	var command protocol.PlanCapacityCommand
	raw, err := json.Marshal(payload["planCapacity"])
	if err == nil {
		err = json.Unmarshal(raw, &command)
	}
	if err == nil && (command.Namespace == "" || len(command.Plans) == 0) {
		err = errors.New("capacity collection requires a namespace and plans")
	}
	return command, err
}

func capacityDue(now time.Time, clusterID string) bool {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(clusterID))
	now = now.UTC()
	return now.Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)) >= time.Duration(hash.Sum32()%300)*time.Second
}

func (r *Router) planCapacityTarget(plan store.ProtectionPlan) (protocol.PlanCapacityTarget, error) {
	target := protocol.PlanCapacityTarget{PlanID: plan.ID, StorageRepositoryID: plan.StorageRepoID}
	repo, ok, err := r.store.GetStorageRepository(plan.StorageRepoID)
	if err != nil {
		return target, err
	}
	if !ok || repo.TenantID != plan.TenantID {
		return target, errors.New("storage repository unavailable")
	}
	target.StorageLocation = storageDomainBSLName(repo, plan.SourceClusterID)
	appIDs := append([]string{}, plan.AppIDs...)
	if len(appIDs) == 0 && plan.AppID != "" {
		appIDs = append(appIDs, plan.AppID)
	}
	for _, appID := range appIDs {
		app, found, err := r.store.GetApplication(appID)
		if err != nil {
			return target, err
		}
		if !found || app.ClusterID != plan.SourceClusterID || app.Namespace == "" {
			return target, errors.New("plan namespace unavailable")
		}
		if !slices.Contains(target.Namespaces, app.Namespace) {
			target.Namespaces = append(target.Namespaces, app.Namespace)
		}
	}
	sort.Strings(target.Namespaces)
	if len(target.Namespaces) == 0 {
		return target, errors.New("plan has no namespaces")
	}
	return target, nil
}

func capacityPlanEligible(plan store.ProtectionPlan) bool {
	return plan.SourceClusterID != "" && plan.StorageRepoID != ""
}

func (r *Router) createCapacityTask(clusterID string, plans []store.ProtectionPlan, now time.Time, daily bool) (store.Task, bool, error) {
	trigger := "manual"
	if daily {
		trigger = "scheduled"
	}
	return r.createCapacityTaskTriggered(clusterID, plans, now, trigger)
}

func (r *Router) createCapacityTaskTriggered(clusterID string, plans []store.ProtectionPlan, now time.Time, trigger string) (store.Task, bool, error) {
	daily := trigger == "scheduled"
	if len(plans) == 0 {
		return store.Task{}, false, errors.New("no plans to collect")
	}
	tasks, err := r.store.ListTasksFiltered(store.TaskFilter{ClusterID: clusterID, Types: []string{"plan-capacity"}, Statuses: []string{"queued", "dispatched", "accepted", "running"}, Limit: 1})
	if err != nil {
		return store.Task{}, false, err
	}
	day := now.UTC().Format("2006-01-02")
	for _, task := range tasks {
		if isActiveTaskStatus(task.Status) {
			command, err := capacityCommand(task.Payload)
			if err != nil {
				return store.Task{}, false, err
			}
			if daily {
				return task, true, nil
			}
			for _, target := range command.Plans {
				if target.PlanID == plans[0].ID {
					return task, true, nil
				}
			}
			return store.Task{}, false, errors.New("another capacity collection is running for this cluster")
		}
	}
	if daily {
		previous, err := r.store.ListTasksFiltered(store.TaskFilter{ClusterID: clusterID, Types: []string{"plan-capacity"}, CapacityScheduleDay: day, Limit: 1})
		if err != nil {
			return store.Task{}, false, err
		}
		if len(previous) != 0 {
			return previous[0], true, nil
		}
	}
	if !r.hub.supportsCapacity(clusterID) {
		return store.Task{}, false, errors.New("cluster agent is offline or requires an upgrade")
	}
	command := protocol.PlanCapacityCommand{Namespace: r.dataProtectionNamespaceForCluster(clusterID)}
	for _, plan := range plans {
		if !capacityPlanEligible(plan) {
			continue
		}
		target, err := r.planCapacityTarget(plan)
		if err != nil {
			target.ErrorMessage = err.Error()
		}
		command.Plans = append(command.Plans, target)
	}
	if len(command.Plans) == 0 {
		return store.Task{}, false, errors.New("no eligible plans")
	}
	payload := map[string]any{"planCapacity": command, "trigger": trigger}
	if daily {
		payload["trigger"], payload["scheduleDay"] = "scheduled", day
	}
	task, err := r.store.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "plan-capacity", Status: "queued", CommandID: store.NewPublicID(), Payload: payload})
	if err != nil {
		return store.Task{}, false, err
	}
	conn, ok := r.hub.get(clusterID)
	if ok {
		r.startWorker(func() { r.dispatchComponentUpgrade(conn, task, "Plan capacity collection dispatched") })
	}
	return task, false, nil
}

func (r *Router) collectInitialPlanCapacity(plan store.ProtectionPlan, now time.Time) error {
	if !capacityPlanEligible(plan) || !r.hub.supportsCapacity(plan.SourceClusterID) {
		return nil
	}
	if int64FromAny(plan.PlanStorageSize["totalBytes"]) > 0 && stringFromMap(plan.PlanStorageSize, "collectedAt") != "" {
		return nil
	}
	backups, err := r.store.ListTasksFiltered(store.TaskFilter{TenantID: plan.TenantID, ClusterID: plan.SourceClusterID, ProtectionPlanID: plan.ID, Types: []string{"backup"}, Statuses: []string{"succeeded"}, Limit: 1})
	if err != nil || len(backups) == 0 {
		return err
	}
	tasks, err := r.store.ListTasksFiltered(store.TaskFilter{TenantID: plan.TenantID, ClusterID: plan.SourceClusterID, Types: []string{"plan-capacity"}, CapacityPlanID: plan.ID, CapacityTrigger: "first-backup", Limit: 1})
	if err != nil {
		return err
	}
	if len(tasks) > 0 {
		return nil
	}
	_, _, err = r.createCapacityTaskTriggered(plan.SourceClusterID, []store.ProtectionPlan{plan}, now, "first-backup")
	return err
}

func (r *Router) collectInitialPlanCapacityAfterBackup(planID string) {
	_, err := r.store.RunSchedulerExclusive(func() {
		plan, found, err := r.store.GetProtectionPlan(planID)
		if err == nil && found {
			err = r.collectInitialPlanCapacity(plan, time.Now().UTC())
		}
		if err != nil {
			r.logger.Warn("initial plan capacity collection deferred", "plan_id", planID, "error", err)
		}
	})
	if err != nil {
		r.logger.Warn("initial plan capacity scheduling deferred", "plan_id", planID, "error", err)
	}
}

func (r *Router) schedulePlanCapacity(now time.Time) {
	plans, err := r.store.ListProtectionPlans("")
	if err != nil {
		r.logger.Warn("failed to list capacity collection plans", "error", err)
		return
	}
	groups := map[string][]store.ProtectionPlan{}
	for _, plan := range plans {
		if capacityPlanEligible(plan) {
			groups[plan.SourceClusterID] = append(groups[plan.SourceClusterID], plan)
		}
	}
	for clusterID, plans := range groups {
		if !capacityDue(now, clusterID) || !r.hub.supportsCapacity(clusterID) {
			continue
		}
		if _, _, err := r.createCapacityTask(clusterID, plans, now, true); err != nil {
			r.logger.Warn("failed to schedule plan capacity collection", "cluster_id", clusterID, "error", err)
		}
	}
	for _, plan := range plans {
		if err := r.collectInitialPlanCapacity(plan, now); err != nil {
			r.logger.Warn("initial plan capacity collection deferred", "plan_id", plan.ID, "error", err)
		}
	}
}

func (r *Router) refreshPlanCapacity(w http.ResponseWriter, req *http.Request) {
	plan, ok, err := r.store.GetProtectionPlan(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "load_plan_failed"})
		return
	}
	if !ok || !tenantVisible(req, plan.TenantID) {
		writeJSON(w, 404, map[string]any{"error": "plan_not_found"})
		return
	}
	var task store.Task
	var reused bool
	locked, lockErr := r.store.RunSchedulerExclusive(func() {
		task, reused, err = r.createCapacityTask(plan.SourceClusterID, []store.ProtectionPlan{plan}, time.Now().UTC(), false)
	})
	if lockErr != nil {
		writeJSON(w, 500, map[string]any{"error": "capacity_schedule_failed"})
		return
	}
	if !locked {
		writeJSON(w, 409, map[string]any{"error": "scheduler_busy", "message": "Please retry shortly."})
		return
	}
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "capacity_unavailable", "message": err.Error()})
		return
	}
	writeJSON(w, 202, capacityTaskResponse{Task: task, Reused: reused})
}

func (r *Router) persistCapacityReport(task store.Task, report *protocol.PlanCapacityReport) error {
	command, err := capacityCommand(task.Payload)
	if err != nil {
		return err
	}
	if report == nil || len(report.Plans) != len(command.Plans) {
		return errors.New("incomplete capacity report")
	}
	targets := map[string]protocol.PlanCapacityTarget{}
	for _, target := range command.Plans {
		targets[target.PlanID] = target
	}
	seen := map[string]bool{}
	for _, result := range report.Plans {
		if _, ok := targets[result.PlanID]; !ok || seen[result.PlanID] {
			return errors.New("unexpected or duplicate plan capacity result")
		}
		seen[result.PlanID] = true
		if result.Status != "succeeded" && result.Status != "failed" {
			return errors.New("invalid capacity result status")
		}
		if result.CollectedAt.IsZero() || result.CollectedAt.After(time.Now().UTC().Add(5*time.Minute)) {
			return errors.New("invalid collection time")
		}
		if result.MetadataBytes < 0 || result.KopiaBytes < 0 || result.TotalBytes < 0 || result.TotalBytes-result.MetadataBytes != result.KopiaBytes {
			return errors.New("invalid capacity values")
		}
	}
	for _, result := range report.Plans {
		target := targets[result.PlanID]
		plan, found, err := r.store.GetProtectionPlan(result.PlanID)
		if err != nil {
			return err
		}
		if !found || plan.SourceClusterID != task.ClusterID || plan.TenantID != task.TenantID {
			continue
		}
		current, err := r.planCapacityTarget(plan)
		if plan.StorageRepoID != target.StorageRepositoryID || (target.ErrorMessage == "" && (err != nil || current.StorageLocation != target.StorageLocation || !slices.Equal(current.Namespaces, target.Namespaces))) {
			continue
		}
		patch := map[string]any{"status": result.Status, "requestedAt": task.CreatedAt.UTC(), "lastAttemptAt": result.CollectedAt.UTC(), "taskId": task.ID, "errorMessage": result.ErrorMessage}
		if result.Status == "succeeded" {
			patch["metadataBytes"], patch["kopiaBytes"], patch["totalBytes"], patch["collectedAt"] = result.MetadataBytes, result.KopiaBytes, result.TotalBytes, result.CollectedAt.UTC()
		}
		if err := r.store.SavePlanCapacityResult(plan.ID, task.ClusterID, task.TenantID, patch, task.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}
