package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
		// Locking the plan serializes same-plan task creation and makes insertion
		// plus pointer publication one atomic ordering boundary.
		if scanErr := tx.QueryRow(`select tenant_id from protection_plans where id=$1 for update`, input.ProtectionPlanID).Scan(&tenantID); scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				return Task{}, fmt.Errorf("protection plan %s not found while creating %s task", input.ProtectionPlanID, input.Type)
			}
			return Task{}, scanErr
		}
	} else if input.ClusterID != "" {
		_ = tx.QueryRow(`select tenant_id from clusters where id=$1`, input.ClusterID).Scan(&tenantID)
	}
	if input.TenantID != "" && input.TenantID != tenantID {
		return Task{}, ErrTenantResourceMismatch
	}
	if input.ClusterID != "" {
		var valid bool
		if err := tx.QueryRow(`select exists(select 1 from clusters where id=$1 and tenant_id=$2)`, input.ClusterID, tenantID).Scan(&valid); err != nil {
			return Task{}, err
		}
		if !valid {
			return Task{}, ErrTenantResourceMismatch
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
	result, err := s.db.Exec(`
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
