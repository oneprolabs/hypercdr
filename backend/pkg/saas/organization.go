package saas

import (
	"database/sql"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platform"
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
	var name, description string
	var created time.Time
	var count int
	e := s.db.QueryRowContext(r.Context(), `select name,coalesce(description,''),created_at,(select count(*) from users where tenant_id=$1 and not is_system_admin) from resource_scopes where id=$1`, p.TenantID).Scan(&name, &description, &created, &count)
	if e != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"name": name, "description": description, "createdAt": created, "memberCount": count, "role": p.Role, "roles": []map[string]string{{"name": p.Role, "roleType": "organization"}}, "canEdit": p.Role == "admin" || p.SystemAdmin})
}
func (s *Service) members(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	rows, e := s.db.QueryContext(r.Context(), `select id,email,coalesce(display_name,''),role,status from users where tenant_id=$1 and not is_system_admin order by created_at,id`, p.TenantID)
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var id, email, name, role, status string
		if rows.Scan(&id, &email, &name, &role, &status) != nil {
			internal(w)
			return
		}
		items = append(items, map[string]string{"id": id, "email": email, "displayName": name, "role": role, "status": status})
	}
	if rows.Err() != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (s *Service) updateMember(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !admin(w, p) {
		return
	}
	var in struct {
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	if !decode(r, &in) || (in.Role != "admin" && in.Role != "operator") || (in.Status != "active" && in.Status != "disabled") {
		problem(w, 400, "invalid_member", "Select a valid role and status.")
		return
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	// Serialize changes per organization to protect the last active administrator.
	var scope string
	if e = tx.QueryRowContext(r.Context(), `select id from resource_scopes where id=$1 for update`, p.TenantID).Scan(&scope); e != nil {
		internal(w)
		return
	}
	var oldRole, oldStatus string
	e = tx.QueryRowContext(r.Context(), `select role,status from users where id=$1 and tenant_id=$2 and not is_system_admin for update`, r.PathValue("id"), p.TenantID).Scan(&oldRole, &oldStatus)
	if e == sql.ErrNoRows {
		problem(w, 404, "member_not_found", "Member not found.")
		return
	}
	if e != nil {
		internal(w)
		return
	}
	if oldRole == "admin" && oldStatus == "active" && (in.Role != "admin" || in.Status != "active") {
		var count int
		e = tx.QueryRowContext(r.Context(), `select count(*) from users where tenant_id=$1 and role='admin' and status='active' and not is_system_admin`, p.TenantID).Scan(&count)
		if e != nil {
			internal(w)
			return
		}
		if count <= 1 {
			problem(w, 409, "last_administrator", "Keep at least one active organization administrator.")
			return
		}
	}
	_, e = tx.ExecContext(r.Context(), `update users set role=$1,status=$2,updated_at=now() where id=$3 and tenant_id=$4`, in.Role, in.Status, r.PathValue("id"), p.TenantID)
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
	respond(w, 200, map[string]bool{"updated": true})
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
	if !decode(r, &in) || (in.RoleID != "admin" && in.RoleID != "operator") {
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
	var exists bool
	e = tx.QueryRowContext(r.Context(), `select exists(select 1 from users where lower(email)=$1)`, email).Scan(&exists)
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
		_, e = tx.ExecContext(r.Context(), `insert into saas_onboarding_tokens(token_hash,kind,email,tenant_id,role_id,created_by,expires_at) values($1,'invitation',$2,$3,$4,$5,now()+interval '7 days')`, digest(token), email, p.TenantID, in.RoleID, p.ID)
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
	respond(w, 201, map[string]string{"message": "Invitation sent."})
}
func (s *Service) listInvitations(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if !admin(w, p) {
		return
	}
	rows, e := s.db.QueryContext(r.Context(), `select token_hash,email,role_id,created_at,expires_at,case when consumed_at is not null then 'closed' when expires_at<now() then 'expired' else 'pending' end from saas_onboarding_tokens where kind='invitation' and tenant_id=$1 order by created_at desc limit 100`, p.TenantID)
	if e != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, email, role, status string
		var created, expires time.Time
		if rows.Scan(&id, &email, &role, &created, &expires, &status) != nil {
			internal(w)
			return
		}
		items = append(items, map[string]any{"id": id, "email": email, "role": role, "roleType": "organization", "createdAt": created, "expiresAt": expires, "status": status})
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
	respond(w, 200, map[string]bool{"revoked": true})
}
func (s *Service) audit(r *http.Request, p platform.Principal, action, tenant, target string) {
	if ci, ok := s.identity.(communityIdentity); ok {
		_, _ = ci.repo.CreateAuditLog(store.AuditLogInput{ActorID: p.ID, Actor: p.Email, Action: action, ResourceType: "identity", ResourceID: target, Result: "Success"})
	}
}
