package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (s *PostgresStore) CreateRestorePoint(input RestorePointInput) (RestorePoint, error) {
	now := time.Now().UTC()
	if input.PointType == "" {
		input.PointType = "backup"
	}
	if input.Status == "" {
		input.Status = "available"
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if input.SourceNamespace != "" {
		metadata["sourceNamespace"] = input.SourceNamespace
	}
	if input.LabelSelector != "" {
		metadata["labelSelector"] = input.LabelSelector
	}
	if input.BackupTaskID != "" {
		metadata["backupTaskId"] = input.BackupTaskID
	}
	if input.BackupStorageName != "" {
		metadata["backupStorageName"] = input.BackupStorageName
	}
	metadataRaw, err := json.Marshal(metadata)
	if err != nil {
		return RestorePoint{}, err
	}
	sizeMetricsRaw, err := json.Marshal(input.SizeMetricsV2)
	if err != nil {
		return RestorePoint{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return RestorePoint{}, err
	}
	defer tx.Rollback()
	var tenantID string
	if err := tx.QueryRow(`select tenant_id from clusters where id=$1 for share`, input.SourceClusterID).Scan(&tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RestorePoint{}, ErrTenantResourceMismatch
		}
		return RestorePoint{}, err
	}
	if err := validateRestorePointReferences(tx, input, tenantID); err != nil {
		return RestorePoint{}, err
	}
	point := RestorePoint{
		ID:                newID(),
		TenantID:          tenantID,
		ProtectionPlanID:  input.ProtectionPlanID,
		SourceClusterID:   input.SourceClusterID,
		AppID:             input.AppID,
		StorageRepoID:     input.StorageRepoID,
		DisplayName:       "",
		VeleroBackupName:  input.VeleroBackupName,
		PointType:         input.PointType,
		Status:            input.Status,
		SizeBytes:         input.SizeBytes,
		StartedAt:         input.StartedAt,
		CompletedAt:       input.CompletedAt,
		ExpiresAt:         input.ExpiresAt,
		SourceNamespace:   input.SourceNamespace,
		LabelSelector:     input.LabelSelector,
		BackupTaskID:      input.BackupTaskID,
		BackupStorageName: input.BackupStorageName,
		TaskCreatedAt:     input.TaskCreatedAt,
		Metadata:          metadata,
		SizeMetricsV2:     input.SizeMetricsV2,
		CreatedAt:         now,
	}
	if point.TaskCreatedAt.IsZero() {
		point.TaskCreatedAt = now
	}

	_, err = tx.Exec(`
		insert into restore_points (
			id, tenant_id, protection_plan_id, source_cluster_id, app_id, storage_repo_id,
			display_name, velero_backup_name, point_type, status, size_bytes, started_at, completed_at,
			expires_at, metadata, created_at, task_created_at, size_metrics_v2, backup_task_id
		)
		values ($1, $2, nullif($3, '')::uuid, $4, nullif($5, '')::uuid, nullif($6, '')::uuid,
			$7, $8, $9, $10, nullif($11, 0), nullif($12, '0001-01-01'::timestamptz),
			nullif($13, '0001-01-01'::timestamptz), nullif($14, '0001-01-01'::timestamptz), $15, $16, $17, $18,
			nullif($19, '')::uuid)
		on conflict (source_cluster_id, velero_backup_name) do update
		   set display_name = '',
		       size_bytes = coalesce(excluded.size_bytes, restore_points.size_bytes),
		       completed_at = coalesce(excluded.completed_at, restore_points.completed_at),
		       app_id = coalesce(restore_points.app_id, excluded.app_id),
		       storage_repo_id = coalesce(restore_points.storage_repo_id, excluded.storage_repo_id),
		       task_created_at = coalesce(restore_points.task_created_at, excluded.task_created_at),
		       backup_task_id = coalesce(restore_points.backup_task_id, excluded.backup_task_id),
		       metadata = coalesce(restore_points.metadata, '{}'::jsonb)
		           || (coalesce(excluded.metadata, '{}'::jsonb)
		               - array['velero', 'size', 'restorePointSize', 'planStorageSize', 'sizeStatus', 'sizeWarnings']),
		       size_metrics_v2 = case when excluded.size_metrics_v2 = '{}'::jsonb then restore_points.size_metrics_v2 else excluded.size_metrics_v2 end
	`, point.ID, point.TenantID, point.ProtectionPlanID, point.SourceClusterID, point.AppID,
		point.StorageRepoID, point.DisplayName, point.VeleroBackupName, point.PointType, point.Status, point.SizeBytes,
		point.StartedAt, point.CompletedAt, point.ExpiresAt, metadataRaw, now, point.TaskCreatedAt, sizeMetricsRaw, point.BackupTaskID)
	if err != nil {
		return RestorePoint{}, err
	}
	if err := tx.Commit(); err != nil {
		return RestorePoint{}, err
	}
	points, err := s.ListRestorePoints(RestorePointFilter{ClusterID: input.SourceClusterID})
	if err != nil {
		return RestorePoint{}, err
	}
	for _, existing := range points {
		if existing.VeleroBackupName == input.VeleroBackupName {
			return existing, nil
		}
	}
	return point, nil
}

func (s *PostgresStore) ListRestorePoints(filter RestorePointFilter) ([]RestorePoint, error) {
	metadataExpr := "metadata"
	if filter.Summary {
		metadataExpr = `jsonb_strip_nulls(jsonb_build_object(
			'sourceNamespace',metadata->'sourceNamespace','labelSelector',metadata->'labelSelector',
			'backupTaskId',metadata->'backupTaskId','backupStorageName',metadata->'backupStorageName',
			'includedNamespaces',metadata->'includedNamespaces','scheduled',metadata->'scheduled',
			'retentionState',metadata->'retentionState','retentionWarning',metadata->'retentionWarning',
			'protectionCleanupState',metadata->'protectionCleanupState'))`
	}
	query := `
		select id, tenant_id, coalesce(protection_plan_id::text, ''), source_cluster_id,
		       coalesce(app_id::text, ''), coalesce(storage_repo_id::text, ''),
		       display_name, velero_backup_name, point_type, status, coalesce(size_bytes, 0),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz),
		       coalesce(expires_at, '0001-01-01'::timestamptz),
		       coalesce(task_created_at, created_at), coalesce(backup_task_id::text, ''), ` + metadataExpr + `, created_at, size_metrics_v2
		from restore_points
	`
	args := []any{}
	conditions := []string{}
	if filter.TenantID != "" {
		args = append(args, filter.TenantID)
		conditions = append(conditions, "tenant_id = $"+strconv.Itoa(len(args)))
	}
	if filter.ClusterID != "" {
		args = append(args, filter.ClusterID)
		conditions = append(conditions, "source_cluster_id = $"+strconv.Itoa(len(args)))
	}
	if filter.AppID != "" {
		args = append(args, filter.AppID)
		conditions = append(conditions, "app_id = $"+strconv.Itoa(len(args))+"::uuid")
	}
	if filter.ProtectionPlanID != "" {
		args = append(args, filter.ProtectionPlanID)
		conditions = append(conditions, "protection_plan_id = $"+strconv.Itoa(len(args))+"::uuid")
	}
	if !filter.IncludeDeleted {
		conditions = append(conditions, "status <> 'deleted'")
		conditions = append(conditions, `exists (
			select 1 from protection_plans pp
			where pp.id = restore_points.protection_plan_id and pp.source_cluster_id = restore_points.source_cluster_id
		)`)
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += ` order by created_at desc`
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" limit $%d", len(args))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query += fmt.Sprintf(" offset $%d", len(args))
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []RestorePoint
	for rows.Next() {
		var item RestorePoint
		var metadataRaw, sizeMetricsRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
			&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
			&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.TaskCreatedAt, &item.BackupTaskID, &metadataRaw, &item.CreatedAt, &sizeMetricsRaw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadataRaw, &item.Metadata)
		_ = json.Unmarshal(sizeMetricsRaw, &item.SizeMetricsV2)
		hydrateRestorePointMetadata(&item)
		items = append(items, item)
	}
	return items, rows.Err()
}

func hydrateRestorePointMetadata(item *RestorePoint) {
	if item.Metadata == nil {
		return
	}
	item.SourceNamespace, _ = item.Metadata["sourceNamespace"].(string)
	item.LabelSelector, _ = item.Metadata["labelSelector"].(string)
	item.BackupStorageName, _ = item.Metadata["backupStorageName"].(string)
}

func (s *PostgresStore) GetRestorePoint(id string) (RestorePoint, bool, error) {
	var item RestorePoint
	var metadataRaw, sizeMetricsRaw []byte
	err := s.db.QueryRow(`
		select id, tenant_id, coalesce(protection_plan_id::text, ''), source_cluster_id,
		       coalesce(app_id::text, ''), coalesce(storage_repo_id::text, ''),
		       display_name, velero_backup_name, point_type, status, coalesce(size_bytes, 0),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz),
		       coalesce(expires_at, '0001-01-01'::timestamptz),
		       coalesce(task_created_at, created_at), coalesce(backup_task_id::text, ''), metadata, created_at, size_metrics_v2
		from restore_points
		where id = $1
	`, id).Scan(&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
		&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
		&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.TaskCreatedAt, &item.BackupTaskID, &metadataRaw, &item.CreatedAt, &sizeMetricsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return RestorePoint{}, false, nil
	}
	if err != nil {
		return RestorePoint{}, false, err
	}
	_ = json.Unmarshal(metadataRaw, &item.Metadata)
	_ = json.Unmarshal(sizeMetricsRaw, &item.SizeMetricsV2)
	hydrateRestorePointMetadata(&item)
	return item, true, nil
}

func (s *PostgresStore) UpdateRestorePointState(input RestorePointStateInput) (RestorePoint, bool, error) {
	if input.ID == "" {
		return RestorePoint{}, false, nil
	}
	status := input.Status
	if status == "" {
		status = "available"
	}
	metadataRaw, err := json.Marshal(input.Metadata)
	if err != nil {
		return RestorePoint{}, false, err
	}
	var item RestorePoint
	var metadataRawOut []byte
	err = s.db.QueryRow(`
		update restore_points
		   set status = $2,
		       metadata = coalesce(metadata, '{}'::jsonb) || $3::jsonb
		 where id = $1
		returning id, tenant_id, coalesce(protection_plan_id::text, ''), source_cluster_id,
		       coalesce(app_id::text, ''), coalesce(storage_repo_id::text, ''),
		       display_name, velero_backup_name, point_type, status, coalesce(size_bytes, 0),
		       coalesce(started_at, '0001-01-01'::timestamptz),
		       coalesce(completed_at, '0001-01-01'::timestamptz),
		       coalesce(expires_at, '0001-01-01'::timestamptz),
		       coalesce(task_created_at, created_at), metadata, created_at
	`, input.ID, status, metadataRaw).Scan(
		&item.ID, &item.TenantID, &item.ProtectionPlanID, &item.SourceClusterID,
		&item.AppID, &item.StorageRepoID, &item.DisplayName, &item.VeleroBackupName, &item.PointType, &item.Status,
		&item.SizeBytes, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt, &item.TaskCreatedAt, &metadataRawOut, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RestorePoint{}, false, nil
	}
	if err != nil {
		return RestorePoint{}, false, err
	}
	_ = json.Unmarshal(metadataRawOut, &item.Metadata)
	hydrateRestorePointMetadata(&item)
	return item, true, nil
}
