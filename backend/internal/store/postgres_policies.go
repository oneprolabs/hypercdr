package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *PostgresStore) CreatePolicy(input PolicyInput) (Policy, error) {
	if input.RetentionTimezone == "" {
		input.RetentionTimezone = "UTC"
	}
	if err := ValidateRetentionRules(input.RetentionRules, input.RetentionTimezone); err != nil {
		return Policy{}, err
	}
	if input.RetentionRules == nil {
		input.RetentionRules = []RetentionRule{}
	}
	rulesRaw, _ := json.Marshal(input.RetentionRules)
	if input.TenantID == "" {
		input.TenantID = DefaultTenantID
	}
	now := time.Now().UTC()
	if input.Composition == "" {
		input.Composition = "manual"
	}
	if input.ScheduleType == "" {
		input.ScheduleType = "manual"
	}
	if input.Status == "" {
		input.Status = "pending_activation"
	}
	policy := Policy{
		RetentionRules:    input.RetentionRules,
		RetentionTimezone: input.RetentionTimezone,
		ID:                newID(),
		TenantID:          input.TenantID,
		Name:              input.Name,
		Composition:       input.Composition,
		ScheduleType:      input.ScheduleType,
		IntervalValue:     input.IntervalValue,
		IntervalUnit:      input.IntervalUnit,
		Hour:              input.Hour,
		Minute:            input.Minute,
		WeekDay:           input.WeekDay,
		MonthDay:          input.MonthDay,
		RetentionCount:    input.RetentionCount,
		RetentionDays:     input.RetentionDays,
		Status:            input.Status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	_, err := s.db.Exec(`
		insert into policies (
			id, tenant_id, name, composition, schedule_type, interval_value, interval_unit,
			hour, minute, week_day, month_day, retention_count, retention_days, status,
			created_at, updated_at, retention_rules, retention_timezone
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15, $16, $17)
	`, policy.ID, policy.TenantID, policy.Name, policy.Composition, policy.ScheduleType,
		policy.IntervalValue, policy.IntervalUnit, policy.Hour, policy.Minute, policy.WeekDay,
		policy.MonthDay, policy.RetentionCount, policy.RetentionDays, policy.Status, now, rulesRaw, input.RetentionTimezone)
	return policy, err
}

func (s *PostgresStore) ListPolicies() ([]Policy, error) {
	rows, err := s.db.Query(`
		select id, tenant_id, name, composition, schedule_type,
		       coalesce(interval_value, 0), coalesce(interval_unit, ''),
		       coalesce(hour, 0), coalesce(minute, 0), coalesce(week_day, 0), coalesce(month_day, 0),
		       coalesce(retention_count, 0), coalesce(retention_days, 0),
		       status, coalesce(policy_bindings.bound_count, 0), created_at, updated_at, retention_rules, retention_timezone
		from policies
		left join (
			select policy_id, count(distinct app_id) as bound_count
			from (
				select policy_id, app_id
				from protection_plans
				where policy_id is not null
				union all
				select pp.policy_id, ppa.app_id
				from protection_plans pp
				join protection_plan_apps ppa on ppa.plan_id = pp.id
				where pp.policy_id is not null
			) bound_apps
			group by policy_id
		) policy_bindings on policy_bindings.policy_id = policies.id
		order by policies.created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Policy
	for rows.Next() {
		var item Policy
		var rulesRaw []byte
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Composition, &item.ScheduleType,
			&item.IntervalValue, &item.IntervalUnit, &item.Hour, &item.Minute, &item.WeekDay,
			&item.MonthDay, &item.RetentionCount, &item.RetentionDays, &item.Status, &item.BoundCount,
			&item.CreatedAt, &item.UpdatedAt, &rulesRaw, &item.RetentionTimezone); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rulesRaw, &item.RetentionRules); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdatePolicy(id string, input PolicyInput) (Policy, bool, error) {
	if input.RetentionTimezone == "" {
		input.RetentionTimezone = "UTC"
	}
	if err := ValidateRetentionRules(input.RetentionRules, input.RetentionTimezone); err != nil {
		return Policy{}, false, err
	}
	if input.RetentionRules == nil {
		input.RetentionRules = []RetentionRule{}
	}
	rulesRaw, _ := json.Marshal(input.RetentionRules)
	tx, err := s.db.Begin()
	if err != nil {
		return Policy{}, false, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`select id from protection_plans where policy_id=$1 order by id for update`, id)
	if err != nil {
		return Policy{}, false, err
	}
	var plans []string
	for rows.Next() {
		var planID string
		if err := rows.Scan(&planID); err != nil {
			rows.Close()
			return Policy{}, false, err
		}
		plans = append(plans, planID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Policy{}, false, err
	}
	rows.Close()
	for _, planID := range plans {
		var occupied int
		if err := tx.QueryRow(`select (select count(*) from restore_points where protection_plan_id=$1 and status!='deleted')+(select count(*) from restore_point_reservations rr where rr.protection_plan_id=$1 and (rr.replacement_point_id is null or exists(select 1 from restore_points rp where rp.id=rr.replacement_point_id and rp.status='deleted')) and rr.state in ('reserved','data_ready','deleting'))`, planID).Scan(&occupied); err != nil {
			return Policy{}, false, err
		}
		if input.RetentionCount > 0 && (input.Composition == "combined" || input.Composition == "retention") && occupied > input.RetentionCount {
			return Policy{}, false, ErrRestorePointQuota
		}
	}
	result, err := tx.Exec(`update policies set name=$2,composition=$3,schedule_type=$4,interval_value=$5,interval_unit=nullif($6,''),hour=$7,minute=$8,week_day=$9,month_day=$10,retention_count=$11,retention_days=$12,status=$13,retention_rules=$14,retention_timezone=$15,updated_at=now() where id=$1`, id, input.Name, input.Composition, input.ScheduleType, input.IntervalValue, input.IntervalUnit, input.Hour, input.Minute, input.WeekDay, input.MonthDay, input.RetentionCount, input.RetentionDays, input.Status, rulesRaw, input.RetentionTimezone)
	if err != nil {
		return Policy{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return Policy{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Policy{}, false, err
	}
	items, err := s.ListPolicies()
	if err != nil {
		return Policy{}, false, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, true, nil
		}
	}
	return Policy{}, false, nil
}

func (s *PostgresStore) DeletePolicy(id string) (bool, bool, error) {
	var tenantID string
	if err := s.db.QueryRow(`select tenant_id from policies where id=$1`, id).Scan(&tenantID); errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	} else if err != nil {
		return false, false, err
	}
	var inUse bool
	if err := s.db.QueryRow(`select exists(select 1 from protection_plans where tenant_id=$1 and policy_id=$2)`, tenantID, id).Scan(&inUse); err != nil {
		return false, false, err
	}
	if inUse {
		return false, true, nil
	}
	result, err := s.db.Exec(`delete from policies where id=$1 and tenant_id=$2`, id, tenantID)
	if err != nil {
		return false, false, err
	}
	count, err := result.RowsAffected()
	return count > 0, false, err
}
