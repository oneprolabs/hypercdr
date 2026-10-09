package store

import "database/sql"

// Lock referenced rows until the write commits, so validation and persistence
// observe the same tenant ownership. Table names are supplied only by callers.
func validateTenantReference(tx *sql.Tx, table, id, tenantID string) error {
	if id == "" {
		return nil
	}
	var owner string
	if err := tx.QueryRow("select tenant_id from "+table+" where id=$1 for share", id).Scan(&owner); err != nil {
		if err == sql.ErrNoRows {
			return ErrTenantResourceMismatch
		}
		return err
	}
	if owner != tenantID {
		return ErrTenantResourceMismatch
	}
	return nil
}

func validateTaskReferences(tx *sql.Tx, input TaskInput, tenantID string) error {
	for _, ref := range []struct{ table, id string }{
		{"clusters", input.ClusterID}, {"applications", input.AppID},
		{"protection_plans", input.ProtectionPlanID}, {"restore_points", input.RestorePointID},
	} {
		if err := validateTenantReference(tx, ref.table, ref.id, tenantID); err != nil {
			return err
		}
	}
	return nil
}

func validateRestorePointReferences(tx *sql.Tx, input RestorePointInput, tenantID string) error {
	for _, ref := range []struct{ table, id string }{
		{"clusters", input.SourceClusterID}, {"applications", input.AppID},
		{"protection_plans", input.ProtectionPlanID}, {"storage_repositories", input.StorageRepoID},
		{"tasks", input.BackupTaskID},
	} {
		if err := validateTenantReference(tx, ref.table, ref.id, tenantID); err != nil {
			return err
		}
	}
	return nil
}
