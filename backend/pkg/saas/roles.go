package saas

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"hypercdr-platform/platform/backend/pkg/accesscontrol"
	"hypercdr-platform/platform/backend/pkg/platform"
)

type organizationRole struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	BuiltIn      bool     `json:"builtIn"`
	Permissions  []string `json:"permissions"`
	MemberCount  int      `json:"memberCount"`
	PendingCount int      `json:"pendingInvitationCount"`
}

func (s *Service) administrationRoles(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	scoped, ok := s.organizationContext(w, r, p)
	if ok {
		s.roles(w, r, scoped)
	}
}

func (s *Service) roles(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if r.Method != http.MethodGet {
		s.mutateRole(w, r, p)
		return
	}
	items := []organizationRole{
		{ID: "admin", Name: "Administrator", Description: "All organization business and administration permissions. Platform administration is separate.", BuiltIn: true, Permissions: accesscontrol.All()},
		{ID: "operator", Name: "Operator", Description: "All business permissions. No member, role, organization or license management.", BuiltIn: true, Permissions: accesscontrol.All()},
	}
	for index := range items {
		if err := s.db.QueryRowContext(r.Context(), `select (select count(*) from users where tenant_id=$1 and role=$2 and custom_role_id is null),(select count(*) from saas_onboarding_tokens where tenant_id=$1 and kind='invitation' and role_id=$2 and custom_role_id is null and consumed_at is null and expires_at>now())`, p.TenantID, items[index].ID).Scan(&items[index].MemberCount, &items[index].PendingCount); err != nil {
			internal(w)
			return
		}
	}
	rows, err := s.db.QueryContext(r.Context(), `select r.id,r.name,r.description,r.permissions,(select count(*) from users u where u.tenant_id=r.tenant_id and u.custom_role_id=r.id),(select count(*) from saas_onboarding_tokens t where t.tenant_id=r.tenant_id and t.custom_role_id=r.id and t.consumed_at is null and t.expires_at>now()) from organization_roles r where r.tenant_id=$1 order by lower(r.name),r.id`, p.TenantID)
	if err != nil {
		internal(w)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var role organizationRole
		var raw []byte
		if rows.Scan(&role.ID, &role.Name, &role.Description, &raw, &role.MemberCount, &role.PendingCount) != nil || json.Unmarshal(raw, &role.Permissions) != nil {
			internal(w)
			return
		}
		items = append(items, role)
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"items": items, "permissions": accesscontrol.Catalog()})
}

func (s *Service) mutateRole(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !admin(w, p) {
		return
	}
	id := r.PathValue("id")
	if id == "admin" || id == "operator" {
		problem(w, 409, "built_in_role_read_only", "Built-in roles cannot be changed or deleted.")
		return
	}
	creating, deleting := r.Method == http.MethodPost, r.Method == http.MethodDelete
	if creating {
		id = uuid.NewString()
	} else if _, err := uuid.Parse(id); err != nil {
		problem(w, 404, "role_not_found", "Role not found in this organization.")
		return
	}
	var in struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if !deleting {
		if !decode(r, &in) {
			problem(w, 400, "invalid_role", "Enter a role name and select valid business permissions.")
			return
		}
		in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
		if utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 64 || utf8.RuneCountInString(in.Description) > 500 || !accesscontrol.Valid(in.Permissions) {
			problem(w, 400, "invalid_role", "Use a name of 1–64 characters, a description of up to 500 characters and valid business permissions.")
			return
		}
		if strings.EqualFold(in.Name, "admin") || strings.EqualFold(in.Name, "Administrator") || strings.EqualFold(in.Name, "Operator") {
			problem(w, 409, "role_name_reserved", "This name is reserved for a built-in role.")
			return
		}
		if in.Permissions == nil {
			in.Permissions = []string{}
		}
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var tenantStatus string
	if err = tx.QueryRowContext(r.Context(), `select status from resource_scopes where id=$1 for update`, p.TenantID).Scan(&tenantStatus); err != nil {
		internal(w)
		return
	}
	if tenantStatus != "active" {
		problem(w, 403, "organization_disabled", "This organization is disabled.")
		return
	}
	if !creating {
		var existing string
		err = tx.QueryRowContext(r.Context(), `select id from organization_roles where tenant_id=$1 and id=$2 for update`, p.TenantID, id).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			problem(w, 404, "role_not_found", "Role not found in this organization.")
			return
		}
		if err != nil {
			internal(w)
			return
		}
	}
	if deleting {
		var used bool
		err = tx.QueryRowContext(r.Context(), `select exists(select 1 from users where tenant_id=$1 and custom_role_id=$2) or exists(select 1 from saas_onboarding_tokens where tenant_id=$1 and custom_role_id=$2 and consumed_at is null and expires_at>now())`, p.TenantID, id).Scan(&used)
		if err != nil {
			internal(w)
			return
		}
		if used {
			problem(w, 409, "role_in_use", "Reassign members and revoke pending invitations before deleting this role.")
			return
		}
		_, err = tx.ExecContext(r.Context(), `update saas_onboarding_tokens set custom_role_id=null where tenant_id=$1 and custom_role_id=$2`, p.TenantID, id)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `delete from organization_roles where tenant_id=$1 and id=$2`, p.TenantID, id)
		}
	} else {
		raw, _ := json.Marshal(in.Permissions)
		if creating {
			_, err = tx.ExecContext(r.Context(), `insert into organization_roles(id,tenant_id,name,description,permissions,created_by) values($1,$2,$3,$4,$5,nullif($6,'')::uuid)`, id, p.TenantID, in.Name, in.Description, raw, p.ID)
		} else {
			_, err = tx.ExecContext(r.Context(), `update organization_roles set name=$3,description=$4,permissions=$5,updated_at=now() where tenant_id=$1 and id=$2`, p.TenantID, id, in.Name, in.Description, raw)
		}
	}
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			problem(w, 409, "role_name_exists", "A role with this name already exists in your organization.")
		} else {
			internal(w)
		}
		return
	}
	if tx.Commit() != nil {
		internal(w)
		return
	}
	action, status := "role.updated", 200
	if creating {
		action, status = "role.created", 201
	} else if deleting {
		action = "role.deleted"
	}
	s.audit(r, p, action, p.TenantID, id)
	respond(w, status, map[string]string{"id": id})
}

func resolveRole(tx *sql.Tx, tenantID, requested string) (string, any, error) {
	if requested == "admin" || requested == "operator" {
		return requested, nil, nil
	}
	if _, err := uuid.Parse(requested); err != nil {
		return "", nil, sql.ErrNoRows
	}
	var id string
	err := tx.QueryRow(`select id from organization_roles where tenant_id=$1 and id=$2`, tenantID, requested).Scan(&id)
	return "operator", id, err
}
