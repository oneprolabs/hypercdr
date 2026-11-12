package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrCacheConfigurationBusy = errors.New("cluster cache configuration conflicts with an active task")

func (s *PostgresStore) CreateTask(input TaskInput) (Task, error) {
	if input.Type == "" {
		input.Type = "backup"
	}
	if input.Status == "" {
		input.Status = "queued"
	}
	if input.Payload == nil {
		input.Payload = map[string]any{}
	}
	payloadRaw, err := json.Marshal(input.Payload)
	if err != nil {
		return Task{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	tenantID := input.TenantID
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if input.ProtectionPlanID != "" {
		var planStatus string
		// Locking the plan serializes same-plan task creation and makes insertion
		// plus pointer publication one atomic ordering boundary.
		if scanErr := tx.QueryRow(`select tenant_id from protection_plans where id=$1 for update`, input.ProtectionPlanID).Scan(&tenantID); scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				return Task{}, fmt.Errorf("protection plan %s not found while creating %s task", input.ProtectionPlanID, input.Type)
			}
			return Task{}, scanErr
		}
		if err := tx.QueryRow(`select status from protection_plans where id=$1`, input.ProtectionPlanID).Scan(&planStatus); err != nil {
			return Task{}, err
		}
		if (planStatus == "cleanup_running" || planStatus == "cleaning" || planStatus == "cleanup_failed") && input.Type != "protection-cleanup" {
			return Task{}, ErrProtectionPlanBusy
		}
	} else if input.ClusterID != "" {
		_ = tx.QueryRow(`select tenant_id from clusters where id=$1`, input.ClusterID).Scan(&tenantID)
	}
	if input.TenantID != "" && input.TenantID != tenantID {
		return Task{}, ErrTenantResourceMismatch
	}
	if err := validateTaskReferences(tx, input, tenantID); err != nil {
		return Task{}, err
	}
	if parentID, _ := input.Payload["cacheSettingsTaskId"].(string); input.Type == "cache-cleanup" && parentID != "" {
		if _, err := tx.Exec(`select pg_advisory_xact_lock(hashtextextended($1,0))`, "cache-cleanup-parent:"+parentID); err != nil {
			return Task{}, err
		}
		var existingID string
		err := tx.QueryRow(`select id::text from tasks where tenant_id=$1 and cluster_id=$2 and type='cache-cleanup' and payload->>'cacheSettingsTaskId'=$3 order by created_at limit 1`, tenantID, input.ClusterID, parentID).Scan(&existingID)
		if err == nil {
			if err := tx.Commit(); err != nil {
				return Task{}, err
			}
			existing, found, err := s.GetTask(existingID)
			if err == nil && !found {
				return Task{}, fmt.Errorf("cache cleanup task disappeared")
			}
			return existing, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Task{}, err
		}
	}
	cacheUser := input.Type == "backup" || input.Type == "restore" || input.Type == "drill" || input.Type == "takeover" || input.Type == "failback" || input.Type == "storage-sync" || input.Type == "schedule-sync"
	if input.ClusterID != "" && (cacheUser || input.Type == "cache-settings") {
		if _, err := tx.Exec(`select pg_advisory_xact_lock(hashtextextended($1,0))`, "cache-configuration:"+input.ClusterID); err != nil {
			return Task{}, err
		}
		var busy bool
		if err := tx.QueryRow(`select exists(select 1 from tasks where cluster_id=$1 and status not in ('succeeded','failed','canceled','cancelled') and (type='cache-settings' or ($2 and type in ('backup','restore','drill','takeover','failback','storage-sync','schedule-sync'))))`, input.ClusterID, input.Type == "cache-settings").Scan(&busy); err != nil {
			return Task{}, err
		}
		if busy {
			return Task{}, ErrCacheConfigurationBusy
		}
	}
	if input.Type == "backup" && input.ProtectionPlanID != "" && !input.SuppressLatestPointer {
		var busy bool
		if err := tx.QueryRow(`select exists(select 1 from tasks where protection_plan_id=$1 and type='backup' and status not in ('succeeded','failed','canceled','cancelled')) or exists(select 1 from restore_point_reservations where protection_plan_id=$1 and state in ('data_ready','deleting'))`, input.ProtectionPlanID).Scan(&busy); err != nil {
			return Task{}, err
		}
		if busy {
			return Task{}, ErrProtectionPlanBusy
		}
	}
	if input.Type == "backup-cancel" {
		target, _ := input.Payload["targetTaskId"].(string)
		var irreversible bool
		if err := tx.QueryRow(`select exists(select 1 from restore_point_reservations where task_id=nullif($1,'')::uuid and state in ('data_ready','deleting','published'))`, target).Scan(&irreversible); err != nil {
			return Task{}, err
		}
		if irreversible {
			return Task{}, ErrRestorePointLocked
		}
	}
	pointID := input.RestorePointID
	if pointID == "" {
		pointID, _ = input.Payload["restorePointId"].(string)
	}
	if pointID != "" && (input.Type == "drill" || input.Type == "restore" || input.Type == "takeover") {
		var pointPlan, state string
		var timestamp time.Time
		if err := tx.QueryRow(`select coalesce(protection_plan_id::text,''),coalesce(metadata->>'retentionState',''),coalesce(task_created_at,created_at) from restore_points where id=$1 and tenant_id=$2 and status='available'`, pointID, tenantID).Scan(&pointPlan, &state, &timestamp); err != nil {
			return Task{}, err
		}
		if pointPlan != input.ProtectionPlanID {
			if err := quotaPlanLock(tx, pointPlan); err != nil {
				return Task{}, err
			}
			if err := tx.QueryRow(`select coalesce(metadata->>'retentionState',''),coalesce(task_created_at,created_at) from restore_points where id=$1 and tenant_id=$2 and status='available'`, pointID, tenantID).Scan(&state, &timestamp); err != nil {
				return Task{}, err
			}
		}
		busy, err := pointBusy(tx, pointID)
		if err != nil {
			return Task{}, err
		}
		if busy || state == "deleting" || state == "pending_delete" {
			return Task{}, ErrRestorePointLocked
		}
		input.Payload["restorePointTaskCreatedAt"] = timestamp.UTC().Format(time.RFC3339Nano)
		payloadRaw, err = json.Marshal(input.Payload)
		if err != nil {
			return Task{}, err
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := Task{
		ID:               newID(),
		TenantID:         tenantID,
		ClusterID:        input.ClusterID,
		AppID:            input.AppID,
		ProtectionPlanID: input.ProtectionPlanID,
		RestorePointID:   input.RestorePointID,
		Type:             input.Type,
		Status:           input.Status,
		CommandID:        input.CommandID,
		Payload:          input.Payload,
		CreatedAt:        now,
	}
	if task.Payload == nil {
		task.Payload = map[string]any{}
	}
	_, err = tx.Exec(`
		insert into tasks (
			id, tenant_id, cluster_id, app_id, protection_plan_id, restore_point_id, type, status,
			progress, command_id, payload, created_at
		)
		values ($1, $2, nullif($3, '')::uuid, nullif($4, '')::uuid, nullif($5, '')::uuid, nullif($6, '')::uuid,
			$7, $8, 0, nullif($9, '')::uuid, $10, $11)
	`, task.ID, task.TenantID, task.ClusterID, task.AppID, task.ProtectionPlanID, task.RestorePointID,
		task.Type, task.Status, task.CommandID, payloadRaw, now)
	if err != nil {
		return Task{}, err
	}
	if task.ProtectionPlanID != "" && !input.SuppressLatestPointer {
		if err := reserveBackupQuota(tx, task); err != nil {
			if !errors.Is(err, ErrRestorePointQuota) {
				return Task{}, err
			}
			task.Status = "failed"
			task.ErrorCode = "RESTORE_POINT_QUOTA_EXHAUSTED"
			task.ErrorMessage = err.Error()
			task.CompletedAt = now
			if _, err := tx.Exec(`update tasks set status='failed',error_code=$2,error_message=$3,completed_at=$4 where id=$1`, task.ID, task.ErrorCode, task.ErrorMessage, now); err != nil {
				return Task{}, err
			}
		}
		column := ""
		switch task.Type {
		case "backup":
			column = "latest_sync_task_id"
		case "drill", "restore", "takeover":
			column = "latest_recovery_task_id"
		}
		if column != "" {
			result, updateErr := tx.Exec(`update protection_plans set `+column+`=$2 where id=$1`, task.ProtectionPlanID, task.ID)
			if updateErr != nil {
				return Task{}, updateErr
			}
			if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
				return Task{}, affectedErr
			} else if affected != 1 {
				return Task{}, fmt.Errorf("protection plan %s not found while creating %s task", task.ProtectionPlanID, task.Type)
			}
		}
	}
	if err := prepareRestorePointDeletion(tx, task); err != nil {
		return Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *PostgresStore) ClaimQueuedTask(taskType string, executorID string) (Task, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRow(`select id from tasks where type=$1 and status='queued' order by created_at asc limit 1 for update skip locked`, taskType).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	result, err := tx.Exec(`update tasks set status='running', accepted_at=coalesce(accepted_at, now()), started_at=coalesce(started_at, now()), payload=coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb) || jsonb_build_object('executorId',$2::text,'stage','preparing') where id=$1::uuid and status='queued'`, id, executorID)
	if err != nil {
		return Task{}, false, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Task{}, false, nil
	}
	if err = tx.Commit(); err != nil {
		return Task{}, false, err
	}
	task, ok, err := s.GetTask(id)
	return task, ok, err
}

func (s *PostgresStore) ClaimQueuedTaskByID(taskID string, taskType string, executorID string) (Task, bool, error) {
	result, err := s.db.Exec(`update tasks set status='running', accepted_at=coalesce(accepted_at, now()), started_at=coalesce(started_at, now()), payload=coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb) || jsonb_build_object('executorId',$3::text,'stage','preparing') where id=$1::uuid and type=$2::text and status='queued'`, taskID, taskType, executorID)
	if err != nil {
		return Task{}, false, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Task{}, false, nil
	}
	task, ok, err := s.GetTask(taskID)
	return task, ok, err
}

func (s *PostgresStore) ListTasks(clusterID string) ([]Task, error) {
	return s.listTasks(TaskFilter{ClusterID: clusterID})
}

func (s *PostgresStore) ListTasksFiltered(filter TaskFilter) ([]Task, error) {
	return s.listTasks(filter)
}

func (s *PostgresStore) GetTask(id string) (Task, bool, error) {
	return s.getTask(id)
}

func (s *PostgresStore) listTasks(filter TaskFilter) ([]Task, error) {
	payloadExpr := "payload"
	if filter.Summary {
		payloadExpr = `jsonb_strip_nulls(jsonb_build_object(
			'namespace',payload->'namespace','sourceNamespace',payload->'sourceNamespace',
			'targetNamespace',payload->'targetNamespace','targetNamespaces',payload->'targetNamespaces',
			'targetClusterName',payload->'targetClusterName',
			'applicationName',payload->'applicationName','stage',payload->'stage',
			'recoveryStages',payload->'recoveryStages',
			'archivedClusterId',payload->'archivedClusterId','archivedClusterName',payload->'archivedClusterName',
			'restorePointId',payload->'restorePointId','archivedRestorePointId',payload->'archivedRestorePointId',
			'pointId',payload->'pointId','veleroBackupName',payload->'veleroBackupName','backupName',payload->'backupName',
			'storageRepoId',payload->'storageRepoId','repositoryId',payload->'repositoryId',
			'storageRepo',payload->'storageRepo','backupStorageName',payload->'backupStorageName',
			'storageLocation',payload->'storageLocation','repository',payload->'repository','name',payload->'name'))`
	}
	query := `
		select id, tenant_id, coalesce(cluster_id::text, ''), coalesce(app_id::text, ''), coalesce(protection_plan_id::text, ''),
		       coalesce(restore_point_id::text, ''), type, status, progress, coalesce(command_id::text, ''),
		       coalesce(error_code, ''), coalesce(error_message, ''), ` + payloadExpr + `,
		       created_at, coalesce(dispatched_at, '0001-01-01'::timestamptz),
		       coalesce(accepted_at, '0001-01-01'::timestamptz),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz)
		from tasks
	`
	args := []any{}
	conditions := []string{}
	if filter.CapacityTrigger != "" {
		args = append(args, filter.CapacityTrigger)
		conditions = append(conditions, fmt.Sprintf("payload->>'trigger' = $%d", len(args)))
	}
	if filter.CapacityScheduleDay != "" {
		args = append(args, filter.CapacityScheduleDay)
		conditions = append(conditions, fmt.Sprintf("payload->>'scheduleDay' = $%d", len(args)))
	}
	if filter.CapacityPlanID != "" {
		args = append(args, filter.CapacityPlanID)
		conditions = append(conditions, fmt.Sprintf("payload->'planCapacity'->'plans' @> jsonb_build_array(jsonb_build_object('planId', $%d::text))", len(args)))
	}
	for _, ref := range []struct{ field, value string }{
		{"ownerId", filter.RegistrationOwnerID}, {"idempotencyKey", filter.RegistrationKey},
		{"sessionId", filter.RegistrationSessionID},
	} {
		if ref.value != "" {
			args = append(args, ref.value)
			conditions = append(conditions, fmt.Sprintf("payload->>'%s' = $%d", ref.field, len(args)))
		}
	}
	if filter.TenantID != "" {
		args = append(args, filter.TenantID)
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if filter.ClusterID != "" {
		args = append(args, filter.ClusterID)
		conditions = append(conditions, fmt.Sprintf("cluster_id = $%d", len(args)))
	}
	if filter.ProtectionPlanID != "" {
		args = append(args, filter.ProtectionPlanID)
		conditions = append(conditions, fmt.Sprintf("protection_plan_id = $%d", len(args)))
	}
	if len(filter.Types) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, value := range filter.Types {
			args = append(args, value)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		conditions = append(conditions, "type in ("+strings.Join(placeholders, ",")+")")
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, value := range filter.Statuses {
			args = append(args, value)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		conditions = append(conditions, "status in ("+strings.Join(placeholders, ",")+")")
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += ` order by created_at desc`
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" limit $%d", len(args))
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Task
	for rows.Next() {
		var item Task
		var clusterID sql.NullString
		var payloadRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &clusterID, &item.AppID,
			&item.ProtectionPlanID, &item.RestorePointID, &item.Type, &item.Status, &item.Progress,
			&item.CommandID, &item.ErrorCode, &item.ErrorMessage, &payloadRaw, &item.CreatedAt,
			&item.DispatchedAt, &item.AcceptedAt, &item.StartedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		if clusterID.Valid {
			item.ClusterID = clusterID.String
		}
		_ = json.Unmarshal(payloadRaw, &item.Payload)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateTaskStatus(input TaskStatusInput) (Task, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Task{}, false, err
	}
	defer tx.Rollback()
	var planID string
	if err := tx.QueryRow(`select coalesce(protection_plan_id::text,'') from tasks where id=$1`, input.TaskID).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, false, nil
		}
		return Task{}, false, err
	}
	if err := quotaPlanLock(tx, planID); err != nil {
		return Task{}, false, err
	}
	if input.MarkDone && input.Status != "succeeded" {
		var publishing bool
		if err := tx.QueryRow(`select exists(select 1 from restore_point_reservations rr join tasks t on t.id=rr.task_id where rr.task_id=$1 and rr.state in ('data_ready','deleting','published') and t.completed_at is null)`, input.TaskID).Scan(&publishing); err != nil {
			return Task{}, false, err
		}
		if publishing {
			input.Status = "finalizing"
			input.MarkDone = false
		}
	}
	if input.RestorePointID != "" {
		var tenantID string
		var completedAt sql.NullTime
		if err := tx.QueryRow(`select tenant_id, completed_at from tasks where id=$1 for update`, input.TaskID).Scan(&tenantID, &completedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Task{}, false, nil
			}
			return Task{}, false, err
		}
		if !completedAt.Valid {
			if err := validateTenantReference(tx, "restore_points", input.RestorePointID, tenantID); err != nil {
				return Task{}, false, err
			}
		}
	}
	now := time.Now().UTC()
	payload := make(map[string]any, len(input.Payload)+1)
	for key, value := range input.Payload {
		payload[key] = value
	}
	// Persist platform-observed task activity independently of progress. Some
	// restore phases legitimately report the same percentage repeatedly, and
	// an agent restart can otherwise leave the platform with no reliable way
	// to distinguish a slow task from an abandoned one.
	payload["lastStatusAt"] = now.Format(time.RFC3339Nano)
	payloadRaw := []byte("{}")
	if payload != nil {
		var err error
		payloadRaw, err = json.Marshal(payload)
		if err != nil {
			return Task{}, false, err
		}
	}
	result, err := tx.Exec(`
		update tasks
		set status = case
		        when completed_at is not null then status
		        else coalesce(nullif($2, ''), status)
		    end,
		    restore_point_id = case when completed_at is not null then restore_point_id else coalesce(nullif($11, '')::uuid, restore_point_id) end,
		    progress = greatest(progress, $3),
		    error_code = case when completed_at is not null then error_code else nullif($4, '') end,
		    error_message = case when completed_at is not null then error_message else nullif($5, '') end,
		    payload = coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb) || coalesce($10::jsonb, '{}'::jsonb),
		    accepted_at = case when $6 then coalesce(accepted_at, $9) else accepted_at end,
		    started_at = case when $7 then coalesce(started_at, $9) else started_at end,
		    completed_at = case when $8 then coalesce(completed_at, $9) else completed_at end,
		    dispatched_at = case when $2 = 'dispatched' then coalesce(dispatched_at, $9) else dispatched_at end
		where id = $1
	`, input.TaskID, input.Status, input.Progress, input.ErrorCode, input.ErrorMessage,
		input.MarkAccepted, input.MarkStarted, input.MarkDone, now, payloadRaw, input.RestorePointID)
	if err != nil {
		return Task{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Task{}, false, err
	}
	if input.MarkDone || input.Status == "failed" || input.Status == "canceled" || input.Status == "cancelled" {
		if _, err := tx.Exec(`update restore_points set metadata=metadata-'replacementTaskId' where id in (select replacement_point_id from restore_point_reservations rr join tasks t on t.id=rr.task_id where rr.task_id=$1 and rr.state='reserved' and t.status in ('failed','canceled','cancelled'))`, input.TaskID); err != nil {
			return Task{}, false, err
		}
		if _, err := tx.Exec(`update restore_point_reservations set state='released',updated_at=now() where task_id=$1 and state='reserved' and exists(select 1 from tasks where id=$1 and status in ('failed','canceled','cancelled'))`, input.TaskID); err != nil {
			return Task{}, false, err
		}
		if _, err := tx.Exec(`update restore_points set metadata=metadata || jsonb_build_object('drillRetained', id=(select coalesce(restore_point_id,nullif(payload->>'restorePointId','')::uuid) from tasks where id=$1)) where protection_plan_id::text=$2 and exists(select 1 from tasks where id=$1 and type='drill' and status='succeeded' and completed_at=$3)`, input.TaskID, planID, now); err != nil {
			return Task{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Task{}, false, err
	}
	if affected == 0 {
		return Task{}, false, nil
	}
	task, ok, err := s.getTask(input.TaskID)
	return task, ok, err
}

func (s *PostgresStore) AddTaskEvent(input TaskEventInput) error {
	payloadRaw, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	_, err = tx.Exec(`
		insert into task_events (id, task_id, level, reason, message, payload, created_at)
		values ($1, $2, $3, $4, $5, $6, $7)
	`, newID(), input.TaskID, input.Level, input.Reason, input.Message, payloadRaw, now)
	if err != nil {
		return err
	}
	if s.diagnosticWriter == nil {
		_, err = tx.Exec(`insert into diagnostic_logs(id,tenant_id,scope,level,component,operation,message,cluster_id,task_id,command_id,error_code,status,details,created_at)
			select $1,t.tenant_id,'tenant',$2,'task',t.type,$3,t.cluster_id,t.id,t.command_id,nullif($4,''),t.status,$5,$6 from tasks t where t.id=$7`, newID(), normalizeDiagnosticLevel(input.Level), input.Message, input.Reason, payloadRaw, now, input.TaskID)
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.diagnosticWriter != nil {
		task, found, taskErr := s.getTask(input.TaskID)
		if taskErr != nil || !found {
			return taskErr
		}
		_, err = s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: task.TenantID, Scope: "tenant", Level: input.Level, Component: "task", Operation: task.Type, Message: input.Message, ClusterID: task.ClusterID, TaskID: task.ID, CommandID: task.CommandID, ErrorCode: input.Reason, Status: task.Status, Details: input.Payload, EventAt: now})
	}
	return err
}
