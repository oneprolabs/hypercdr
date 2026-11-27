package store

import (
	"database/sql"
	"errors"
	"fmt"
)

type ClusterSyncSettings struct {
	Configured                     bool   `json:"configured"`
	MaxSyncTasks                   int    `json:"maxSyncTasks"`
	NodeTransferConcurrency        int    `json:"nodeTransferConcurrency"`
	AppliedMaxSyncTasks            int    `json:"appliedMaxSyncTasks"`
	AppliedNodeTransferConcurrency int    `json:"appliedNodeTransferConcurrency"`
	ApplyTaskID                    string `json:"applyTaskId,omitempty"`
}

const DefaultSyncPrepareQueueLength = 8

func DefaultClusterSyncSettings() ClusterSyncSettings {
	return ClusterSyncSettings{MaxSyncTasks: 4, NodeTransferConcurrency: 8}
}

func (s *PostgresStore) EnsureClusterSyncSettings(clusterID string) error {
	_, err := s.db.Exec(`insert into cluster_sync_settings(cluster_id) values($1) on conflict do nothing`, clusterID)
	return err
}

func (settings ClusterSyncSettings) Validate() error {
	if settings.MaxSyncTasks < 1 || settings.MaxSyncTasks > 20 || settings.NodeTransferConcurrency < 1 || settings.NodeTransferConcurrency > 32 {
		return fmt.Errorf("simultaneous sync tasks must be 1–20; transfers per node must be 1–32")
	}
	return nil
}

func (s *PostgresStore) GetClusterSyncSettings(clusterID string) (ClusterSyncSettings, error) {
	if _, err := s.db.Exec(`update cluster_sync_settings setting set applied_max_sync_tasks=(task.payload->>'concurrentBackups')::integer,applied_node_transfer_concurrency=(task.payload->>'nodeAgentConcurrency')::integer from tasks task where setting.cluster_id=$1 and task.id=setting.apply_task_id and task.status='succeeded' and task.payload->>'syncSettingsApply'='true' and (setting.applied_max_sync_tasks is distinct from (task.payload->>'concurrentBackups')::integer or setting.applied_node_transfer_concurrency is distinct from (task.payload->>'nodeAgentConcurrency')::integer)`, clusterID); err != nil {
		return ClusterSyncSettings{}, err
	}
	settings := DefaultClusterSyncSettings()
	err := s.db.QueryRow(`select max_sync_tasks,node_transfer_concurrency,coalesce(applied_max_sync_tasks,0),coalesce(applied_node_transfer_concurrency,0),coalesce(apply_task_id::text,'') from cluster_sync_settings where cluster_id=$1`, clusterID).Scan(&settings.MaxSyncTasks, &settings.NodeTransferConcurrency, &settings.AppliedMaxSyncTasks, &settings.AppliedNodeTransferConcurrency, &settings.ApplyTaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	settings.Configured = err == nil
	return settings, err
}

func (s *PostgresStore) SaveClusterSyncSettings(clusterID string, settings ClusterSyncSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	_, err := s.db.Exec(`insert into cluster_sync_settings(cluster_id,max_sync_tasks,node_transfer_concurrency) values($1,$2,$3) on conflict(cluster_id) do update set max_sync_tasks=excluded.max_sync_tasks,node_transfer_concurrency=excluded.node_transfer_concurrency,apply_task_id=case when exists(select 1 from tasks where id=cluster_sync_settings.apply_task_id and status in ('succeeded','failed','cancelled','canceled')) then null else cluster_sync_settings.apply_task_id end,updated_at=now()`, clusterID, settings.MaxSyncTasks, settings.NodeTransferConcurrency)
	return err
}

func (s *PostgresStore) AdmitSyncTask(taskID, clusterID, tenantID string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var owner string
	if err = tx.QueryRow(`select tenant_id::text from clusters where id=$1 for update`, clusterID).Scan(&owner); err != nil {
		return false, err
	}
	if owner != tenantID {
		return false, ErrTenantResourceMismatch
	}
	var admitted sql.NullTime
	var status string
	var taskType string
	var done sql.NullTime
	if err = tx.QueryRow(`select sync_admitted_at,status,completed_at,type from tasks where id=$1 and cluster_id=$2 and tenant_id=$3 for update`, taskID, clusterID, tenantID).Scan(&admitted, &status, &done, &taskType); err != nil {
		return false, err
	}
	if done.Valid || status == "failed" || status == "succeeded" || status == "cancelled" || status == "canceled" {
		return false, nil
	}
	if admitted.Valid || status != "queued" {
		return true, tx.Commit()
	}
	var applying bool
	if err = tx.QueryRow(`select exists(select 1 from tasks where cluster_id=$1 and type='velero-upgrade' and completed_at is null and status not in ('succeeded','failed','cancelled','canceled'))`, clusterID).Scan(&applying); err != nil {
		return false, err
	}
	if applying {
		return false, nil
	}
	if taskType != "storage-sync" && taskType != "schedule-sync" {
		if err = tx.QueryRow(`select exists(select 1 from cluster_sync_settings where cluster_id=$1 and (max_sync_tasks is distinct from applied_max_sync_tasks or node_transfer_concurrency is distinct from applied_node_transfer_concurrency))`, clusterID).Scan(&applying); err != nil {
			return false, err
		}
		if applying {
			return false, nil
		}
	}
	var limit, active int
	if err = tx.QueryRow(`select coalesce((select max_sync_tasks from cluster_sync_settings where cluster_id=$1),4)`, clusterID).Scan(&limit); err != nil {
		return false, err
	}
	if err = tx.QueryRow(`select count(*) from tasks where cluster_id=$1 and type='backup' and completed_at is null and status not in ('succeeded','failed','cancelled','canceled') and (sync_admitted_at is not null or status <> 'queued')`, clusterID).Scan(&active); err != nil {
		return false, err
	}
	if taskType == "backup" && active >= limit {
		return false, nil
	}
	var first string
	if taskType == "backup" {
		if err = tx.QueryRow(`select id::text from tasks where cluster_id=$1 and type='backup' and status='queued' and completed_at is null and sync_admitted_at is null order by created_at,id limit 1`, clusterID).Scan(&first); err != nil {
			return false, err
		}
		if first != taskID {
			return false, nil
		}
	}
	if _, err = tx.Exec(`update tasks set sync_admitted_at=now() where id=$1`, taskID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
