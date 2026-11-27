package httpserver

import (
	"fmt"

	"hypercdr-platform/platform/backend/internal/store"
)

func validateTaskConsistencyCompletion(task store.Task, result map[string]any) error {
	return store.ValidateTaskConsistencyCompletion(task, result)
}
func (r *Router) validateBackupConsistencyCompletion(task store.Task, result map[string]any) error {
	if err := validateTaskConsistencyCompletion(task, result); err != nil {
		return err
	}
	if _, captured := task.Payload["dataProtectionSnapshotVersion"]; captured || task.ProtectionPlanID == "" {
		return nil
	}
	plan, found, err := r.store.GetProtectionPlan(task.ProtectionPlanID)
	if err != nil {
		return err
	}
	if !found || plan.SourceClusterID != task.ClusterID || plan.TenantID != task.TenantID {
		return fmt.Errorf("%w: legacy task protection plan cannot be verified", store.ErrBackupResultConflict)
	}
	if (plan.DataProtection.Mode != "" && plan.DataProtection.Mode != "live-filesystem") || plan.DataProtection.Group != nil || task.Payload["dataProtectionMode"] == "application-consistent" {
		return fmt.Errorf("%w: strict backup requires a creation-time consistency snapshot", store.ErrBackupResultConflict)
	}
	return nil
}
