package saas

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"hypercdr-platform/platform/backend/pkg/googleidentity"
	"hypercdr-platform/platform/backend/pkg/platform"
)

type googleProvider = googleidentity.Provider
type googleClaims = googleidentity.Claims

func newGoogleProvider(c Config) *googleProvider {
	return googleidentity.New(c.GoogleClientID, c.GoogleClientSecret, c.Origin+"/api/v1/saas/auth/google/callback")
}
func (s *Service) googleStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Invitation string `json:"invitation"`
	}
	if !decode(r, &in) {
		problem(w, 400, "invalid_request", "Reload the sign-in page.")
		return
	}
	s.startGoogle(w, r, in.Invitation, "")
}
func (s *Service) startGoogle(w http.ResponseWriter, r *http.Request, invitation, linkUser string) {
	if s.google == nil {
		problem(w, 503, "google_not_configured", "Google sign-in is not configured.")
		return
	}
	var invitationHash any
	if invitation != "" {
		var exists bool
		e := s.db.QueryRowContext(r.Context(), `select exists(select 1 from saas_onboarding_tokens tok join resource_scopes t on t.id=tok.tenant_id where tok.token_hash=$1 and tok.kind='invitation' and tok.consumed_at is null and tok.expires_at>now() and t.status='active')`, digest(invitation)).Scan(&exists)
		if e != nil || !exists {
			invalidToken(w)
			return
		}
		invitationHash = digest(invitation)
	}
	state, browser, nonce, verifier := randomToken(), randomToken(), randomToken(), oauth2.GenerateVerifier()
	var user any
	if linkUser != "" {
		user = linkUser
	}
	_, e := s.db.ExecContext(r.Context(), `insert into saas_oauth_requests(state_hash,browser_hash,nonce,verifier,invitation_hash,linking_user_id,expires_at) values($1,$2,$3,$4,$5,$6,now()+interval '10 minutes')`, digest(state), digest(browser), nonce, verifier, invitationHash, user)
	if e != nil {
		internal(w)
		return
	}
	setCookie(w, "hcdr_google_browser", browser, 600)
	authorize := s.google.OAuth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce), oauth2.SetAuthURLParam("prompt", "select_account"))
	respond(w, 200, map[string]string{"url": authorize})
}
func (s *Service) googleError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, s.cfg.Origin+"/login?oauth_error="+url.QueryEscape(code), http.StatusSeeOther)
}
func (s *Service) googleCallback(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		s.googleError(w, r, "google_not_configured")
		return
	}
	browser := cookie(r, "hcdr_google_browser")
	if browser == "" || r.URL.Query().Get("state") == "" {
		s.googleError(w, r, "authorization_expired")
		return
	}
	var nonce, verifier string
	var invitation, linking sql.NullString
	e := s.db.QueryRowContext(r.Context(), `delete from saas_oauth_requests where state_hash=$1 and browser_hash=$2 and expires_at>now() returning nonce,verifier,invitation_hash,linking_user_id::text`, digest(r.URL.Query().Get("state")), digest(browser)).Scan(&nonce, &verifier, &invitation, &linking)
	if e != nil {
		s.googleError(w, r, "authorization_expired")
		return
	}
	if r.URL.Query().Get("error") != "" {
		s.googleError(w, r, "authorization_cancelled")
		return
	}
	claims, e := s.google.Verify(r.Context(), r.URL.Query().Get("code"), verifier, nonce)
	if e != nil {
		s.googleError(w, r, "google_verification_failed")
		return
	}
	if linking.Valid {
		// Password was verified before authorization began. Both identities must be owned explicitly.
		var active bool
		e = s.db.QueryRowContext(r.Context(), `select exists(select 1 from users u join resource_scopes t on t.id=u.tenant_id where u.id=$1 and u.status='active' and (u.is_system_admin or t.status='active'))`, linking.String).Scan(&active)
		if e != nil || !active {
			s.googleError(w, r, "account_unavailable")
			return
		}
		_, e = s.db.ExecContext(r.Context(), `insert into saas_external_identities(provider,subject,user_id) values('google',$1,$2)`, claims.Subject, linking.String)
		if e != nil {
			s.googleError(w, r, "identity_already_linked")
			return
		}
		s.audit(r, platform.Principal{ID: linking.String, Email: claims.Email}, "identity.google.linked", s.tenantForUser(r.Context(), linking.String), claims.Email)
		http.Redirect(w, r, s.cfg.Origin+"/?google=linked", http.StatusSeeOther)
		return
	}
	var user, tenant string
	var active bool
	e = s.db.QueryRowContext(r.Context(), `select u.id::text,u.tenant_id::text,(u.status='active' and (u.is_system_admin or t.status='active')) from saas_external_identities i join users u on u.id=i.user_id join resource_scopes t on t.id=u.tenant_id where i.provider='google' and i.subject=$1`, claims.Subject).Scan(&user, &tenant, &active)
	if e != nil && e != sql.ErrNoRows {
		s.googleError(w, r, "identity_unavailable")
		return
	}
	if user != "" && !active {
		s.googleError(w, r, "account_unavailable")
		return
	}
	var invEmail, invTenant, invRole string
	if invitation.Valid {
		e = s.db.QueryRowContext(r.Context(), `select tok.email,tok.tenant_id::text,tok.role_id::text from saas_onboarding_tokens tok join resource_scopes t on t.id=tok.tenant_id where tok.token_hash=$1 and tok.kind='invitation' and tok.consumed_at is null and tok.expires_at>now() and t.status='active'`, invitation.String).Scan(&invEmail, &invTenant, &invRole)
		if e != nil || invEmail != claims.Email || (user != "" && tenant != invTenant) {
			s.googleError(w, r, "invitation_identity_mismatch")
			return
		}
	}
	token := randomToken()
	if user != "" {
		_, e = s.db.ExecContext(r.Context(), `insert into saas_onboarding_tokens(token_hash,kind,email,user_id,browser_hash,expires_at) values($1,'session',$2,$3,$4,now()+interval '2 minutes')`, digest(token), claims.Email, user, digest(browser))
		if e != nil {
			s.googleError(w, r, "identity_unavailable")
			return
		}
		setCookie(w, "hcdr_google_session", token, 120)
		http.Redirect(w, r, s.cfg.Origin+"/auth/complete", http.StatusSeeOther)
		return
	}
	var emailExists bool
	e = s.db.QueryRowContext(r.Context(), `select exists(select 1 from users where (lower(email)=$1 or lower(username)=$1))`, claims.Email).Scan(&emailExists)
	if e != nil {
		s.googleError(w, r, "identity_unavailable")
		return
	}
	if emailExists {
		s.googleError(w, r, "account_link_required")
		return
	}
	if !s.cfg.Signup && !invitation.Valid {
		s.googleError(w, r, "signup_disabled")
		return
	}
	var target, role any
	if invitation.Valid {
		target = invTenant
		role = invRole
	}
	_, e = s.db.ExecContext(r.Context(), `insert into saas_onboarding_tokens(token_hash,kind,email,tenant_id,role_id,browser_hash,provider_subject,expires_at) values($1,'google',$2,$3,$4,$5,$6,now()+interval '10 minutes')`, digest(token), claims.Email, target, role, digest(browser), claims.Subject)
	if e != nil {
		s.googleError(w, r, "identity_unavailable")
		return
	}
	setCookie(w, "hcdr_onboarding", token, 600)
	// Invitation token is retained in the originating tab's sessionStorage, never sent to Google.
	path := "/signup?provider=google"
	if invitation.Valid {
		path = "/join?provider=google"
	}
	http.Redirect(w, r, s.cfg.Origin+path, http.StatusSeeOther)
}
func (s *Service) googleSession(w http.ResponseWriter, r *http.Request) {
	raw, browser := cookie(r, "hcdr_google_session"), cookie(r, "hcdr_google_browser")
	if raw == "" || browser == "" {
		invalidToken(w)
		return
	}
	var user string
	e := s.db.QueryRowContext(r.Context(), `update saas_onboarding_tokens set consumed_at=now() where token_hash=$1 and kind='session' and browser_hash=$2 and consumed_at is null and expires_at>now() returning user_id::text`, digest(raw), digest(browser)).Scan(&user)
	if e != nil {
		invalidToken(w)
		return
	}
	setCookie(w, "hcdr_google_session", "", -1)
	s.session(w, r, user)
}
func (s *Service) googleProfile(w http.ResponseWriter, r *http.Request) {
	var email, tenant string
	e := s.db.QueryRowContext(r.Context(), `select tok.email,coalesce(t.name,'') from saas_onboarding_tokens tok left join resource_scopes t on t.id=tok.tenant_id where tok.token_hash=$1 and tok.kind='google' and tok.browser_hash=$2 and tok.consumed_at is null and tok.expires_at>now() and (tok.tenant_id is null or t.status='active')`, digest(cookie(r, "hcdr_onboarding")), digest(cookie(r, "hcdr_google_browser"))).Scan(&email, &tenant)
	if e != nil {
		invalidToken(w)
		return
	}
	respond(w, 200, map[string]string{"email": email, "organizationName": tenant})
}
func (s *Service) linkGoogle(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	if r.Header.Get("Origin") != s.cfg.Origin {
		problem(w, 403, "origin_not_allowed", "Reload the page and try again.")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !decode(r, &in) || !s.allow(r.Context(), "link:"+p.ID, 5, time.Hour) {
		problem(w, 400, "reauthentication_required", "Verify your current password before linking Google.")
		return
	}
	identity, ok, e := s.identity.Authenticate(r.Context(), p.Username, in.Password)
	if e != nil || !ok || identity.ID != p.ID {
		problem(w, 403, "reauthentication_required", "Your current password could not be verified.")
		return
	}
	s.startGoogle(w, r, "", p.ID)
}
func (s *Service) googleIdentity(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	var linked, passwordConfigured bool
	e := s.db.QueryRowContext(r.Context(), `select exists(select 1 from saas_external_identities where provider='google' and user_id=$1),exists(select 1 from users where id=$1 and password_hash like '$2%')`, p.ID).Scan(&linked, &passwordConfigured)
	if e != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]bool{"linked": linked, "enabled": s.google != nil, "passwordConfigured": passwordConfigured})
}
func (s *Service) tenantForUser(ctx context.Context, id string) string {
	var tenant string
	_ = s.db.QueryRowContext(ctx, `select tenant_id::text from users where id=$1`, id).Scan(&tenant)
	return tenant
}
