package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrRestorePointQuota = errors.New("restore point quota exhausted: no unprotected point can be replaced")
var ErrRestorePointLocked = errors.New("restore point is retained, in use, or being replaced")
var ErrRestorePointReplacementPending = errors.New("restore point replacement pending")

type RestorePointLifecycleRepository interface {
	BeginProtectionPlanCleanup(string) error
	SetRestorePointRetention(string, string, bool) error
	ClaimRestorePointDeletion([]string) error
	PendingRestorePointReplacements() ([]RestorePointReplacement, error)
	BeginRestorePointReplacement(string, string) error
}

func (s *PostgresStore) BeginProtectionPlanCleanup(planID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := quotaPlanLock(tx, planID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRow(`select status from protection_plans where id=$1`, planID).Scan(&status); err != nil {
		return err
	}
	if status == "cleanup_running" || status == "cleaning" {
		return ErrProtectionPlanBusy
	}
	var busy bool
	if err := tx.QueryRow(`select exists(select 1 from tasks where (protection_plan_id=$1 or restore_point_id in (select id from restore_points where protection_plan_id=$1) or payload->>'restorePointId' in (select id::text from restore_points where protection_plan_id=$1)) and status not in ('succeeded','failed','canceled','cancelled')) or exists(select 1 from restore_point_reservations where protection_plan_id=$1 and state in ('reserved','data_ready','deleting'))`, planID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrProtectionPlanBusy
	}
	if _, err := tx.Exec(`update restore_points set metadata=metadata || '{"retentionState":"pending_delete"}'::jsonb where protection_plan_id=$1 and status!='deleted'`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(`update protection_plans set status='cleanup_running',updated_at=now() where id=$1`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(`update protection_plan_schedules set enabled=false,updated_at=now() where protection_plan_id=$1`, planID); err != nil {
		return err
	}
	return tx.Commit()
}

type RestorePointReplacement struct {
	Attempts      int
	TaskID        string
	PointID       string
	CleanupTaskID string
	State         string
	Input         RestorePointInput
}

func quotaPlanLock(tx *sql.Tx, planID string) error {
	if planID == "" {
		return nil
	}
	var found string
	return tx.QueryRow(`select id from protection_plans where id=$1 for update`, planID).Scan(&found)
}

func quotaPoints(tx *sql.Tx, planID string) ([]RestorePoint, error) {
	rows, err := tx.Query(`select id,status,coalesce(task_created_at,created_at),created_at,metadata from restore_points where protection_plan_id=$1 order by coalesce(task_created_at,created_at),id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []RestorePoint
	for rows.Next() {
		var point RestorePoint
		var raw []byte
		if err := rows.Scan(&point.ID, &point.Status, &point.TaskCreatedAt, &point.CreatedAt, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &point.Metadata); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

func quotaPolicy(tx *sql.Tx, planID string) (Policy, error) {
	var policy Policy
	var raw []byte
	err := tx.QueryRow(`select p.retention_count,p.retention_rules,p.retention_timezone from protection_plans pp join policies p on p.id=pp.policy_id where pp.id=$1 and p.composition in ('combined','retention')`, planID).Scan(&policy.RetentionCount, &raw, &policy.RetentionTimezone)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return policy, err
	}
	err = json.Unmarshal(raw, &policy.RetentionRules)
	return policy, err
}

func pointBusy(tx *sql.Tx, pointID string) (bool, error) {
	var busy bool
	err := tx.QueryRow(`select exists(select 1 from tasks where (restore_point_id=$1 or payload->>'restorePointId'=$1::text) and type in ('drill','restore','takeover') and completed_at is null and status not in ('failed','canceled','cancelled','succeeded')) or exists(select 1 from restore_point_reservations where replacement_point_id=$1 and state in ('reserved','data_ready','deleting'))`, pointID).Scan(&busy)
	return busy, err
}

func pointProtected(point RestorePoint) bool {
	return point.Metadata["manualRetained"] == true || point.Metadata["drillRetained"] == true
}

func prepareRestorePointDeletion(tx *sql.Tx, task Task) error {
	if task.Type != "retention-cleanup" && task.Type != "protection-cleanup" {
		return nil
	}
	if task.Payload["cleanupMode"] == "drill" || task.Payload["cleanupMode"] == "target" {
		return nil
	}
	raw, err := json.Marshal(task.Payload["restorePoints"])
	if err != nil {
		return err
	}
	var targets []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &targets); err != nil {
		return err
	}
	for _, target := range targets {
		var planID, status string
		var metadataRaw []byte
		if err := tx.QueryRow(`select coalesce(protection_plan_id::text,''),status,metadata from restore_points where id=$1 and tenant_id=$2`, target.ID, task.TenantID).Scan(&planID, &status, &metadataRaw); err != nil {
			return err
		}
		if err := quotaPlanLock(tx, planID); err != nil {
			return err
		}
		var metadata map[string]any
		if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
			return err
		}
		if status == "deleted" {
			continue
		}
		var fullCleanup bool
		if task.Type == "protection-cleanup" && task.ProtectionPlanID == planID {
			if err := tx.QueryRow(`select status='cleanup_running' from protection_plans where id=$1`, planID).Scan(&fullCleanup); err != nil {
				return err
			}
		}
		if !fullCleanup && (metadata["manualRetained"] == true || metadata["drillRetained"] == true) {
			return ErrRestorePointLocked
		}
		if owner, ok := metadata["replacementTaskId"].(string); ok && owner != "" && !fullCleanup {
			var state string
			if err := tx.QueryRow(`select state from restore_point_reservations where task_id=$1 and replacement_point_id=$2 for update`, owner, target.ID).Scan(&state); err != nil {
				return err
			}
			if task.Type != "retention-cleanup" || (state != "data_ready" && state != "deleting") {
				return ErrRestorePointLocked
			}
			if _, err := tx.Exec(`update restore_point_reservations set state='deleting',cleanup_task_id=$2,cleanup_attempts=cleanup_attempts+1,updated_at=now() where task_id=$1`, owner, task.ID); err != nil {
				return err
			}
		} else {
			busy, err := pointBusy(tx, target.ID)
			if err != nil {
				return err
			}
			if busy {
				return ErrRestorePointLocked
			}
		}
		if !fullCleanup && (task.Payload["manualDelete"] != true || metadata["replacementTaskId"] != nil) {
			policy, err := quotaPolicy(tx, planID)
			if err != nil {
				return err
			}
			if policy.RetentionTimezone != "" {
				points, err := quotaPoints(tx, planID)
				if err != nil {
					return err
				}
				selections, err := RetentionSelections(points, policy.RetentionRules, policy.RetentionTimezone, time.Now().UTC())
				if err != nil {
					return err
				}
				if len(selections[target.ID]) > 0 {
					return ErrRestorePointLocked
				}
			}
		}
		if _, err := tx.Exec(`update restore_points set metadata=metadata || jsonb_build_object('retentionState','deleting','retentionCleanupTask',$2::text) where id=$1`, target.ID, task.ID); err != nil {
			return err
		}
	}
	return nil
}

func reserveBackupQuota(tx *sql.Tx, task Task) error {
	if task.Type != "backup" || task.ProtectionPlanID == "" {
		return nil
	}
	if err := releaseFailedBackupReservations(tx, task.ProtectionPlanID); err != nil {
		return err
	}
	policy, err := quotaPolicy(tx, task.ProtectionPlanID)
	if err != nil || policy.RetentionCount <= 0 {
		return err
	}
	points, err := quotaPoints(tx, task.ProtectionPlanID)
	if err != nil {
		return err
	}
	selections, err := RetentionSelections(points, policy.RetentionRules, policy.RetentionTimezone, time.Now().UTC())
	if err != nil {
		return err
	}
	occupied := 0
	for _, point := range points {
		if point.Status != "deleted" {
			occupied++
		}
	}
	var reserved int
	if err := tx.QueryRow(`select count(*) from restore_point_reservations rr where rr.protection_plan_id=$1 and (rr.replacement_point_id is null or exists(select 1 from restore_points rp where rp.id=rr.replacement_point_id and rp.status='deleted')) and rr.state in ('reserved','data_ready','deleting')`, task.ProtectionPlanID).Scan(&reserved); err != nil {
		return err
	}
	replacement := ""
	if occupied+reserved > policy.RetentionCount {
		return fmt.Errorf("%w (%d/%d slots occupied)", ErrRestorePointQuota, occupied+reserved, policy.RetentionCount)
	}
	if occupied+reserved >= policy.RetentionCount {
		for _, point := range points {
			if point.Status != "available" || pointProtected(point) || len(selections[point.ID]) > 0 {
				continue
			}
			state, _ := point.Metadata["retentionState"].(string)
			if state == "pending_delete" || state == "deleting" {
				continue
			}
			busy, err := pointBusy(tx, point.ID)
			if err != nil {
				return err
			}
			if !busy {
				replacement = point.ID
				break
			}
		}
		if replacement == "" {
			return fmt.Errorf("%w (%d/%d slots occupied)", ErrRestorePointQuota, occupied+reserved, policy.RetentionCount)
		}
	}
	if _, err := tx.Exec(`insert into restore_point_reservations(task_id,protection_plan_id,replacement_point_id) values($1,$2,nullif($3,'')::uuid)`, task.ID, task.ProtectionPlanID, replacement); err != nil {
		return err
	}
	if replacement != "" {
		_, err = tx.Exec(`update restore_points set metadata=metadata || jsonb_build_object('replacementTaskId',$2::text) where id=$1`, replacement, task.ID)
	}
	return err
}

func releaseFailedBackupReservations(tx *sql.Tx, planID string) error {
	_, err := tx.Exec(`with released as (
		update restore_point_reservations rr set state='released',updated_at=now()
		from tasks t where rr.task_id=t.id and rr.protection_plan_id=$1
		and rr.state='reserved' and t.status in ('failed','canceled','cancelled')
		returning rr.task_id,rr.replacement_point_id
	)
	update restore_points rp set metadata=rp.metadata-'replacementTaskId'
	from released where rp.id=released.replacement_point_id
	and rp.metadata->>'replacementTaskId'=released.task_id::text`, planID)
	return err
}

func (s *PostgresStore) releaseFailedBackupReservations() error {
	rows, err := s.db.Query(`select distinct rr.protection_plan_id from restore_point_reservations rr join tasks t on t.id=rr.task_id where rr.state='reserved' and t.status in ('failed','canceled','cancelled') order by rr.protection_plan_id`)
	if err != nil {
		return err
	}
	var plans []string
	for rows.Next() {
		var planID string
		if err := rows.Scan(&planID); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, planID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, planID := range plans {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if err = quotaPlanLock(tx, planID); err == nil {
			err = releaseFailedBackupReservations(tx, planID)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) SetRestorePointRetention(id, tenantID string, retain bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var planID string
	if err := tx.QueryRow(`select coalesce(protection_plan_id::text,'') from restore_points where id=$1 and tenant_id=$2 and status='available'`, id, tenantID).Scan(&planID); err != nil {
		return err
	}
	if err := quotaPlanLock(tx, planID); err != nil {
		return err
	}
	busy, err := pointBusy(tx, id)
	if err != nil {
		return err
	}
	if busy {
		return ErrRestorePointLocked
	}
	result, err := tx.Exec(`update restore_points set metadata=metadata || jsonb_build_object('manualRetained',$3::boolean) where id=$1 and tenant_id=$2 and status='available' and coalesce(metadata->>'retentionState','') not in ('deleting','pending_delete')`, id, tenantID, retain)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrRestorePointLocked
	}
	return tx.Commit()
}

func (s *PostgresStore) ClaimRestorePointDeletion(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var planID string
		if err := tx.QueryRow(`select coalesce(protection_plan_id::text,'') from restore_points where id=$1`, id).Scan(&planID); err != nil {
			return err
		}
		if err := quotaPlanLock(tx, planID); err != nil {
			return err
		}
		busy, err := pointBusy(tx, id)
		if err != nil {
			return err
		}
		if busy {
			return ErrRestorePointLocked
		}
		result, err := tx.Exec(`update restore_points set metadata=metadata || '{"retentionState":"pending_delete"}'::jsonb where id=$1 and status='available' and coalesce(metadata->>'manualRetained','false')!='true' and coalesce(metadata->>'drillRetained','false')!='true' and coalesce(metadata->>'retentionState','') not in ('pending_delete','deleting')`, id)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrRestorePointLocked
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) PendingRestorePointReplacements() ([]RestorePointReplacement, error) {
	if err := s.releaseFailedBackupReservations(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`select rr.task_id,coalesce(rr.replacement_point_id::text,''),coalesce(rr.cleanup_task_id::text,''),rr.state,rr.pending_point,rr.cleanup_attempts from restore_point_reservations rr join tasks t on t.id=rr.task_id where rr.pending_point is not null and (rr.state in ('data_ready','deleting') or (rr.state='published' and t.completed_at is null)) order by rr.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []RestorePointReplacement
	for rows.Next() {
		var item RestorePointReplacement
		var raw []byte
		if err := rows.Scan(&item.TaskID, &item.PointID, &item.CleanupTaskID, &item.State, &raw, &item.Attempts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Input); err != nil {
			return nil, fmt.Errorf("pending restore point: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) BeginRestorePointReplacement(taskID, cleanupID string) error {
	_, err := s.db.Exec(`update restore_point_reservations set state='deleting',cleanup_task_id=$2,updated_at=now() where task_id=$1 and state in ('data_ready','deleting')`, taskID, cleanupID)
	return err
}
