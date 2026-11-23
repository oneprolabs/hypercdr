package saas

import (
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platform"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func admin(w http.ResponseWriter, p platform.Principal) bool {
	if p.Role != "admin" && !p.SystemAdmin {
		problem(w, 403, "administrator_required", "Organization administrator permission is required.")
		return false
	}
	return true
}
func (s *Service) organization(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if r.Method == http.MethodPatch {
		if !admin(w, p) {
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if !decode(r, &in) || strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 100 || len([]rune(in.Description)) > 500 {
			problem(w, 400, "invalid_organization", "Enter a name of up to 100 characters and description of up to 500 characters.")
			return
		}
		if _, e := s.db.ExecContext(r.Context(), `update resource_scopes set name=$1,description=$2,updated_at=now() where id=$3`, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), p.TenantID); e != nil {
			internal(w)
			return
		}
	}
	if r.Method == http.MethodPatch {
		s.audit(r, p, "organization.updated", p.TenantID, p.TenantID)
	}
	var name, description string
	var created time.Time
	var count int
	e := s.db.QueryRowContext(r.Context(), `select name,coalesce(description,''),created_at,(select count(*) from users where tenant_id=$1) from resource_scopes where id=$1`, p.TenantID).Scan(&name, &description, &created, &count)
	if e != nil {
		internal(w)
		return
	}
	var roleName string
	if err := s.db.QueryRowContext(r.Context(), `select coalesce(r.name,case when u.role='admin' then 'Administrator' else 'Operator' end) from users u left join organization_roles r on r.id=u.custom_role_id and r.tenant_id=u.tenant_id where u.id=$1 and u.tenant_id=$2`, p.ID, p.TenantID).Scan(&roleName); err != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"name": name, "description": description, "createdAt": created, "memberCount": count, "role": p.Role, "roles": []map[string]string{{"name": roleName, "roleType": "organization"}}, "canEdit": p.Role == "admin" || p.SystemAdmin})
}
func (s *Service) members(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	rows, e := s.db.QueryContext(r.Context(), `select u.id,u.username,coalesce(u.email,''),coalesce(u.display_name,''),u.role,u.status,u.is_system_admin,coalesce(u.custom_role_id::text,u.role),coalesce(r.name,case when u.role='admin' then 'Administrator' else 'Operator' end) from users u left join organization_roles r on r.id=u.custom_role_id and r.tenant_id=u.tenant_id where u.tenant_id=$1 order by u.created_at,u.id`, p.TenantID)
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, username, email, name, role, status, roleID, roleName string
		var systemAdmin bool
		if rows.Scan(&id, &username, &email, &name, &role, &status, &systemAdmin, &roleID, &roleName) != nil {
			internal(w)
			return
		}
		items = append(items, map[string]any{"id": id, "username": username, "email": email, "displayName": name, "role": role, "roleId": roleID, "roleName": roleName, "status": status, "systemAdmin": systemAdmin})
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (s *Service) updateMember(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	s.mutateMember(w, r, p)
}
func (s *Service) invite(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !admin(w, p) {
		return
	}
	if !s.mailConfigured() {
		problem(w, 503, "mail_not_configured", "Configure system SMTP before sending invitations.")
		return
	}
	var in struct {
		Email  string `json:"email"`
		RoleID string `json:"roleId"`
	}
	if !decode(r, &in) || in.RoleID == "" {
		problem(w, 400, "invalid_invitation", "Enter an email and select a role.")
		return
	}
	email, ok := emailAddress(in.Email)
	if !ok {
		problem(w, 400, "invalid_email", "Enter a valid email address.")
		return
	}
	if !s.allow(r.Context(), "invite:"+p.ID, 20, time.Hour) {
		problem(w, 429, "too_many_attempts", "Please wait before sending more invitations.")
		return
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var name string
	e = tx.QueryRowContext(r.Context(), `select name from resource_scopes where id=$1 and status='active' for update`, p.TenantID).Scan(&name)
	if e != nil {
		internal(w)
		return
	}
	role, customRoleID, roleErr := resolveRole(tx, p.TenantID, in.RoleID)
	if errors.Is(roleErr, sql.ErrNoRows) {
		problem(w, 400, "invalid_invitation_role", "Select a role belonging to this organization.")
		return
	}
	if roleErr != nil {
		internal(w)
		return
	}
	var administratorCount int
	if e = tx.QueryRowContext(r.Context(), `select count(*) from users where tenant_id=$1 and role='admin' and status='active' and not is_system_admin`, p.TenantID).Scan(&administratorCount); e != nil {
		internal(w)
		return
	}
	if administratorCount == 0 && in.RoleID != "admin" {
		problem(w, 409, "administrator_required", "Invite an organization administrator before adding members.")
		return
	}
	var exists bool
	e = tx.QueryRowContext(r.Context(), `select exists(select 1 from users where (lower(email)=$1 or lower(username)=$1))`, email).Scan(&exists)
	if e != nil {
		internal(w)
		return
	}
	if exists {
		problem(w, 409, "account_already_has_organization", "This account already belongs to an organization and cannot join another.")
		return
	}
	token := randomToken()
	_, e = tx.ExecContext(r.Context(), `update saas_onboarding_tokens set consumed_at=now() where tenant_id=$1 and email=$2 and kind='invitation' and consumed_at is null`, p.TenantID, email)
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `insert into saas_onboarding_tokens(token_hash,kind,email,tenant_id,role_id,created_by,expires_at,custom_role_id) values($1,'invitation',$2,$3,$4,$5,now()+interval '7 days',$6)`, digest(token), email, p.TenantID, role, p.ID, customRoleID)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		internal(w)
		return
	}
	if e = s.cfg.Mailer.Send(r.Context(), email, "Invitation to join HyperCDR", "You are invited to join "+name+".\n\n"+s.cfg.Origin+"/join?token="+token+"\n\nThis invitation expires in 7 days and is valid only for "+email+"."); e != nil {
		_, _ = s.db.ExecContext(r.Context(), `update saas_onboarding_tokens set consumed_at=now() where token_hash=$1`, digest(token))
		problem(w, 503, "email_delivery_failed", "Invitation delivery failed. Send a new invitation.")
		return
	}
	s.audit(r, p, "member.invited", p.TenantID, email)
	respond(w, 201, map[string]string{"message": "Invitation sent."})
}
func (s *Service) listInvitations(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !admin(w, p) {
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `select t.token_hash,t.email,t.role_id,t.created_at,t.expires_at,case when t.consumed_at is not null then 'closed' when t.expires_at<now() then 'expired' else 'pending' end,coalesce(t.custom_role_id::text,t.role_id),coalesce(r.name,case when t.role_id='admin' then 'Administrator' else 'Operator' end) from saas_onboarding_tokens t left join organization_roles r on r.id=t.custom_role_id and r.tenant_id=t.tenant_id where t.kind='invitation' and t.tenant_id=$1 order by t.created_at desc limit 100`, p.TenantID)
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, email, role, status, roleID, roleName string
		var created, expires time.Time
		if rows.Scan(&id, &email, &role, &created, &expires, &status, &roleID, &roleName) != nil {
			internal(w)
			return
		}
		items = append(items, map[string]any{"id": id, "email": email, "role": role, "roleId": roleID, "roleName": roleName, "roleType": "organization", "createdAt": created, "expiresAt": expires, "status": status})
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"items": items, "emailConfigured": s.mailConfigured()})
}
func (s *Service) revokeInvitation(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !admin(w, p) {
		return
	}
	res, e := s.db.ExecContext(r.Context(), `update saas_onboarding_tokens set consumed_at=now() where token_hash=$1 and tenant_id=$2 and kind='invitation' and consumed_at is null`, r.PathValue("id"), p.TenantID)
	if e != nil {
		internal(w)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		problem(w, 404, "invitation_not_found", "Invitation not found.")
		return
	}
	s.audit(r, p, "invitation.revoked", p.TenantID, r.PathValue("id"))
	respond(w, 200, map[string]bool{"revoked": true})
}
func (s *Service) audit(r *http.Request, p platform.Principal, action, tenant, target string) {
	if ci, ok := s.identity.(communityIdentity); ok {
		actorOrganization := p.TenantID
		_ = s.db.QueryRowContext(r.Context(), `select tenant_id from users where id=$1`, p.ID).Scan(&actorOrganization)
		resourceID := ""
		result := "Success"
		if strings.HasSuffix(action, ".activation_denied") {
			result = "Denied"
		}
		if id, err := uuid.Parse(target); err == nil {
			resourceID = id.String()
		}
		if _, err := ci.repo.CreateAuditLog(store.AuditLogInput{ActorID: p.ID, Actor: actorLogin(p), Action: action, ResourceType: "identity", ResourceID: resourceID, ResourceName: target, Result: result, Payload: map[string]any{"targetOrganizationId": tenant, "actorOrganizationId": actorOrganization, "target": target}}); err != nil {
			slog.Error("identity audit recording failed", "action", action, "error", err)
		}
	}
}

func actorLogin(p platform.Principal) string {
	if p.Username != "" {
		return p.Username
	}
	if p.Email != "" {
		return p.Email
	}
	return p.ID
}
