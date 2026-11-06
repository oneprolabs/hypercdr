package store

import (
	"encoding/json"
	"time"
)

func (s *PostgresStore) SavePlanCapacityResult(planID, clusterID, tenantID string, result map[string]any, requestedAt time.Time) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`update protection_plans set
plan_storage_size=coalesce(plan_storage_size,'{}'::jsonb) || $4::jsonb,
updated_at=now()
where id=$1 and source_cluster_id=$2 and tenant_id=$3
and coalesce(plan_storage_size->>'requestedAt','0001-01-01T00:00:00Z')::timestamptz < $5`, planID, clusterID, tenantID, raw, requestedAt.UTC())
	return err
}
