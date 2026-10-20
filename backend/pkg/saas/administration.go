package saas

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platform"
)

func systemAdministrator(w http.ResponseWriter, p platform.Principal) bool {
	if !p.SystemAdmin {
		problem(w, 403, "system_administrator_required", "Platform administrator permission is required.")
		return false
	}
	return true
}
func organizationID(w http.ResponseWriter, raw string) (string, bool) {
	id, e := uuid.Parse(raw)
	if e != nil {
		problem(w, 400, "invalid_organization", "Select a valid organization.")
		return "", false
	}
	return id.String(), true
}

// Explicit organization context is restricted to administration routes. It never
// changes the authenticated identity, its role, or the session's organization.
func (s *Service) organizationContext(w http.ResponseWriter, r *http.Request, p platform.Principal) (platform.Principal, bool) {
	if !systemAdministrator(w, p) {
		return p, false
	}
	id, ok := organizationID(w, r.PathValue("organization"))
	if !ok {
		return p, false
	}
	var exists bool
	if e := s.db.QueryRowContext(r.Context(), `select exists(select 1 from resource_scopes where id=$1)`, id).Scan(&exists); e != nil {
		internal(w)
		return p, false
	}
	if !exists {
		problem(w, 404, "organization_not_found", "Organization not found.")
		return p, false
	}
	p.TenantID = id
	return p, true
}
func (s *Service) administrationOrganizations(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !systemAdministrator(w, p) {
		return
	}
	if r.Method == http.MethodPost {
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if !decode(r, &in) || strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 100 || len([]rune(in.Description)) > 500 {
			problem(w, 400, "invalid_organization", "Enter an organization name of up to 100 characters.")
			return
		}
		id := uuid.NewString()
		_, e := s.db.ExecContext(r.Context(), `insert into resource_scopes(id,name,status,description) values($1,$2,'active',$3)`, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description))
		if e != nil {
			internal(w)
			return
		}
		s.audit(r, p, "organization.created", id, id)
		respond(w, 201, map[string]string{"id": id})
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `select t.id,t.name,t.status,coalesce(t.description,''),t.created_at,(select count(*) from users u where u.tenant_id=t.id),(select count(*) from users u where u.tenant_id=t.id and u.is_system_admin),(select count(*) from clusters c where c.tenant_id=t.id),(select count(*) from protection_plans a where a.tenant_id=t.id) from resource_scopes t order by t.created_at,t.id`)
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, status, description string
		var created time.Time
		var members, admins, clusters, plans int
		if rows.Scan(&id, &name, &status, &description, &created, &members, &admins, &clusters, &plans) != nil {
			internal(w)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "status": status, "description": description, "createdAt": created, "memberCount": members, "platformAdministratorCount": admins, "clusterCount": clusters, "protectedApplicationCount": plans, "systemDefault": id == store.DefaultTenantID})
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (s *Service) administrationOrganization(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	scoped, ok := s.organizationContext(w, r, p)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		s.organization(w, r, scoped)
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if !decode(r, &in) || strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 100 || len([]rune(in.Description)) > 500 || (in.Status != "active" && in.Status != "disabled") {
		problem(w, 400, "invalid_organization", "Enter a valid name, description and status.")
		return
	}
	if scoped.TenantID == store.DefaultTenantID && in.Status != "active" {
		problem(w, 409, "protected_organization", "The system default organization cannot be disabled.")
		return
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRowContext(r.Context(), `select id from resource_scopes where id=$1 for update`, scoped.TenantID).Scan(&id); e == nil {
		_, e = tx.ExecContext(r.Context(), `update resource_scopes set name=$1,description=$2,status=$3,updated_at=now() where id=$4`, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), in.Status, id)
	}
	if e == nil && in.Status == "disabled" {
		_, e = tx.ExecContext(r.Context(), `delete from platform_sessions where user_id in (select id from users where tenant_id=$1 and not is_system_admin)`, id)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		internal(w)
		return
	}
	s.audit(r, p, "organization.updated", id, id)
	respond(w, 200, map[string]bool{"updated": true})
}
func (s *Service) administrationUsers(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !systemAdministrator(w, p) {
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `select u.id,u.username,coalesce(u.email,''),coalesce(u.display_name,''),u.role,u.status,u.is_system_admin,u.tenant_id,t.name,t.status,u.created_at from users u join resource_scopes t on t.id=u.tenant_id order by u.is_system_admin desc,u.created_at,u.id`)
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, username, email, name, role, status, org, orgName, orgStatus string
		var sys bool
		var created time.Time
		if rows.Scan(&id, &username, &email, &name, &role, &status, &sys, &org, &orgName, &orgStatus, &created) != nil {
			internal(w)
			return
		}
		items = append(items, map[string]any{"id": id, "username": username, "email": email, "displayName": name, "role": role, "status": status, "systemAdmin": sys, "organizationId": org, "organizationName": orgName, "organizationStatus": orgStatus, "createdAt": created})
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (s *Service) administrationMember(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	scoped, ok := s.organizationContext(w, r, p)
	if !ok {
		return
	}
	s.mutateMember(w, r, scoped)
}
func (s *Service) mutateMember(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !admin(w, p) {
		return
	}
	if _, e := uuid.Parse(r.PathValue("id")); e != nil {
		problem(w, 404, "member_not_found", "Member not found.")
		return
	}
	var in struct {
		DisplayName *string `json:"displayName"`
		Role        string  `json:"role"`
		Status      string  `json:"status"`
	}
	deleting := r.Method == http.MethodDelete
	if !deleting && (!decode(r, &in) || (in.Role != "admin" && in.Role != "operator") || (in.Status != "active" && in.Status != "disabled") || (in.DisplayName != nil && len([]rune(*in.DisplayName)) > 100)) {
		problem(w, 400, "invalid_member", "Enter a name of up to 100 characters and select a valid role and status.")
		return
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var scope, status string
	if e = tx.QueryRowContext(r.Context(), `select id,status from resource_scopes where id=$1 for update`, p.TenantID).Scan(&scope, &status); e != nil {
		internal(w)
		return
	}
	if status != "active" && !p.SystemAdmin {
		problem(w, 403, "organization_disabled", "This organization is disabled.")
		return
	}
	var role, oldStatus string
	var sys bool
	e = tx.QueryRowContext(r.Context(), `select role,status,is_system_admin from users where id=$1 and tenant_id=$2 for update`, r.PathValue("id"), scope).Scan(&role, &oldStatus, &sys)
	if errors.Is(e, sql.ErrNoRows) {
		problem(w, 404, "member_not_found", "Member not found.")
		return
	}
	if e != nil {
		internal(w)
		return
	}
	if sys {
		problem(w, 409, "protected_platform_administrator", "The built-in platform administrator is protected. Use personal settings to update your account.")
		return
	}
	if p.ID == r.PathValue("id") && (deleting || in.Role != role || in.Status != oldStatus) {
		problem(w, 409, "self_account_protected", "You cannot delete or disable your own account or change your own role. Use Personal Information to update your profile.")
		return
	}
	if role == "admin" && oldStatus == "active" && (deleting || in.Role != "admin" || in.Status != "active") {
		var n int
		e = tx.QueryRowContext(r.Context(), `select count(*) from users where tenant_id=$1 and role='admin' and status='active' and not is_system_admin`, scope).Scan(&n)
		if e != nil {
			internal(w)
			return
		}
		if n <= 1 {
			problem(w, 409, "last_administrator", "Keep at least one active organization administrator.")
			return
		}
	}
	if deleting {
		// Preserve historical attribution where possible. Old onboarding links cannot
		// become usable again after the deleted account's email is registered anew.
		_, e = tx.ExecContext(r.Context(), `update saas_onboarding_tokens set created_by=null,consumed_at=coalesce(consumed_at,now()) where created_by=$1 or email=(select email from users where id=$1)`, r.PathValue("id"))
		if e == nil {
			_, e = tx.ExecContext(r.Context(), `delete from users where id=$1 and tenant_id=$2`, r.PathValue("id"), scope)
		}
	} else {
		_, e = tx.ExecContext(r.Context(), `update users set role=$1,status=$2,display_name=coalesce($3,display_name),updated_at=now() where id=$4 and tenant_id=$5`, in.Role, in.Status, in.DisplayName, r.PathValue("id"), scope)
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `delete from platform_sessions where user_id=$1`, r.PathValue("id"))
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		internal(w)
		return
	}
	action := "member.updated"
	if deleting {
		action = "member.deleted"
	}
	s.audit(r, p, action, scope, r.PathValue("id"))
	respond(w, 200, map[string]bool{"updated": true})
}
func (s *Service) administrationMembers(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if scoped, ok := s.organizationContext(w, r, p); ok {
		s.members(w, r, scoped)
	}
}
func (s *Service) administrationInvitation(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if scoped, ok := s.organizationContext(w, r, p); ok {
		if r.Method == http.MethodGet {
			s.listInvitations(w, r, scoped)
		} else {
			s.invite(w, r, scoped)
		}
	}
}
func (s *Service) administrationRevoke(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if scoped, ok := s.organizationContext(w, r, p); ok {
		s.revokeInvitation(w, r, scoped)
	}
}
