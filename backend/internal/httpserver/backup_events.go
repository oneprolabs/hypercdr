package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"slices"
	"sort"
	"strings"
	"time"
)

func (r *Router) ingestVeleroBackupsFromInventory(clusterID string, backups []map[string]any) {
	if len(backups) == 0 {
		return
	}
	existingPoints, err := r.store.ListRestorePoints(store.RestorePointFilter{ClusterID: clusterID, IncludeDeleted: true})
	if err != nil {
		r.logger.Error("failed to list restore points before velero ingest", "cluster_id", clusterID, "error", err)
		return
	}
	seen := map[string]struct{}{}
	for _, point := range existingPoints {
		seen[point.VeleroBackupName] = struct{}{}
	}
	for _, backup := range backups {
		name := stringFromMap(backup, "name")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		phase := stringFromMap(backup, "phase")
		if phase != "Completed" {
			continue
		}
		labels := mapPayload(backup, "labels")
		planID := stringFromMap(labels, "hypercdr.io/plan-id")
		if planID == "" {
			continue
		}
		plan, ok, err := r.store.GetProtectionPlan(planID)
		if err != nil {
			r.logger.Error("failed to load protection plan for velero backup ingest", "plan_id", planID, "error", err)
			continue
		}
		if !ok {
			continue
		}
		if plan.SourceClusterID != clusterID {
			continue
		}
		sourceNamespace := stringFromMap(labels, "hypercdr.io/source-namespace")
		if sourceNamespace == "" {
			sourceNamespace = firstStringFromAny(backup["includedNamespaces"])
		}
		storageName := stringFromMap(backup, "storageLocation")
		completedAt := parseTimeFromAny(backup["completedAt"])
		if completedAt.IsZero() {
			completedAt = parseTimeFromAny(backup["createdAt"])
		}
		task, ok, err := r.findVeleroBackupTask(clusterID, name)
		if err != nil {
			r.logger.Error("failed to find scheduled backup task from inventory", "backup", name, "error", err)
			continue
		}
		if !ok {
			commandID := store.NewPublicID()
			task, err = r.store.CreateTask(store.TaskInput{
				ClusterID:        clusterID,
				AppID:            plan.AppID,
				ProtectionPlanID: plan.ID,
				Type:             "backup",
				Status:           "queued",
				CommandID:        commandID,
				Payload: map[string]any{
					"scheduled":        true,
					"sourceNamespace":  sourceNamespace,
					"storageRepo":      storageName,
					"veleroBackupName": name,
					"phase":            phase,
				},
				SuppressLatestPointer: true,
			})
			if err != nil {
				r.logger.Error("failed to create scheduled backup task from inventory", "backup", name, "error", err)
				continue
			}
		} else if !task.CompletedAt.IsZero() {
			// Inventory is eventually consistent and can report completion after a
			// task has already reached a terminal state (notably after force stop).
			// Terminal task state and restore-point outcome are immutable.
			continue
		}
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:      task.ID,
			Status:      "finalizing",
			Progress:    100,
			Payload:     backupTaskPayloadPatch(backup),
			MarkStarted: true,
		})
		point, err := r.store.CreateRestorePoint(store.RestorePointInput{
			ProtectionPlanID:  plan.ID,
			SourceClusterID:   clusterID,
			AppID:             plan.AppID,
			StorageRepoID:     plan.StorageRepoID,
			TaskCreatedAt:     task.CreatedAt,
			VeleroBackupName:  name,
			PointType:         "backup",
			Status:            "available",
			SizeBytes:         veleroBackupSizeBytes(backup),
			CompletedAt:       completedAt,
			SourceNamespace:   sourceNamespace,
			BackupTaskID:      task.ID,
			BackupStorageName: storageName,
			Metadata: map[string]any{
				"scheduled":        true,
				"phase":            phase,
				"velero":           backup,
				"size":             firstNonEmptyMap(mapFromAny(backup["restorePointSize"]), mapFromAny(backup["size"])),
				"sizeStatus":       backup["sizeStatus"],
				"sizeWarnings":     sliceFromAny(backup["sizeWarnings"]),
				"restorePointSize": firstNonEmptyMap(mapFromAny(backup["restorePointSize"]), mapFromAny(backup["size"])),
				"planStorageSize":  mapFromAny(backup["planStorageSize"]),
			},
		})
		if err != nil {
			r.logger.Error("failed to create restore point from scheduled backup", "backup", name, "error", err)
			continue
		}
		r.scheduleRestorePointContentIndex(point)
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:   task.ID,
			Status:   "succeeded",
			Progress: 100,
			Payload:  backupTaskPayloadPatch(backup),
			MarkDone: true,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "velero-schedule",
			Message: "scheduled velero backup completed",
			Payload: map[string]any{"velero": backup},
		})
		r.updateProtectionPlanStorageSizeFromVelero(plan.ID, backup)
		seen[name] = struct{}{}
		r.reconcileRetention(point.ProtectionPlanID, point.BackupTaskID)
	}
}

func (r *Router) handleVeleroBackupEvent(clusterID string, event protocol.VeleroEventPayload) (store.Task, error) {
	if event.BackupName == "" {
		return store.Task{}, nil
	}
	planID := event.PlanID
	if planID == "" && event.Labels != nil {
		planID = event.Labels["hypercdr.io/plan-id"]
	}
	if planID == "" {
		return store.Task{}, nil
	}
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err != nil {
		r.logger.Error("failed to load protection plan for velero event", "plan_id", planID, "backup", event.BackupName, "error", err)
		return store.Task{}, err
	}
	if !ok {
		return store.Task{}, nil
	}
	if plan.SourceClusterID != clusterID {
		return store.Task{}, nil
	}
	// HyperCDR-created operations must be correlated by task ID.  A Plan ID
	// and Velero name alone are insufficient and can attach an event to the
	// wrong operation (or create a duplicate task).
	if strings.TrimSpace(event.TaskID) == "" {
		if event.Labels == nil || strings.TrimSpace(event.Labels["hypercdr.io/task-id"]) == "" {
			r.logger.Warn("orphan velero event without task id", "cluster_id", clusterID, "plan_id", planID, "backup", event.BackupName, "event_type", event.EventType)
			return store.Task{}, fmt.Errorf("velero event missing task id")
		}
		event.TaskID = strings.TrimSpace(event.Labels["hypercdr.io/task-id"])
	}
	if strings.EqualFold(event.Phase, "Deleting") || strings.EqualFold(event.EventType, "backup_deleting") {
		return store.Task{}, nil
	}
	task, err := r.findOrCreateVeleroBackupTask(clusterID, plan, event)
	if err != nil {
		r.logger.Error("failed to upsert velero backup task", "backup", event.BackupName, "error", err)
		return store.Task{}, err
	}
	if !task.CompletedAt.IsZero() {
		// A late Velero event must not resurrect a canceled/failed operation or
		// create a restore point after its task reached any terminal outcome.
		return task, nil
	}
	status := "running"
	markDone := false
	errorCode := ""
	errorMessage := ""
	switch event.EventType {
	case "backup_completed":
		status = "succeeded"
		markDone = true
	case "backup_failed":
		status = "failed"
		markDone = true
		errorCode = "VELERO_BACKUP_FAILED"
		errorMessage = detailedTaskFailureMessage(event.Message, event.Velero)
	default:
		status = "running"
	}
	if event.Phase == "Failed" || event.Phase == "FailedValidation" || event.Phase == "PartiallyFailed" || event.Phase == "Canceled" {
		status = "failed"
		markDone = true
		errorCode = "VELERO_BACKUP_FAILED"
		errorMessage = detailedTaskFailureMessage(event.Message, event.Velero)
	}
	if event.Phase == "Completed" {
		status = "succeeded"
		markDone = true
	}
	progress := event.Progress
	if markDone && status == "succeeded" {
		progress = 100
	}
	var completedPoint store.RestorePoint
	if status == "succeeded" {
		task, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:      task.ID,
			Status:      "finalizing",
			Progress:    100,
			Payload:     backupTaskPayloadPatch(event.Velero),
			MarkStarted: true,
		})
		if err != nil {
			r.logger.Error("failed to mark velero backup task finalizing", "task_id", task.ID, "backup", event.BackupName, "error", err)
			return task, err
		}
		completedPoint, err = r.createRestorePointFromVeleroEvent(clusterID, plan, task, event)
		if err != nil {
			r.logger.Error("failed to create restore point from velero event", "backup", event.BackupName, "error", err)
			return task, err
		}
	}
	task, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       task.ID,
		Status:       status,
		Progress:     progress,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
		Payload:      backupTaskPayloadPatch(event.Velero),
		MarkStarted:  true,
		MarkDone:     markDone,
	})
	if err != nil {
		r.logger.Error("failed to update velero backup task", "task_id", task.ID, "backup", event.BackupName, "error", err)
		return task, err
	}
	level := "info"
	if status == "failed" {
		level = "error"
	}
	_ = r.addTaskEventIfChanged(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   level,
		Reason:  event.EventType,
		Message: detailedTaskFailureMessage(event.Message, event.Velero),
		Payload: map[string]any{"velero": event.Velero},
	})
	if status == "succeeded" {
		if completedPoint.ID != "" {
			r.updateProtectionPlanStorageSizeFromVelero(plan.ID, event.Velero)
			r.reconcileRetention(completedPoint.ProtectionPlanID, completedPoint.BackupTaskID)
		}
	}
	return task, nil
}

func (r *Router) addTaskEventIfChanged(input store.TaskEventInput) error {
	incomingVelero := mapFromAny(input.Payload["velero"])
	if len(incomingVelero) == 0 {
		return r.store.AddTaskEvent(input)
	}
	events, err := r.store.ListTaskEvents(input.TaskID)
	if err != nil {
		return r.store.AddTaskEvent(input)
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Reason != input.Reason {
			continue
		}
		if sameVeleroEventPayload(mapFromAny(event.Payload["velero"]), incomingVelero) {
			return nil
		}
	}
	return r.store.AddTaskEvent(input)
}

func mapFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	default:
		return map[string]any{}
	}
}

func mapFromAnyJSON(value any) map[string]any {
	if mapped := mapFromAny(value); len(mapped) > 0 {
		return mapped
	}
	if value == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var mapped map[string]any
	if json.Unmarshal(raw, &mapped) != nil {
		return map[string]any{}
	}
	return mapped
}

func firstNonEmptyMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return map[string]any{}
}

func sliceFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func detailedTaskFailureMessage(fallback string, details map[string]any) string {
	messages := taskFailureMessagesFromDetails(details)
	if len(messages) == 0 {
		if fallback != "" {
			return fallback
		}
		return "Task failed"
	}
	prefix := fallback
	if strings.EqualFold(prefix, "velero backup failed") ||
		strings.HasPrefix(prefix, "Velero backup PartiallyFailed:") ||
		strings.HasPrefix(prefix, "Velero backup Failed:") ||
		strings.HasPrefix(prefix, "Velero backup FailedValidation:") ||
		strings.HasPrefix(prefix, "Velero backup Canceled:") {
		prefix = ""
	}
	if prefix == "" {
		return strings.Join(messages, "\n")
	}
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message), strings.TrimSpace(prefix)) {
			return strings.Join(messages, "\n")
		}
	}
	return prefix + "\n" + strings.Join(messages, "\n")
}

func taskFailurePayloadPatch(details map[string]any) map[string]any {
	// A terminal failure must not retain the last running size sample. Task
	// payload updates merge recursively, so the explicit nil is required even
	// when the agent has no additional failure details.
	patch := map[string]any{"sizeProgressV2": nil}
	if velero := mapFromAny(details["velero"]); len(velero) > 0 {
		if stages := sliceFromAny(velero["recoveryStages"]); len(stages) > 0 {
			patch["recoveryStages"] = stages
		}
		if volumeProgress := mapFromAny(velero["volumeProgress"]); len(volumeProgress) > 0 {
			patch["volumeProgress"] = volumeProgress
		}
		if size := mapFromAny(velero["size"]); len(size) > 0 {
			patch["size"] = size
		}
		if restorePointSize := mapFromAny(velero["restorePointSize"]); len(restorePointSize) > 0 {
			patch["restorePointSize"] = restorePointSize
		}
		if planStorageSize := mapFromAny(velero["planStorageSize"]); len(planStorageSize) > 0 {
			patch["planStorageSize"] = planStorageSize
		}
		if sizeStatus := stringFromMap(velero, "sizeStatus"); sizeStatus != "" {
			patch["sizeStatus"] = sizeStatus
		}
		if sizeWarnings := sliceFromAny(velero["sizeWarnings"]); len(sizeWarnings) > 0 {
			patch["sizeWarnings"] = sizeWarnings
		}
		if dataPathFailure := mapFromAny(velero["dataPathFailure"]); len(dataPathFailure) > 0 {
			patch["dataPathFailure"] = dataPathFailure
		}
	}
	if messages := taskFailureMessagesFromDetails(details); len(messages) > 0 {
		patch["failureDetails"] = messages
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func taskFailureMessagesFromDetails(details map[string]any) []string {
	velero := details
	if nested := mapFromAny(details["velero"]); len(nested) > 0 {
		velero = nested
	}
	var messages []string
	volumeProgress := mapFromAny(velero["volumeProgress"])
	for _, raw := range sliceFromAny(volumeProgress["items"]) {
		item := mapFromAny(raw)
		message := strings.TrimSpace(fmt.Sprint(item["message"]))
		if message == "" || message == "<nil>" {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(item["name"]))
		phase := strings.TrimSpace(fmt.Sprint(item["phase"]))
		label := "Volume backup"
		if name != "" && name != "<nil>" {
			label += " " + name
		}
		if phase != "" && phase != "<nil>" {
			label += " " + phase
		}
		messages = append(messages, label+": "+humanizeBackupFailureMessage(message))
	}
	status := mapFromAny(velero["status"])
	if statusMessage := strings.TrimSpace(fmt.Sprint(status["message"])); statusMessage != "" && statusMessage != "<nil>" {
		messages = append(messages, humanizeBackupFailureMessage(statusMessage))
	}
	dataPathFailure := mapFromAny(velero["dataPathFailure"])
	if pod := strings.TrimSpace(fmt.Sprint(dataPathFailure["pod"])); pod != "" && pod != "<nil>" {
		messages = append(messages, "Restore data-path Pod: "+pod)
	}
	if node := strings.TrimSpace(fmt.Sprint(dataPathFailure["node"])); node != "" && node != "<nil>" {
		messages = append(messages, "Target node: "+node)
	}
	if logDetail := strings.TrimSpace(fmt.Sprint(dataPathFailure["logDetail"])); logDetail != "" && logDetail != "<nil>" {
		messages = append(messages, "Original Kopia/Velero log: "+logDetail)
	}
	return dedupeStrings(messages)
}

func humanizeBackupFailureMessage(message string) string {
	if strings.Contains(message, "repository not initialized in the provided storage") {
		return message + "。Kopia 文件系统备份仓库不存在或未初始化，请重新配置/重试该集群的 BackupStorageLocation 后再执行同步；如果刚手动删除过对象存储 kopia 目录，需要先让系统重新初始化仓库。"
	}
	return message
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func backupTaskPayloadPatch(velero map[string]any) map[string]any {
	if len(velero) == 0 {
		return nil
	}
	patch := map[string]any{}
	if name := stringFromMap(velero, "name"); name != "" {
		patch["veleroBackupName"] = name
	}
	if storage := stringFromMap(velero, "storageLocation"); storage != "" {
		patch["backupStorageName"] = storage
		patch["storageLocation"] = storage
	}
	if namespaces := stringArrayFromAny(velero["includedNamespaces"]); len(namespaces) > 0 {
		patch["includedNamespaces"] = namespaces
		if _, ok := patch["sourceNamespace"]; !ok && len(namespaces) == 1 {
			patch["sourceNamespace"] = namespaces[0]
		}
	}
	if phase := stringFromMap(velero, "phase"); phase != "" {
		patch["phase"] = phase
	}
	if stages := sliceFromAny(velero["recoveryStages"]); len(stages) > 0 {
		patch["recoveryStages"] = stages
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func taskProgressPayloadPatch(payload protocol.TaskProgressPayload) map[string]any {
	patch := backupTaskPayloadPatch(payload.Velero)
	if patch == nil {
		patch = map[string]any{}
	}
	progress := map[string]any{}
	if payload.TotalBytes > 0 {
		progress["totalBytes"] = payload.TotalBytes
		patch["totalBytes"] = payload.TotalBytes
	}
	if payload.SyncedBytes > 0 {
		progress["syncedBytes"] = payload.SyncedBytes
		patch["syncedBytes"] = payload.SyncedBytes
	}
	if payload.SpeedBytesPerSecond > 0 {
		progress["speedBytesPerSecond"] = payload.SpeedBytesPerSecond
		patch["speedBytesPerSecond"] = payload.SpeedBytesPerSecond
	}
	if payload.Percent > 0 {
		progress["percent"] = payload.Percent
		patch["percent"] = payload.Percent
	}
	if payload.EtaSeconds > 0 {
		progress["etaSeconds"] = payload.EtaSeconds
		patch["etaSeconds"] = payload.EtaSeconds
	}
	if len(progress) > 0 {
		patch["progressMetrics"] = progress
	}
	if payload.SizeProgressV2 != nil {
		patch["sizeProgressV2"] = payload.SizeProgressV2
	}
	// Persistent-data progress ends when readiness validation begins. Payload
	// patches are merged, so explicitly clear the previous 100% transfer sample
	// instead of leaving the UI apparently stuck in Restoring Persistent Data.
	if readinessStage := strings.TrimSpace(fmt.Sprint(payload.Velero["readinessStage"])); readinessStage != "" && readinessStage != "<nil>" {
		patch["sizeProgressV2"] = nil
	}
	volumeProgress := mapFromAny(payload.Velero["volumeProgress"])
	if len(volumeProgress) > 0 {
		patch["volumeProgress"] = volumeProgress
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func taskCompletedPayloadPatch(payload protocol.TaskCompletedPayload) map[string]any {
	patch := backupTaskPayloadPatch(payload.Velero)
	if patch == nil {
		patch = map[string]any{}
	}
	restorePointSize := mapFromAny(payload.Velero["restorePointSize"])
	if len(restorePointSize) == 0 {
		restorePointSize = payload.Size
	}
	if len(restorePointSize) == 0 {
		restorePointSize = mapFromAny(payload.Velero["size"])
	}
	if len(restorePointSize) > 0 {
		patch["restorePointSize"] = restorePointSize
		patch["size"] = restorePointSize
		if totalBytes := int64FromAny(restorePointSize["totalBytes"]); totalBytes > 0 {
			patch["sizeBytes"] = totalBytes
			patch["totalBytes"] = totalBytes
		}
	}
	if planStorageSize := mapFromAny(payload.Velero["planStorageSize"]); len(planStorageSize) > 0 {
		patch["planStorageSize"] = planStorageSize
	}
	if payload.SizeMetricsV2 != nil {
		patch["sizeMetricsV2"] = payload.SizeMetricsV2
	}
	// Final metrics supersede the running sample. Keeping an InProgress phase
	// beside a succeeded task is misleading and can make clients appear stuck.
	patch["sizeProgressV2"] = nil
	if sizeStatus := stringFromMap(payload.Velero, "sizeStatus"); sizeStatus != "" {
		patch["sizeStatus"] = sizeStatus
	}
	if sizeWarnings := sliceFromAny(payload.Velero["sizeWarnings"]); len(sizeWarnings) > 0 {
		patch["sizeWarnings"] = sizeWarnings
	}
	volumeProgress := mapFromAny(payload.Velero["volumeProgress"])
	if len(volumeProgress) > 0 {
		patch["volumeProgress"] = volumeProgress
	} else {
		// Task payloads are merged into the persisted JSON document. Explicitly
		// overwrite the last running sample so a completed task cannot retain an
		// InProgress volumeProgress value from an earlier progress event.
		patch["volumeProgress"] = nil
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func sameVeleroEventPayload(existing map[string]any, incoming map[string]any) bool {
	if len(existing) == 0 || len(incoming) == 0 {
		return false
	}
	existingName := fmt.Sprint(existing["name"])
	incomingName := fmt.Sprint(incoming["name"])
	if existingName == "" || existingName != incomingName {
		return false
	}
	if existingKind := fmt.Sprint(existing["kind"]); existingKind != "" && fmt.Sprint(incoming["kind"]) != "" && existingKind != fmt.Sprint(incoming["kind"]) {
		return false
	}
	existingResourceVersion := fmt.Sprint(existing["resourceVersion"])
	incomingResourceVersion := fmt.Sprint(incoming["resourceVersion"])
	if existingResourceVersion != "" && incomingResourceVersion != "" {
		return existingResourceVersion == incomingResourceVersion
	}
	existingPhase := fmt.Sprint(existing["phase"])
	incomingPhase := fmt.Sprint(incoming["phase"])
	if existingPhase != incomingPhase {
		return false
	}
	if isTerminalVeleroPhase(incomingPhase) {
		return true
	}
	existingVolume := mapFromAny(existing["volumeProgress"])
	incomingVolume := mapFromAny(incoming["volumeProgress"])
	if len(existingVolume) == 0 || len(incomingVolume) == 0 {
		return false
	}
	return fmt.Sprint(existingVolume["bytesDone"]) == fmt.Sprint(incomingVolume["bytesDone"]) &&
		fmt.Sprint(existingVolume["totalBytes"]) == fmt.Sprint(incomingVolume["totalBytes"]) &&
		fmt.Sprint(existingVolume["knownTotal"]) == fmt.Sprint(incomingVolume["knownTotal"])
}

func isTerminalVeleroPhase(phase string) bool {
	switch phase {
	case "Completed", "PartiallyFailed", "Failed", "FailedValidation", "Canceled":
		return true
	default:
		return false
	}
}

func (r *Router) findOrCreateVeleroBackupTask(clusterID string, plan store.ProtectionPlan, event protocol.VeleroEventPayload) (store.Task, error) {
	if strings.TrimSpace(event.TaskID) == "" {
		return store.Task{}, errors.New("velero event missing task id")
	}
	task, found, err := r.store.GetTask(event.TaskID)
	if err != nil {
		return store.Task{}, err
	}
	if !found || task.ClusterID != clusterID || task.ProtectionPlanID != plan.ID || task.Type != "backup" {
		return store.Task{}, errors.New("velero event task does not belong to this cluster and protection plan")
	}
	if event.CommandID != "" && event.CommandID != task.CommandID {
		return store.Task{}, errors.New("velero event command id does not match task")
	}
	if label := event.Labels["hypercdr.io/task-id"]; label != "" && label != task.ID {
		return store.Task{}, errors.New("velero event task id conflicts with resource label")
	}
	if name := taskPayloadString(task.Payload, "veleroBackupName"); name != "" && name != event.BackupName {
		return store.Task{}, errors.New("velero event backup name does not match task")
	}
	return task, nil
}

func (r *Router) findVeleroBackupTask(clusterID string, backupName string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.Type != "backup" {
			continue
		}
		if taskPayloadString(task.Payload, "veleroBackupName") == backupName {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) findTaskByID(clusterID string, taskID string) (store.Task, bool, error) {
	if taskID == "" {
		return store.Task{}, false, nil
	}
	tasks, err := r.store.ListTasks(clusterID)
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

func (r *Router) finishBackupCancelTask(clusterID string, cancelTask store.Task, completed protocol.TaskCompletedPayload) error {
	patch := taskCompletedPayloadPatch(completed)
	cancelTask, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:      cancelTask.ID,
		Status:      "succeeded",
		Progress:    100,
		Payload:     patch,
		MarkStarted: true,
		MarkDone:    true,
	})
	if err != nil {
		return err
	}
	_ = r.addTaskEventIfChanged(store.TaskEventInput{
		TaskID:  cancelTask.ID,
		Level:   "info",
		Reason:  "completed",
		Message: completed.Message,
		Payload: map[string]any{"velero": completed.Velero},
	})
	targetTaskID := firstNonEmptyString(stringPayload(cancelTask.Payload, "targetTaskId"), stringPayload(completed.Velero, "targetTaskId"))
	if targetTaskID == "" {
		return errors.New("backup cancel target task id is missing")
	}
	target, ok, err := r.findTaskByID(clusterID, targetTaskID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("backup cancel target task not found")
	}
	if target.Type != "backup" {
		return fmt.Errorf("backup cancel target has unsupported task type %q", target.Type)
	}
	if !target.CompletedAt.IsZero() {
		return nil
	}
	targetPatch := map[string]any{
		"cancelTaskId":     cancelTask.ID,
		"cancelReason":     firstNonEmptyString(stringPayload(cancelTask.Payload, "reason"), "user_requested"),
		"canceledByUser":   true,
		"veleroBackupName": firstNonEmptyString(stringPayload(cancelTask.Payload, "veleroBackupName"), stringPayload(completed.Velero, "backupName")),
	}
	if deleted, ok := completed.Velero["deleted"].(bool); ok {
		targetPatch["veleroBackupDeleted"] = deleted
	}
	_, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       target.ID,
		Status:       "canceled",
		Progress:     target.Progress,
		ErrorCode:    "SYNC_FORCE_STOPPED",
		ErrorMessage: "Sync was force stopped by user.",
		Payload:      targetPatch,
		MarkDone:     true,
	})
	if err != nil {
		return err
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  target.ID,
		Level:   "warning",
		Reason:  "canceled",
		Message: "Sync was force stopped by user.",
		Payload: map[string]any{"cancelTaskId": cancelTask.ID, "velero": completed.Velero},
	})
	return nil
}

func (r *Router) markBackupCancelFailed(cancelTask store.Task, message string) {
	targetTaskID := stringPayload(cancelTask.Payload, "targetTaskId")
	if targetTaskID == "" {
		return
	}
	target, ok, err := r.findTaskByID(cancelTask.ClusterID, targetTaskID)
	if err != nil || !ok || target.Type != "backup" || !target.CompletedAt.IsZero() {
		return
	}
	if target.Status != "canceling" {
		return
	}
	if message == "" {
		message = "Force stop failed."
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       target.ID,
		Status:       "running",
		Progress:     target.Progress,
		ErrorCode:    "SYNC_FORCE_STOP_FAILED",
		ErrorMessage: message,
		Payload: map[string]any{
			"cancelTaskId": cancelTask.ID,
			"cancelFailed": true,
		},
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  target.ID,
		Level:   "error",
		Reason:  "cancel_failed",
		Message: message,
		Payload: map[string]any{"cancelTaskId": cancelTask.ID},
	})
}

func isForceStoppedBackupTask(task store.Task) bool {
	status := strings.ToLower(task.Status)
	return status == "canceling" || status == "canceled" || strings.EqualFold(task.ErrorCode, "SYNC_FORCE_STOPPED")
}

func (r *Router) createRestorePointFromVeleroEvent(clusterID string, plan store.ProtectionPlan, task store.Task, event protocol.VeleroEventPayload) (store.RestorePoint, error) {
	completedAt := event.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	sourceNamespace := firstNonEmptyString(event.Labels["hypercdr.io/source-namespace"], firstStringFromStrings(event.IncludedNamespaces), taskPayloadString(task.Payload, "sourceNamespace"))
	scheduled := taskPayloadBool(task.Payload, "scheduled")
	point, err := r.store.CreateRestorePoint(store.RestorePointInput{
		ProtectionPlanID:  plan.ID,
		SourceClusterID:   clusterID,
		AppID:             plan.AppID,
		StorageRepoID:     plan.StorageRepoID,
		TaskCreatedAt:     task.CreatedAt,
		VeleroBackupName:  event.BackupName,
		PointType:         "backup",
		Status:            "available",
		SizeBytes:         veleroBackupSizeBytes(event.Velero),
		CompletedAt:       completedAt,
		SourceNamespace:   sourceNamespace,
		BackupTaskID:      task.ID,
		BackupStorageName: event.StorageLocation,
		Metadata: map[string]any{
			"scheduled":          scheduled,
			"phase":              event.Phase,
			"velero":             event.Velero,
			"size":               firstNonEmptyMap(mapFromAny(event.Velero["restorePointSize"]), mapFromAny(event.Velero["size"])),
			"sizeStatus":         event.Velero["sizeStatus"],
			"sizeWarnings":       sliceFromAny(event.Velero["sizeWarnings"]),
			"restorePointSize":   firstNonEmptyMap(mapFromAny(event.Velero["restorePointSize"]), mapFromAny(event.Velero["size"])),
			"planStorageSize":    mapFromAny(event.Velero["planStorageSize"]),
			"includedNamespaces": event.IncludedNamespaces,
			"sourceNamespaces":   event.IncludedNamespaces,
		},
	})
	if err == nil {
		r.scheduleRestorePointContentIndex(point)
	}
	return point, err
}

func (r *Router) createRestorePointFromBackup(task store.Task, veleroPayload map[string]any) (store.RestorePoint, error) {
	if task.ProtectionPlanID == "" {
		return store.RestorePoint{}, errors.New("backup restore point requires protection plan id")
	}
	kind, _ := veleroPayload["kind"].(string)
	if kind != "Backup" {
		return store.RestorePoint{}, nil
	}
	backupName, _ := veleroPayload["name"].(string)
	if backupName == "" {
		return store.RestorePoint{}, nil
	}
	storageRepoID := ""
	if task.ProtectionPlanID != "" {
		plan, ok, err := r.store.GetProtectionPlan(task.ProtectionPlanID)
		if err != nil {
			return store.RestorePoint{}, err
		}
		if ok && plan.SourceClusterID != task.ClusterID {
			return store.RestorePoint{}, nil
		}
		if ok {
			storageRepoID = plan.StorageRepoID
		}
	}

	sourceNamespace, _ := task.Payload["sourceNamespace"].(string)
	labelSelector, _ := task.Payload["labelSelector"].(string)
	storageName, _ := task.Payload["storageRepo"].(string)
	if manifest, ok := veleroPayload["manifest"].(map[string]any); ok {
		if spec, ok := manifest["spec"].(map[string]any); ok {
			if value, ok := spec["storageLocation"].(string); ok && value != "" {
				storageName = value
			}
		}
	}
	completedAt := task.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}

	point, err := r.store.CreateRestorePoint(store.RestorePointInput{
		ProtectionPlanID:  task.ProtectionPlanID,
		SourceClusterID:   task.ClusterID,
		AppID:             task.AppID,
		StorageRepoID:     storageRepoID,
		TaskCreatedAt:     task.CreatedAt,
		VeleroBackupName:  backupName,
		PointType:         "backup",
		Status:            "available",
		SizeBytes:         veleroBackupSizeBytes(veleroPayload),
		CompletedAt:       completedAt,
		SourceNamespace:   sourceNamespace,
		LabelSelector:     labelSelector,
		BackupTaskID:      task.ID,
		BackupStorageName: storageName,
		Metadata: map[string]any{
			"velero":           veleroPayload,
			"sizeMetricsV2":    task.Payload["sizeMetricsV2"],
			"size":             firstNonEmptyMap(mapFromAny(veleroPayload["restorePointSize"]), mapFromAny(veleroPayload["size"])),
			"sizeStatus":       veleroPayload["sizeStatus"],
			"sizeWarnings":     sliceFromAny(veleroPayload["sizeWarnings"]),
			"restorePointSize": firstNonEmptyMap(mapFromAny(veleroPayload["restorePointSize"]), mapFromAny(veleroPayload["size"])),
			"planStorageSize":  mapFromAny(veleroPayload["planStorageSize"]),
		},
		SizeMetricsV2: mapFromAnyJSON(task.Payload["sizeMetricsV2"]),
	})
	if err != nil {
		return store.RestorePoint{}, err
	}
	r.scheduleRestorePointContentIndex(point)
	if task.ID != "" && point.ID != "" {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:         task.ID,
			RestorePointID: point.ID,
			Payload: map[string]any{
				"restorePointId": point.ID,
			},
		})
	}
	r.updateProtectionPlanStorageSizeFromVelero(task.ProtectionPlanID, veleroPayload)
	return point, nil
}

func (r *Router) updateProtectionPlanStorageSizeFromVelero(planID string, veleroPayload map[string]any) {
	if planID == "" {
		return
	}
	planStorageSize := mapFromAny(veleroPayload["planStorageSize"])
	if len(planStorageSize) == 0 {
		return
	}
	if _, _, err := r.store.UpdateProtectionPlanStorageSize(planID, planStorageSize); err != nil {
		r.logger.Error("failed to update protection plan storage size", "plan_id", planID, "error", err)
	}
}

func (r *Router) reconcileRetention(planID string, triggerTaskID string) {
	if planID == "" {
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err != nil {
		r.logger.Error("failed to load protection plan for retention", "plan_id", planID, "error", err)
		return
	}
	if !ok || plan.PolicyID == "" {
		return
	}
	policy, ok, err := r.findPolicy(plan.PolicyID)
	if err != nil {
		r.logger.Error("failed to load policy for retention", "plan_id", planID, "policy_id", plan.PolicyID, "error", err)
		return
	}
	if !ok || policy.RetentionCount <= 0 {
		return
	}
	points, err := r.store.ListRestorePoints(store.RestorePointFilter{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: planID,
	})
	if err != nil {
		r.logger.Error("failed to list restore points for retention", "plan_id", planID, "error", err)
		return
	}
	clusterTasks, err := r.store.ListTasks(plan.SourceClusterID)
	if err != nil {
		r.logger.Error("failed to list tasks for retention", "plan_id", planID, "error", err)
		return
	}
	activeRetentionTasks := map[string]struct{}{}
	staleTaskCutoff := time.Now().UTC().Add(-35 * time.Minute)
	for _, task := range clusterTasks {
		if task.Type != "retention-cleanup" || !isActiveTaskStatus(task.Status) {
			continue
		}
		lastActivity := task.StartedAt
		if lastActivity.IsZero() {
			lastActivity = task.AcceptedAt
		}
		if lastActivity.IsZero() {
			lastActivity = task.DispatchedAt
		}
		if lastActivity.IsZero() {
			lastActivity = task.CreatedAt
		}
		if lastActivity.After(staleTaskCutoff) {
			activeRetentionTasks[task.ID] = struct{}{}
		}
	}
	available := make([]store.RestorePoint, 0, len(points))
	for _, point := range points {
		if point.Status != "available" || point.VeleroBackupName == "" {
			continue
		}
		state, _ := point.Metadata["retentionState"].(string)
		if state == "deleting" {
			taskID, _ := point.Metadata["retentionCleanupTask"].(string)
			if _, ok := activeRetentionTasks[taskID]; ok {
				continue
			}
		}
		available = append(available, point)
	}
	if len(available) <= policy.RetentionCount {
		return
	}
	sort.SliceStable(available, func(i, j int) bool {
		left := available[i].CompletedAt
		if left.IsZero() {
			left = available[i].CreatedAt
		}
		right := available[j].CompletedAt
		if right.IsZero() {
			right = available[j].CreatedAt
		}
		return left.After(right)
	})
	candidates := available[policy.RetentionCount:]
	if len(candidates) == 0 {
		return
	}
	restorePoints := make([]map[string]any, 0, len(candidates))
	for _, point := range candidates {
		restorePoints = append(restorePoints, map[string]any{
			"id":               point.ID,
			"taskCreatedAt":    point.TaskCreatedAt,
			"veleroBackupName": point.VeleroBackupName,
			"namespace":        r.dataProtectionNamespaceForCluster(plan.SourceClusterID),
		})
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"retentionState": "pending_delete",
			},
		})
	}
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: plan.ID,
		Type:             "retention-cleanup",
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"planId":        plan.ID,
			"triggerTaskId": triggerTaskID,
			"restorePoints": restorePoints,
		},
	})
	if err != nil {
		r.logger.Error("failed to create retention cleanup task", "plan_id", plan.ID, "error", err)
		return
	}
	for _, point := range candidates {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"retentionState":       "deleting",
				"retentionCleanupTask": task.ID,
			},
		})
	}
	conn, ok := r.hub.get(plan.SourceClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; retention cleanup will be dispatched after reconnect",
		})
		r.markBackupTaskRetentionWarning(triggerTaskID, "RETENTION_CLEANUP_PENDING", "Expired restore points were selected for deletion, but the source cluster agent is offline. Cleanup will retry after reconnect.")
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "failed",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
			MarkDone:     true,
		})
		r.markRetentionCleanupFailed(task, err.Error())
		r.markBackupTaskRetentionWarning(triggerTaskID, "RETENTION_CLEANUP_FAILED", err.Error())
		r.logger.Error("failed to dispatch retention cleanup", "task_id", task.ID, "plan_id", plan.ID, "error", err)
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
}

func (r *Router) createRestorePointDeleteTask(clusterID string, points []store.RestorePoint) (store.Task, string, error) {
	if clusterID == "" || len(points) == 0 {
		return store.Task{}, "", errors.New("restore point delete target required")
	}
	restorePoints := make([]map[string]any, 0, len(points))
	protectionPlanID := ""
	for _, point := range points {
		if protectionPlanID == "" {
			protectionPlanID = point.ProtectionPlanID
		}
		restorePoints = append(restorePoints, map[string]any{
			"id":               point.ID,
			"taskCreatedAt":    point.TaskCreatedAt,
			"veleroBackupName": point.VeleroBackupName,
			"namespace":        r.dataProtectionNamespaceForCluster(clusterID),
		})
	}
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        clusterID,
		ProtectionPlanID: protectionPlanID,
		Type:             "retention-cleanup",
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"planId":        protectionPlanID,
			"manualDelete":  true,
			"restorePoints": restorePoints,
		},
	})
	if err != nil {
		return store.Task{}, "", err
	}
	for _, point := range points {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"retentionState":       "deleting",
				"retentionCleanupTask": task.ID,
				"deleteRequestedAt":    time.Now().UTC().Format(time.RFC3339),
				"deleteRequestedBy":    "manual",
			},
		})
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "manual_restore_point_delete",
		Message: "restore point delete requested",
		Payload: map[string]any{"restorePoints": restorePoints},
	})
	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; restore point delete will be dispatched after reconnect",
		})
		return task, "agent is offline; restore point delete task remains queued", nil
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "failed",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
			MarkDone:     true,
		})
		r.markRetentionCleanupFailed(task, err.Error())
		return task, "restore point delete task created but dispatch failed", nil
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
		Message: "restore point delete task dispatched to agent",
	})
	return task, "", nil
}

func (r *Router) createProtectionCleanupTask(plan store.ProtectionPlan) (store.Task, string, error) {
	if plan.ID == "" || plan.SourceClusterID == "" {
		return store.Task{}, "", errors.New("protection cleanup target required")
	}
	sourceNamespaces, err := r.protectionPlanNamespaces(plan)
	if err != nil {
		return store.Task{}, "", err
	}
	repo, ok, err := r.store.GetStorageRepository(plan.StorageRepoID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok {
		return store.Task{}, "", errors.New("storage repository not found")
	}
	storageName := storageDomainBSLName(repo, plan.SourceClusterID)
	points, err := r.store.ListRestorePoints(store.RestorePointFilter{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: plan.ID,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	restorePoints := make([]map[string]any, 0, len(points))
	for _, point := range points {
		if point.Status == "deleted" || point.VeleroBackupName == "" {
			continue
		}
		restorePoints = append(restorePoints, map[string]any{
			"id":               point.ID,
			"taskCreatedAt":    point.TaskCreatedAt,
			"veleroBackupName": point.VeleroBackupName,
			"namespace":        r.dataProtectionNamespaceForCluster(plan.SourceClusterID),
		})
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"protectionCleanupState": "pending_delete",
				"cleanupRequestedAt":     time.Now().UTC().Format(time.RFC3339),
			},
		})
	}
	restoreNames, err := r.protectionPlanRestoreNames(plan.ID)
	if err != nil {
		return store.Task{}, "", err
	}
	cleanupRunID := store.NewPublicID()
	sourcePayload := map[string]any{
		"planId":                 plan.ID,
		"cleanupRunId":           cleanupRunID,
		"cleanupMode":            "source",
		"scheduleName":           scheduleNameForPlan(plan.ID),
		"backupNamePrefix":       scheduleNameForPlan(plan.ID),
		"namespace":              r.dataProtectionNamespaceForCluster(plan.SourceClusterID),
		"sourceNamespaces":       sourceNamespaces,
		"storageRepo":            storageName,
		"storageRepoDisplayName": repo.Name,
		"sourceClusterId":        plan.SourceClusterID,
		"objectPrefix":           storageDomainPrefix(plan.TenantID, plan.SourceClusterID),
		"cleanupObjectStorage":   true,
		"restorePoints":          restorePoints,
		"restoreNames":           restoreNames,
	}
	task, warning, err := r.createAndDispatchProtectionCleanupTask(plan, plan.SourceClusterID, sourcePayload, "source protection cleanup task dispatched to agent")
	if err != nil {
		return store.Task{}, "", err
	}
	warnings := []string{}
	if warning != "" {
		warnings = append(warnings, warning)
	}
	if plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID {
		targetPayload := map[string]any{
			"planId":                 plan.ID,
			"cleanupRunId":           cleanupRunID,
			"cleanupMode":            "target",
			"backupNamePrefix":       scheduleNameForPlan(plan.ID),
			"namespace":              r.dataProtectionNamespaceForCluster(plan.TargetClusterID),
			"sourceNamespaces":       sourceNamespaces,
			"storageRepo":            storageName,
			"storageRepoDisplayName": repo.Name,
			"sourceClusterId":        plan.SourceClusterID,
			"objectPrefix":           storageDomainPrefix(plan.TenantID, plan.SourceClusterID),
			"cleanupObjectStorage":   false,
			"restorePoints":          restorePoints,
			"restoreNames":           restoreNames,
		}
		_, targetWarning, err := r.createAndDispatchProtectionCleanupTask(plan, plan.TargetClusterID, targetPayload, "target protection cleanup task dispatched to agent")
		if err != nil {
			return store.Task{}, "", err
		}
		if targetWarning != "" {
			warnings = append(warnings, targetWarning)
		}
	}
	return task, strings.Join(warnings, "; "), nil
}

func (r *Router) protectionPlanRestoreNames(planID string) ([]string, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, task := range tasks {
		if task.ProtectionPlanID != planID && stringPayload(task.Payload, "protectionPlanId") != planID && stringPayload(task.Payload, "planId") != planID {
			continue
		}
		switch task.Type {
		case "restore", "drill", "takeover", "failback":
		default:
			continue
		}
		name := strings.TrimSpace(stringPayload(task.Payload, "veleroBackupName"))
		if name != "" {
			names = append(names, name)
		}
	}
	return uniqueNonEmptyStrings(names), nil
}

func (r *Router) createAndDispatchProtectionCleanupTask(plan store.ProtectionPlan, clusterID string, payload map[string]any, dispatchedMessage string) (store.Task, string, error) {
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        clusterID,
		ProtectionPlanID: plan.ID,
		Type:             "protection-cleanup",
		Status:           "queued",
		CommandID:        commandID,
		Payload:          payload,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "protection_cleanup_requested",
		Message: "protection resource cleanup requested",
		Payload: map[string]any{
			"planId":            plan.ID,
			"cleanupMode":       stringPayload(payload, "cleanupMode"),
			"clusterId":         clusterID,
			"scheduleName":      stringPayload(payload, "scheduleName"),
			"restorePointCount": len(retentionRestorePointsFromAny(payload["restorePoints"])),
		},
	})
	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; protection cleanup will be dispatched after reconnect",
		})
		return task, "agent is offline; protection cleanup task remains queued", nil
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		return task, "protection cleanup task created but dispatch failed", nil
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
		Message: dispatchedMessage,
	})
	return task, "", nil
}

func (r *Router) protectionPlanNamespaces(plan store.ProtectionPlan) ([]string, error) {
	sourceNamespaces := []string{}
	appIDs := plan.AppIDs
	if len(appIDs) == 0 && plan.AppID != "" {
		appIDs = []string{plan.AppID}
	}
	for _, appID := range appIDs {
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return nil, err
		}
		if ok && app.Namespace != "" && !slices.Contains(sourceNamespaces, app.Namespace) {
			sourceNamespaces = append(sourceNamespaces, app.Namespace)
		}
	}
	if len(sourceNamespaces) == 0 {
		return nil, errors.New("protection plan has no application namespaces")
	}
	return sourceNamespaces, nil
}

func (r *Router) finishRetentionCleanupTask(task store.Task, veleroPayload map[string]any) {
	deletedRaw, _ := veleroPayload["deleted"].([]any)
	if len(deletedRaw) == 0 {
		if deletedStrings, ok := veleroPayload["deleted"].([]string); ok {
			for _, id := range deletedStrings {
				r.markRestorePointDeleted(id)
			}
			r.clearBackupTaskRetentionWarning(stringPayload(task.Payload, "triggerTaskId"))
			return
		}
	}
	for _, item := range deletedRaw {
		if id, ok := item.(string); ok {
			r.markRestorePointDeleted(id)
		}
	}
	r.clearBackupTaskRetentionWarning(stringPayload(task.Payload, "triggerTaskId"))
}

func (r *Router) finishProtectionCleanupTask(task store.Task, veleroPayload map[string]any) {
	if stringPayload(task.Payload, "cleanupMode") == "drill" {
		return
	}
	planID := firstNonEmptyString(task.ProtectionPlanID, stringPayload(task.Payload, "planId"))
	if planID == "" {
		r.logger.Warn("protection cleanup completed without plan id", "task_id", task.ID)
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err != nil {
		r.logger.Error("failed to load protection plan before final cleanup", "plan_id", planID, "task_id", task.ID, "error", err)
		return
	}
	if !ok {
		r.logger.Warn("protection cleanup completed for missing plan", "plan_id", planID, "task_id", task.ID)
		return
	}
	cleanupRunID := stringPayload(task.Payload, "cleanupRunId")
	complete, err := r.protectionCleanupTasksComplete(plan, cleanupRunID)
	if err != nil {
		r.logger.Error("failed to inspect protection cleanup task completion", "plan_id", planID, "task_id", task.ID, "error", err)
		return
	}
	if !complete {
		r.logger.Info("protection cleanup task completed; waiting for remaining cleanup tasks", "plan_id", planID, "task_id", task.ID, "mode", stringPayload(task.Payload, "cleanupMode"))
		// Source and target agents can finish almost simultaneously. Each event
		// handler may observe the peer task before its succeeded status commits,
		// leaving the plan in cleanup_running forever. Recheck shortly after the
		// concurrent completions settle; CleanupProtectionPlanRecords is
		// idempotent, so either retry may safely close the plan.
		go func(task store.Task) {
			time.Sleep(500 * time.Millisecond)
			plan, ok, err := r.store.GetProtectionPlan(planID)
			if err != nil || !ok {
				return
			}
			complete, err := r.protectionCleanupTasksComplete(plan, cleanupRunID)
			if err != nil || !complete {
				return
			}
			if _, ok, err := r.store.CleanupProtectionPlanRecords(planID); err != nil {
				r.logger.Error("failed to physically cleanup protection plan records after completion race", "plan_id", planID, "task_id", task.ID, "error", err)
				_, _, _ = r.store.UpdateProtectionPlanStatus(planID, "cleanup_failed")
			} else if !ok {
				r.logger.Warn("protection cleanup completion race resolved for missing plan", "plan_id", planID, "task_id", task.ID)
			}
		}(task)
		return
	}
	if _, ok, err := r.store.CleanupProtectionPlanRecords(planID); err != nil {
		r.logger.Error("failed to physically cleanup protection plan records", "plan_id", planID, "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateProtectionPlanStatus(planID, "cleanup_failed")
	} else if !ok {
		r.logger.Warn("protection cleanup completed for missing plan", "plan_id", planID, "task_id", task.ID)
	}
}

func (r *Router) protectionCleanupTasksComplete(plan store.ProtectionPlan, cleanupRunID string) (bool, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return false, err
	}
	expectTarget := plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID
	sourceDone := false
	targetDone := !expectTarget
	var latestSource, latestTarget *store.Task
	for _, item := range tasks {
		if item.ProtectionPlanID != plan.ID || item.Type != "protection-cleanup" {
			continue
		}
		// Retries create a new cleanup run. Historical failed tasks must not
		// prevent a later successful run from finalizing the plan.
		if cleanupRunID != "" && stringPayload(item.Payload, "cleanupRunId") != cleanupRunID {
			continue
		}
		mode := stringPayload(item.Payload, "cleanupMode")
		if mode == "" {
			mode = "source"
		}
		if cleanupRunID == "" {
			candidate := item
			if mode == "target" {
				if latestTarget == nil || candidate.CreatedAt.After(latestTarget.CreatedAt) {
					latestTarget = &candidate
				}
			} else if latestSource == nil || candidate.CreatedAt.After(latestSource.CreatedAt) {
				latestSource = &candidate
			}
			continue
		}
		if item.Status != "succeeded" {
			return false, nil
		}
		if mode == "target" {
			targetDone = true
		} else {
			sourceDone = true
		}
	}
	if cleanupRunID == "" {
		sourceDone = latestSource != nil && latestSource.Status == "succeeded"
		targetDone = !expectTarget || (latestTarget != nil && latestTarget.Status == "succeeded")
	}
	return sourceDone && targetDone, nil
}

func (r *Router) markRestorePointDeleted(id string) {
	if id == "" {
		return
	}
	_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
		ID:     id,
		Status: "deleted",
		Metadata: map[string]any{
			"retentionState": "deleted",
			"deletedAt":      time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (r *Router) markRetentionCleanupFailed(task store.Task, message string) {
	command := retentionCleanupCommandFromPayload(task.Payload)
	for _, point := range command.RestorePoints {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: "available",
			Metadata: map[string]any{
				"retentionState": "delete_failed",
				"deleteError":    message,
			},
		})
	}
	r.markBackupTaskRetentionWarning(stringPayload(task.Payload, "triggerTaskId"), "RETENTION_CLEANUP_FAILED", message)
}

func (r *Router) markProtectionCleanupFailed(task store.Task, message string) {
	planID := firstNonEmptyString(task.ProtectionPlanID, stringPayload(task.Payload, "planId"))
	if planID != "" {
		if _, _, err := r.store.UpdateProtectionPlanStatus(planID, "cleanup_failed"); err != nil {
			r.logger.Error("failed to mark protection plan cleanup failed", "plan_id", planID, "task_id", task.ID, "error", err)
		}
	}
	command := protectionCleanupCommandFromPayload(task.Payload)
	for _, point := range command.RestorePoints {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: "available",
			Metadata: map[string]any{
				"protectionCleanupState": "delete_failed",
				"cleanupError":           message,
			},
		})
	}
}

func (r *Router) markBackupTaskRetentionWarning(taskID string, code string, message string) {
	if taskID == "" {
		return
	}
	if message == "" {
		message = "Expired restore point cleanup did not complete."
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       taskID,
		ErrorCode:    code,
		ErrorMessage: message,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  taskID,
		Level:   "warning",
		Reason:  code,
		Message: message,
	})
}

func (r *Router) clearBackupTaskRetentionWarning(taskID string) {
	if taskID == "" {
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: taskID})
}

func newRejectedMessage(reason string, message string) protocol.Message[protocol.RegisterRejectedPayload] {
	return protocol.Message[protocol.RegisterRejectedPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformRegisterRejected,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.RegisterRejectedPayload{
			Reason:    reason,
			ErrorCode: reason,
			Message:   message,
		},
	}
}
