package saas

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

func (s *Service) requestSignup(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Signup {
		problem(w, 403, "signup_disabled", "Public registration is not enabled.")
		return
	}
	if !s.mailConfigured() {
		problem(w, 503, "mail_not_configured", "Email registration is not configured. Contact the platform administrator.")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if !decode(r, &in) {
		problem(w, 400, "invalid_request", "Enter a valid email address.")
		return
	}
	email, ok := emailAddress(in.Email)
	if !ok {
		problem(w, 400, "invalid_email", "Enter a valid email address.")
		return
	}
	if !s.allow(r.Context(), "signup-attempt:"+email, 10, time.Hour) {
		s.signupLimited(w, r, "signup-attempt:"+email)
		return
	}
	var exists bool
	if e := s.db.QueryRowContext(r.Context(), `select exists(select 1 from users where (lower(email)=$1 or lower(username)=$1))`, email).Scan(&exists); e != nil {
		internal(w)
		return
	}
	if exists {
		problem(w, 409, "email_already_registered", "This email is already registered. Sign in or reset your password.")
		return
	}
	if !s.allow(r.Context(), "signup-resend:"+email, 1, time.Minute) {
		s.signupLimited(w, r, "signup-resend:"+email)
		return
	}
	if !s.allow(r.Context(), "signup:"+email, 3, time.Hour) {
		_, _ = s.db.ExecContext(r.Context(), `delete from saas_auth_rate_limits where bucket=$1`, digest("signup-resend:"+email))
		s.signupLimited(w, r, "signup:"+email)
		return
	}
	token := randomToken()
	_, e := s.db.ExecContext(r.Context(), `insert into saas_onboarding_tokens(token_hash,kind,email,expires_at) values($1,'signup',$2,now()+interval '30 minutes')`, digest(token), email)
	if e != nil {
		s.releaseSignupSend(email, token)
		internal(w)
		return
	}
	if e = s.cfg.Mailer.Send(r.Context(), email, "Verify your HyperCDR email", "Verify your email and create your organization:\n\n"+s.cfg.Origin+"/signup?token="+token+"\n\nThis link expires in 30 minutes. If you did not request it, ignore this email."); e != nil {
		s.releaseSignupSend(email, token)
		slog.Warn("signup email delivery failed")
		problem(w, 503, "email_delivery_failed", "The email could not be sent. Please try again later.")
		return
	}
	respond(w, 202, map[string]any{"message": "Verification email sent to " + email + ". This link expires in 30 minutes. Check your inbox and spam folder.", "email": email, "retryAfterSeconds": 60})
}

func (s *Service) releaseSignupSend(email, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, `delete from saas_onboarding_tokens where token_hash=$1`, digest(token))
	_, _ = s.db.ExecContext(ctx, `update saas_auth_rate_limits set count=greatest(count-1,0) where bucket=$1`, digest("signup:"+email))
	_, _ = s.db.ExecContext(ctx, `delete from saas_auth_rate_limits where bucket=$1`, digest("signup-resend:"+email))
}

func (s *Service) signupLimited(w http.ResponseWriter, r *http.Request, key string) {
	var seconds int
	if err := s.db.QueryRowContext(r.Context(), `select greatest(1,ceil(extract(epoch from (expires_at-now()))))::int from saas_auth_rate_limits where bucket=$1`, digest(key)).Scan(&seconds); err != nil {
		internal(w)
		return
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	minutes := (seconds + 59) / 60
	unit := "minute"
	if minutes != 1 {
		unit = "minutes"
	}
	respond(w, 429, map[string]any{"error": "too_many_attempts", "message": "Too many registration requests. Try again in " + strconv.Itoa(minutes) + " " + unit + ".", "retryAfterSeconds": seconds})
}

type onboardingToken struct {
	Kind, Email, Subject        string
	Tenant, Role, User, Browser sql.NullString
	CustomRole                  sql.NullString
}

func readToken(ctx context.Context, tx *sql.Tx, hash string) (onboardingToken, error) {
	var t onboardingToken
	e := tx.QueryRowContext(ctx, `select kind,email,tenant_id::text,role_id::text,user_id::text,browser_hash,coalesce(provider_subject,''),custom_role_id::text from saas_onboarding_tokens where token_hash=$1 and consumed_at is null and expires_at>now() for update`, hash).Scan(&t.Kind, &t.Email, &t.Tenant, &t.Role, &t.User, &t.Browser, &t.Subject, &t.CustomRole)
	return t, e
}
func (s *Service) complete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token        string `json:"token"`
		Organization string `json:"organizationName"`
		DisplayName  string `json:"displayName"`
		Password     string `json:"password"`
		Invitation   string `json:"invitation"`
	}
	if !decode(r, &in) {
		problem(w, 400, "invalid_request", "Check the form and try again.")
		return
	}
	raw := in.Token
	google := false
	if raw == "" {
		raw = cookie(r, "hcdr_onboarding")
		google = true
	}
	if raw == "" {
		problem(w, 400, "verification_required", "Open your verification or invitation link first.")
		return
	}
	if len(in.Password) > 72 || (!google && utf8.RuneCountInString(in.Password) < 8) || utf8.RuneCountInString(in.DisplayName) > 100 || utf8.RuneCountInString(in.Organization) > 100 {
		problem(w, 400, "invalid_input", "Use at least 8 password characters (at most 72 UTF-8 bytes) and names of at most 100 characters.")
		return
	}
	hash := "!google-only"
	if !google || in.Password != "" {
		if utf8.RuneCountInString(in.Password) < 8 {
			problem(w, 400, "weak_password", "Use at least 8 characters.")
			return
		}
		b, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if e != nil {
			internal(w)
			return
		}
		hash = string(b)
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	t, e := readToken(r.Context(), tx, digest(raw))
	if e != nil {
		invalidToken(w)
		return
	}
	if (google && t.Kind != "google") || (!google && t.Kind != "signup" && t.Kind != "invitation") {
		invalidToken(w)
		return
	}
	if google && (t.Browser.String == "" || digest(cookie(r, "hcdr_google_browser")) != t.Browser.String) {
		invalidToken(w)
		return
	}
	inviteHash := ""
	if google && t.Tenant.Valid {
		if in.Invitation == "" {
			invalidToken(w)
			return
		}
		inviteHash = digest(in.Invitation)
		inv, err := readToken(r.Context(), tx, inviteHash)
		if err != nil || inv.Kind != "invitation" || inv.Email != t.Email || inv.Tenant.String != t.Tenant.String {
			invalidToken(w)
			return
		}
		t.Role = inv.Role
		t.CustomRole = inv.CustomRole
	}
	tenant := t.Tenant.String
	if tenant == "" {
		if !s.cfg.Signup {
			problem(w, 403, "signup_disabled", "Public registration is disabled.")
			return
		}
		in.Organization = strings.TrimSpace(in.Organization)
		if in.Organization == "" {
			problem(w, 400, "organization_required", "Enter your organization name.")
			return
		}
		tenant = uuid.NewString()
		_, e = tx.ExecContext(r.Context(), `insert into resource_scopes(id,name,status) values($1,$2,'active')`, tenant, in.Organization)
		t.Role = sql.NullString{String: "admin", Valid: true}
	} else {
		var active bool
		e = tx.QueryRowContext(r.Context(), `select status='active' from resource_scopes where id=$1 for share`, tenant).Scan(&active)
		if e == nil && !active {
			invalidToken(w)
			return
		}
	}
	if e != nil {
		internal(w)
		return
	}
	if t.Role.String != "admin" && t.Role.String != "operator" {
		invalidToken(w)
		return
	}
	user := uuid.NewString()
	provider := "password"
	if google {
		provider = "google"
	}
	_, e = tx.ExecContext(r.Context(), `insert into users(id,tenant_id,username,email,email_verified_at,display_name,password_hash,role,status,must_change_password,auth_provider,custom_role_id) values($1,$2,$3,$3,now(),nullif($4,''),$5,$6,'active',false,$7,$8)`, user, tenant, t.Email, strings.TrimSpace(in.DisplayName), hash, t.Role.String, provider, t.CustomRole)
	if e == nil && google {
		_, e = tx.ExecContext(r.Context(), `insert into saas_external_identities(provider,subject,user_id) values('google',$1,$2)`, t.Subject, user)
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `update saas_onboarding_tokens set consumed_at=now() where token_hash=$1 or (kind='invitation' and token_hash=$2)`, digest(raw), inviteHash)
	}
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			problem(w, 409, "account_already_has_organization", "This account already belongs to an organization. Sign in to that account.")
			return
		}
		slog.Error("onboarding transaction failed", "error", e)
		internal(w)
		return
	}
	if e = tx.Commit(); e != nil {
		internal(w)
		return
	}
	setCookie(w, "hcdr_onboarding", "", -1)
	s.startSession(w, r, user, true)
}
func (s *Service) session(w http.ResponseWriter, r *http.Request, user string) {
	s.startSession(w, r, user, false)
}
func (s *Service) startSession(w http.ResponseWriter, r *http.Request, user string, accountCreated bool) {
	failed := func() {
		if accountCreated {
			problem(w, 503, "account_created_login_failed", "Your account was created, but automatic sign-in could not be completed. Sign in to continue.")
			return
		}
		internal(w)
	}
	sess, e := s.identity.CreateSession(r.Context(), user, 8*time.Hour)
	if e != nil {
		failed()
		return
	}
	identity, ok, e := s.identity.AuthenticateSession(r.Context(), sess.Token)
	if e != nil || !ok {
		_ = s.identity.DeleteSession(r.Context(), sess.Token)
		if accountCreated {
			failed()
			return
		}
		problem(w, 403, "account_unavailable", "Your account is not available. Contact the platform administrator.")
		return
	}
	respond(w, 200, map[string]any{"user": identity, "session": map[string]any{"token": sess.Token, "expiresAt": sess.ExpiresAt}})
}
func internal(w http.ResponseWriter) {
	problem(w, 500, "identity_operation_failed", "The operation could not be completed. Please try again later.")
}
func invalidToken(w http.ResponseWriter) {
	problem(w, 400, "link_invalid", "This link is invalid, expired or already used. Request a new link.")
}
func (s *Service) invitationInfo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(r, &in) {
		invalidToken(w)
		return
	}
	var email, name, role, roleType string
	e := s.db.QueryRowContext(r.Context(), `select tok.email,t.name,tok.role_id,'organization' from saas_onboarding_tokens tok join resource_scopes t on t.id=tok.tenant_id where tok.token_hash=$1 and tok.kind='invitation' and tok.consumed_at is null and tok.expires_at>now() and t.status='active'`, digest(in.Token)).Scan(&email, &name, &role, &roleType)
	if e != nil {
		invalidToken(w)
		return
	}
	respond(w, 200, map[string]string{"email": email, "organizationName": name, "role": role, "roleType": roleType})
}

func (s *Service) verificationInfo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(r, &in) {
		invalidToken(w)
		return
	}
	var email string
	e := s.db.QueryRowContext(r.Context(), `select email from saas_onboarding_tokens where token_hash=$1 and kind='signup' and consumed_at is null and expires_at>now()`, digest(in.Token)).Scan(&email)
	if e != nil {
		invalidToken(w)
		return
	}
	respond(w, 200, map[string]string{"email": email})
}
