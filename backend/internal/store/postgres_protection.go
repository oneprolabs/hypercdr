package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *PostgresStore) CreateProtectionPlan(input ProtectionPlanInput) (ProtectionPlan, error) {
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	now := time.Now().UTC()
	if input.ScopeType == "" {
		input.ScopeType = "all"
	}
	if input.Status == "" {
		input.Status = "active"
	}
	includedResources, err := json.Marshal(input.IncludedResources)
	if err != nil {
		return ProtectionPlan{}, err
	}
	selection := input.ResourceSelection
	if selection.Mode == "" {
		selection.Mode = "all"
	}
	resourceSelection, err := json.Marshal(selection)
	if err != nil {
		return ProtectionPlan{}, err
	}
	labelSelector, err := json.Marshal(input.LabelSelector)
	if err != nil {
		return ProtectionPlan{}, err
	}
	excludedResources, err := json.Marshal(input.ExcludedResources)
	if err != nil {
		return ProtectionPlan{}, err
	}
	preHooks, err := json.Marshal(input.PreHooks)
	if err != nil {
		return ProtectionPlan{}, err
	}
	postHooks, err := json.Marshal(input.PostHooks)
	if err != nil {
		return ProtectionPlan{}, err
	}
	// Merge AppIDs (new) with the legacy AppID for backward compatibility.
	appIDs := dedupNonEmpty(append([]string{}, input.AppIDs...))
	if input.AppID != "" {
		appIDs = dedupNonEmpty(append(appIDs, input.AppID))
	}
	primary := ""
	if len(appIDs) > 0 {
		primary = appIDs[0]
	}
	plan := ProtectionPlan{
		ID:                   newID(),
		TenantID:             input.TenantID,
		SourceClusterID:      input.SourceClusterID,
		AppID:                primary,
		AppIDs:               appIDs,
		ScopeType:            input.ScopeType,
		IncludedResources:    input.IncludedResources,
		ResourceSelection:    selection,
		LabelSelector:        input.LabelSelector,
		IncludeClusterScoped: input.IncludeClusterScoped,
		StorageRepoID:        input.StorageRepoID,
		PolicyID:             input.PolicyID,
		TargetClusterID:      input.TargetClusterID,
		ExcludedResources:    input.ExcludedResources,
		PreHooks:             input.PreHooks,
		PostHooks:            input.PostHooks,
		Status:               input.Status,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ProtectionPlan{}, err
	}
	defer tx.Rollback()

	// Validate relationships in the same transaction as insertion. Handlers are
	// not the only caller: schedules and edition integrations also create plans.
	var referencesValid bool
	err = tx.QueryRow(`select
  ($2::uuid is null or exists(select 1 from clusters where id=$2 and tenant_id=$1)) and
  ($3::uuid is null or exists(select 1 from clusters where id=$3 and tenant_id=$1)) and
  ($4::uuid is null or exists(select 1 from storage_repositories where id=$4 and tenant_id=$1)) and
  ($5::uuid is null or exists(select 1 from policies where id=$5 and tenant_id=$1))`,
		input.TenantID, nullableResourceID(input.SourceClusterID), nullableResourceID(input.TargetClusterID), nullableResourceID(input.StorageRepoID), nullableResourceID(input.PolicyID)).Scan(&referencesValid)
	if err != nil {
		return ProtectionPlan{}, err
	}
	if !referencesValid {
		return ProtectionPlan{}, ErrTenantResourceMismatch
	}
	for _, appID := range appIDs {
		var valid bool
		err := tx.QueryRow(`select exists(select 1 from applications a join clusters c on c.id=a.cluster_id where a.id=$1 and a.cluster_id=$2 and c.tenant_id=$3)`, appID, input.SourceClusterID, input.TenantID).Scan(&valid)
		if err != nil {
			return ProtectionPlan{}, err
		}
		if !valid {
			return ProtectionPlan{}, ErrTenantResourceMismatch
		}
	}
	// Lock the application rows so concurrent requests for the same app cannot
	// both pass the ownership check and create duplicate plans.
	if len(appIDs) > 0 {
		rows, err := tx.Query(`select id from applications where id = any($1::uuid[]) order by id for update`, planIDsSlice(appIDs))
		if err != nil {
			return ProtectionPlan{}, err
		}
		for rows.Next() {
			var lockedID string
			if err := rows.Scan(&lockedID); err != nil {
				rows.Close()
				return ProtectionPlan{}, err
			}
		}
		if err := rows.Close(); err != nil {
			return ProtectionPlan{}, err
		}
		var existingPlanID, existingAppID string
		err = tx.QueryRow(`
			select p.id, ppa.app_id
			from protection_plans p
			join protection_plan_apps ppa on ppa.plan_id = p.id
			where p.tenant_id = $1 and p.source_cluster_id = $2
			  and ppa.app_id = any($3::uuid[])
			limit 1
		`, plan.TenantID, plan.SourceClusterID, planIDsSlice(appIDs)).Scan(&existingPlanID, &existingAppID)
		if err == nil {
			return ProtectionPlan{}, &ApplicationAlreadyProtectedError{ProtectionPlanID: existingPlanID, ApplicationID: existingAppID}
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, err
		}
	}
	_, err = tx.Exec(`
		insert into protection_plans (
			id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, resource_selection, label_selector,
			include_cluster_scoped, storage_repo_id, policy_id, target_cluster_id,
			excluded_resources, pre_hooks, post_hooks, status, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, nullif($10, '')::uuid, nullif($11, '')::uuid,
			nullif($12, '')::uuid, $13, $14, $15, $16, $17, $17)
	`, plan.ID, plan.TenantID, plan.SourceClusterID, plan.AppID, plan.ScopeType, includedResources, resourceSelection, labelSelector,
		plan.IncludeClusterScoped, plan.StorageRepoID, plan.PolicyID, plan.TargetClusterID,
		excludedResources, preHooks, postHooks, plan.Status, now)
	if err != nil {
		return ProtectionPlan{}, err
	}
	for _, appID := range appIDs {
		if _, err := tx.Exec(`
			insert into protection_plan_apps (plan_id, app_id, created_at)
			values ($1, $2, $3)
			on conflict (plan_id, app_id) do nothing
		`, plan.ID, appID, now); err != nil {
			return ProtectionPlan{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProtectionPlan{}, err
	}
	return plan, nil
}

func (s *PostgresStore) UpdateProtectionPlanStatus(id string, status string) (ProtectionPlan, bool, error) {
	if id == "" {
		return ProtectionPlan{}, false, nil
	}
	if status == "" {
		status = "pending_activation"
	}
	_, err := s.db.Exec(`
		update protection_plans
		set status = $2,
		    updated_at = $3
		where id = $1
	`, id, status, time.Now().UTC())
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	return s.GetProtectionPlan(id)
}

func (s *PostgresStore) UpdateProtectionPlanStorageSize(id string, size map[string]any) (ProtectionPlan, bool, error) {
	if id == "" || len(size) == 0 {
		return ProtectionPlan{}, false, nil
	}
	raw, err := json.Marshal(size)
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := s.db.Exec(`
		update protection_plans
		set plan_storage_size = $2::jsonb,
		    updated_at = $3
		where id = $1
	`, id, raw, time.Now().UTC()); err != nil {
		return ProtectionPlan{}, false, err
	}
	return s.GetProtectionPlan(id)
}

func (s *PostgresStore) UpsertProtectionPlanSchedule(input ProtectionPlanScheduleInput) (ProtectionPlanSchedule, error) {
	now := time.Now().UTC()
	enabled := input.Enabled
	var item ProtectionPlanSchedule
	err := s.db.QueryRow(`
		insert into protection_plan_schedules (
			protection_plan_id, next_fire_at, enabled, created_at, updated_at
		)
		values ($1, nullif($2, '0001-01-01'::timestamptz), $3, $4, $4)
		on conflict (protection_plan_id) do update
		   set next_fire_at = excluded.next_fire_at,
		       enabled = excluded.enabled,
		       updated_at = excluded.updated_at
		returning protection_plan_id::text,
		          coalesce(last_fired_at, '0001-01-01'::timestamptz),
		          coalesce(next_fire_at, '0001-01-01'::timestamptz),
		          enabled, created_at, updated_at
	`, input.ProtectionPlanID, input.NextFireAt, enabled, now).Scan(
		&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *PostgresStore) GetProtectionPlanSchedule(planID string) (ProtectionPlanSchedule, bool, error) {
	var item ProtectionPlanSchedule
	err := s.db.QueryRow(`
		select pps.protection_plan_id::text,
		       coalesce(pps.last_fired_at, '0001-01-01'::timestamptz),
		       coalesce(pps.next_fire_at, '0001-01-01'::timestamptz),
		       pps.enabled, pps.created_at, pps.updated_at
		from protection_plan_schedules pps
		where pps.protection_plan_id = $1
	`, planID).Scan(&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ProtectionPlanSchedule{}, false, nil
	}
	if err != nil {
		return ProtectionPlanSchedule{}, false, err
	}
	return item, true, nil
}

func (s *PostgresStore) ListDueProtectionPlanSchedules(now time.Time) ([]ProtectionPlanSchedule, error) {
	rows, err := s.db.Query(`
		select pps.protection_plan_id::text,
		       coalesce(pps.last_fired_at, '0001-01-01'::timestamptz),
		       coalesce(pps.next_fire_at, '0001-01-01'::timestamptz),
		       pps.enabled, pps.created_at, pps.updated_at
		from protection_plan_schedules pps
		join protection_plans pp on pp.id=pps.protection_plan_id
		join resource_scopes t on t.id=pp.tenant_id and t.status='active'
		where pps.enabled = true
		  and pps.next_fire_at is not null
		  and pps.next_fire_at <= $1
		order by pps.next_fire_at asc
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProtectionPlanSchedule{}
	for rows.Next() {
		var item ProtectionPlanSchedule
		if err := rows.Scan(&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) MarkProtectionPlanScheduleFired(input ProtectionPlanScheduleFiredInput) (ProtectionPlanSchedule, bool, error) {
	var item ProtectionPlanSchedule
	err := s.db.QueryRow(`
		update protection_plan_schedules
		   set last_fired_at = nullif($2, '0001-01-01'::timestamptz),
		       next_fire_at = nullif($3, '0001-01-01'::timestamptz),
		       updated_at = $4
		 where protection_plan_id = $1
		returning protection_plan_id::text,
		          coalesce(last_fired_at, '0001-01-01'::timestamptz),
		          coalesce(next_fire_at, '0001-01-01'::timestamptz),
		          enabled, created_at, updated_at
	`, input.ProtectionPlanID, input.LastFiredAt, input.NextFireAt, time.Now().UTC()).Scan(
		&item.ProtectionPlanID, &item.LastFiredAt, &item.NextFireAt, &item.Enabled, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProtectionPlanSchedule{}, false, nil
	}
	if err != nil {
		return ProtectionPlanSchedule{}, false, err
	}
	return item, true, nil
}

func (s *PostgresStore) DisableProtectionPlanSchedule(planID string) error {
	_, err := s.db.Exec(`
		update protection_plan_schedules
		   set enabled = false,
		       updated_at = $2
		 where protection_plan_id = $1
	`, planID, time.Now().UTC())
	return err
}

func (s *PostgresStore) ListProtectionPlans(clusterID string) ([]ProtectionPlan, error) {
	query := `
		select pp.id, pp.tenant_id, pp.source_cluster_id, pp.app_id, pp.scope_type, pp.included_resources, pp.resource_selection, pp.label_selector,
		       pp.include_cluster_scoped, coalesce(pp.storage_repo_id::text, ''), coalesce(pp.policy_id::text, ''),
		       coalesce(pp.target_cluster_id::text, ''), pp.excluded_resources, pp.pre_hooks, pp.post_hooks,
		       pp.plan_storage_size, coalesce(pps.next_fire_at, '0001-01-01'::timestamptz), coalesce(pps.enabled, false),
		       coalesce(pp.latest_sync_task_id::text, ''), coalesce(pp.latest_recovery_task_id::text, ''),
		       pp.status, pp.created_at, pp.updated_at
		from protection_plans pp
		left join protection_plan_schedules pps on pps.protection_plan_id = pp.id
	`
	args := []any{}
	if clusterID != "" {
		query += ` where pp.source_cluster_id = $1`
		args = append(args, clusterID)
	}
	query += ` order by pp.created_at desc`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	planIDs := make([]string, 0, 32)
	planMap := map[string]*ProtectionPlan{}
	for rows.Next() {
		var item ProtectionPlan
		var includedResources, resourceSelection, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
			&includedResources, &resourceSelection, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
			&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize,
			&item.NextFireAt, &item.ScheduleEnabled, &item.LatestSyncTaskID, &item.LatestRecoveryTaskID, &item.Status,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(includedResources, &item.IncludedResources)
		_ = json.Unmarshal(resourceSelection, &item.ResourceSelection)
		if item.ResourceSelection.Mode == "" {
			item.ResourceSelection.Mode = "all"
		}
		_ = json.Unmarshal(labelSelector, &item.LabelSelector)
		_ = json.Unmarshal(excludedResources, &item.ExcludedResources)
		_ = json.Unmarshal(preHooks, &item.PreHooks)
		_ = json.Unmarshal(postHooks, &item.PostHooks)
		_ = json.Unmarshal(planStorageSize, &item.PlanStorageSize)
		item.AppIDs = []string{}
		planIDs = append(planIDs, item.ID)
		planCopy := item
		planMap[item.ID] = &planCopy
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(planIDs) > 0 {
		if err := s.loadProtectionPlanApps(planIDs, planMap); err != nil {
			return nil, err
		}
	}
	items := make([]ProtectionPlan, 0, len(planMap))
	for _, id := range planIDs {
		items = append(items, *planMap[id])
	}
	return items, nil
}

func (s *PostgresStore) loadProtectionPlanApps(planIDs []string, planMap map[string]*ProtectionPlan) error {
	rows, err := s.db.Query(`
		select plan_id, app_id
		from protection_plan_apps
		where plan_id = any($1::uuid[])
		order by plan_id, created_at
	`, planIDsSlice(planIDs))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var planID, appID string
		if err := rows.Scan(&planID, &appID); err != nil {
			return err
		}
		if plan, ok := planMap[planID]; ok {
			plan.AppIDs = append(plan.AppIDs, appID)
		}
	}
	return rows.Err()
}

func (s *PostgresStore) DeleteProtectionPlan(id string) (ProtectionPlan, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	defer tx.Rollback()

	var item ProtectionPlan
	var includedResources, resourceSelection, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	err = tx.QueryRow(`
		select id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, resource_selection, label_selector,
		       include_cluster_scoped, coalesce(storage_repo_id::text, ''), coalesce(policy_id::text, ''),
		       coalesce(target_cluster_id::text, ''), excluded_resources, pre_hooks, post_hooks,
		       plan_storage_size, status, created_at, updated_at
		from protection_plans
		where id = $1
		for update
	`, id).Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &resourceSelection, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize, &item.Status,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, false, nil
		}
		return ProtectionPlan{}, false, err
	}
	_ = json.Unmarshal(includedResources, &item.IncludedResources)
	_ = json.Unmarshal(resourceSelection, &item.ResourceSelection)
	if item.ResourceSelection.Mode == "" {
		item.ResourceSelection.Mode = "all"
	}
	_ = json.Unmarshal(labelSelector, &item.LabelSelector)
	_ = json.Unmarshal(excludedResources, &item.ExcludedResources)
	_ = json.Unmarshal(preHooks, &item.PreHooks)
	_ = json.Unmarshal(postHooks, &item.PostHooks)
	_ = json.Unmarshal(planStorageSize, &item.PlanStorageSize)

	rows, err := tx.Query(`select app_id from protection_plan_apps where plan_id = $1 order by created_at`, id)
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	for rows.Next() {
		var appID string
		if err := rows.Scan(&appID); err != nil {
			rows.Close()
			return ProtectionPlan{}, false, err
		}
		item.AppIDs = append(item.AppIDs, appID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ProtectionPlan{}, false, err
	}
	rows.Close()
	if len(item.AppIDs) == 0 && item.AppID != "" {
		item.AppIDs = []string{item.AppID}
	}

	if _, err := tx.Exec(`
		update tasks
		set payload = jsonb_set(coalesce(nullif(payload, 'null'::jsonb), '{}'::jsonb), '{archivedProtectionPlanId}', to_jsonb(protection_plan_id::text), true),
		    protection_plan_id = null
		where protection_plan_id = $1
	`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`
		update restore_points
		set metadata = jsonb_set(coalesce(metadata, '{}'::jsonb), '{archivedProtectionPlanId}', to_jsonb(protection_plan_id::text), true),
		    protection_plan_id = null
		where protection_plan_id = $1
	`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plan_apps where plan_id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plans where id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if len(item.AppIDs) > 0 {
		if _, err := tx.Exec(`
			update applications
			set protection_status = 'unprotected',
			    updated_at = $2
			where id = any($1::uuid[])
		`, planIDsSlice(item.AppIDs), time.Now().UTC()); err != nil {
			return ProtectionPlan{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProtectionPlan{}, false, err
	}
	return item, true, nil
}

func (s *PostgresStore) ClearProtectionPlanTargetCluster(id string, targetClusterID string) (ProtectionPlan, bool, error) {
	var item ProtectionPlan
	result := s.db.QueryRow(`update protection_plans set target_cluster_id = null, updated_at = now() where id = $1 and target_cluster_id = $2 returning id`, id, targetClusterID)
	if err := result.Scan(&item.ID); errors.Is(err, sql.ErrNoRows) {
		return ProtectionPlan{}, false, nil
	} else if err != nil {
		return ProtectionPlan{}, false, err
	}
	return s.GetProtectionPlan(id)
}

func (s *PostgresStore) CleanupProtectionPlanRecords(id string) (ProtectionPlan, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	defer tx.Rollback()

	var item ProtectionPlan
	var includedResources, resourceSelection, labelSelector, excludedResources, preHooks, postHooks, planStorageSize []byte
	err = tx.QueryRow(`
		select id, tenant_id, source_cluster_id, app_id, scope_type, included_resources, resource_selection, label_selector,
		       include_cluster_scoped, coalesce(storage_repo_id::text, ''), coalesce(policy_id::text, ''),
		       coalesce(target_cluster_id::text, ''), excluded_resources, pre_hooks, post_hooks,
		       plan_storage_size, status, created_at, updated_at
		from protection_plans
		where id = $1
		for update
	`, id).Scan(&item.ID, &item.TenantID, &item.SourceClusterID, &item.AppID, &item.ScopeType,
		&includedResources, &resourceSelection, &labelSelector, &item.IncludeClusterScoped, &item.StorageRepoID, &item.PolicyID,
		&item.TargetClusterID, &excludedResources, &preHooks, &postHooks, &planStorageSize, &item.Status,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProtectionPlan{}, false, nil
		}
		return ProtectionPlan{}, false, err
	}
	_ = json.Unmarshal(includedResources, &item.IncludedResources)
	_ = json.Unmarshal(resourceSelection, &item.ResourceSelection)
	if item.ResourceSelection.Mode == "" {
		item.ResourceSelection.Mode = "all"
	}
	_ = json.Unmarshal(labelSelector, &item.LabelSelector)
	_ = json.Unmarshal(excludedResources, &item.ExcludedResources)
	_ = json.Unmarshal(preHooks, &item.PreHooks)
	_ = json.Unmarshal(postHooks, &item.PostHooks)
	_ = json.Unmarshal(planStorageSize, &item.PlanStorageSize)

	rows, err := tx.Query(`select app_id from protection_plan_apps where plan_id = $1 order by created_at`, id)
	if err != nil {
		return ProtectionPlan{}, false, err
	}
	for rows.Next() {
		var appID string
		if err := rows.Scan(&appID); err != nil {
			rows.Close()
			return ProtectionPlan{}, false, err
		}
		item.AppIDs = append(item.AppIDs, appID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ProtectionPlan{}, false, err
	}
	rows.Close()
	if len(item.AppIDs) == 0 && item.AppID != "" {
		item.AppIDs = []string{item.AppID}
	}

	// restore_points.backup_task_id points back to the backup task with ON
	// DELETE SET NULL, while the restore-point integrity trigger rejects a null
	// backup task. Break the task -> restore point edge first, delete restore
	// points while their backup tasks still exist, and only then delete tasks.
	if _, err := tx.Exec(`
		update tasks
		set restore_point_id = null
		where restore_point_id in (
			select id from restore_points where protection_plan_id = $1
		)
	`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from restore_points where protection_plan_id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	// The plan's latest-task pointers also use ON DELETE SET NULL plus a guard
	// trigger. Clear them explicitly while the referenced tasks still exist;
	// otherwise deleting either task invokes the guard after the task has gone.
	if _, err := tx.Exec(`
		update protection_plans
		set latest_sync_task_id = null,
		    latest_recovery_task_id = null
		where id = $1
	`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from tasks where protection_plan_id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plan_apps where plan_id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if _, err := tx.Exec(`delete from protection_plans where id = $1`, id); err != nil {
		return ProtectionPlan{}, false, err
	}
	if len(item.AppIDs) > 0 {
		if _, err := tx.Exec(`
			update applications
			set protection_status = 'pending_protection',
			    updated_at = $2
			where id = any($1::uuid[])
			  and not exists (
			    select 1
			    from protection_plan_apps ppa
			    join protection_plans pp on pp.id = ppa.plan_id
			    where ppa.app_id = applications.id
			      and pp.tenant_id = $3
			      and pp.source_cluster_id = $4
			  )
		`, planIDsSlice(item.AppIDs), time.Now().UTC(), item.TenantID, item.SourceClusterID); err != nil {
			return ProtectionPlan{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ProtectionPlan{}, false, err
	}
	return item, true, nil
}

func planIDsSlice(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dedupNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func nullableResourceID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
