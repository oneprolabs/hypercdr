package store

import (
	"encoding/json"

	"hypercdr-platform/platform/backend/pkg/accesscontrol"
)

func (s *PostgresStore) populateUserAccess(user *User) error {
	var customRoleID, name string
	var raw []byte
	err := s.db.QueryRow(`select u.role,coalesce(u.custom_role_id::text,''),coalesce(r.name,''),coalesce(r.permissions,'[]'::jsonb) from users u left join organization_roles r on r.id=u.custom_role_id and r.tenant_id=u.tenant_id where u.id=$1 and u.tenant_id=$2`, user.ID, user.TenantID).Scan(&user.Role, &customRoleID, &name, &raw)
	if err != nil {
		return err
	}
	user.RoleID = user.Role
	user.RoleName = "Operator"
	user.Permissions = accesscontrol.All()
	if user.Role == "admin" {
		user.RoleName = "Administrator"
	}
	if customRoleID != "" {
		user.RoleID, user.RoleName = customRoleID, name
		if err := json.Unmarshal(raw, &user.Permissions); err != nil {
			return err
		}
	}
	return nil
}
