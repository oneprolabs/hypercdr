package saas

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/pkg/platform"
)

func (s *Service) boundEmail(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	var email string
	var verified sql.NullTime
	if err := s.db.QueryRowContext(r.Context(), `select coalesce(email,''),email_verified_at from users where id=$1`, p.ID).Scan(&email, &verified); err != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]any{"email": email, "verified": verified.Valid})
}

func (s *Service) requestBoundEmail(w http.ResponseWriter, r *http.Request, p platform.Principal) {
	var in struct{ Email, Password string }
	if !decode(r, &in) {
		problem(w, 400, "invalid_request", "Enter a valid email address.")
		return
	}
	email, ok := emailAddress(in.Email)
	if !ok {
		problem(w, 400, "invalid_email", "Enter a valid email address.")
		return
	}
	// A stolen unattended browser session alone must not authorize changing the
	// recovery destination. Google-only users verify their existing email first.
	var username, hash, previous string
	if err := s.db.QueryRowContext(r.Context(), `select username,password_hash,coalesce(email,'') from users where id=$1`, p.ID).Scan(&username, &hash, &previous); err != nil {
		internal(w)
		return
	}
	if strings.HasPrefix(hash, "!google") {
		problem(w, 409, "password_required", "Set up a password using password recovery before changing your bound email.")
		return
	}
	identity, valid, err := s.identity.Authenticate(r.Context(), username, in.Password)
	if err != nil || !valid || identity.ID != p.ID {
		problem(w, 400, "current_password_invalid", "Current password is incorrect.")
		return
	}
	if email == previous {
		problem(w, 400, "email_unchanged", "This email is already bound to your account.")
		return
	}
	var exists bool
	if err = s.db.QueryRowContext(r.Context(), `select exists(select 1 from users where id<>$1 and (lower(username)=$2 or lower(email)=$2))`, p.ID, email).Scan(&exists); err != nil {
		internal(w)
		return
	}
	if exists {
		problem(w, 409, "email_in_use", "This email belongs to another account.")
		return
	}
	if !s.mailConfigured() {
		problem(w, 503, "mail_not_configured", "Email delivery is not configured.")
		return
	}
	if !s.allow(r.Context(), "bind-email:"+p.ID, 3, time.Hour) {
		problem(w, 429, "too_many_attempts", "Please wait before requesting another email.")
		return
	}
	token := randomToken()
	// New requests invalidate older links. Existing email remains valid until
	// the destination has been verified successfully.
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `delete from user_email_verifications where user_id=$1`, p.ID); err == nil {
		_, err = tx.ExecContext(r.Context(), `insert into user_email_verifications(token_hash,user_id,email,previous_email,expires_at) values($1,$2,$3,$4,now()+interval '30 minutes')`, digest(token), p.ID, email, previous)
	}
	if err != nil {
		internal(w)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w)
		return
	}
	if err = s.cfg.Mailer.Send(r.Context(), email, "Verify your HyperCDR bound email", "Confirm this address for sign-in and password recovery:\n\n"+s.cfg.Origin+"/verify-email?token="+token+"\n\nThis link expires in 30 minutes. If you did not request it, ignore this email."); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.db.ExecContext(ctx, `delete from user_email_verifications where token_hash=$1`, digest(token))
		_, _ = s.db.ExecContext(ctx, `update saas_auth_rate_limits set count=greatest(count-1,0) where bucket=$1`, digest("bind-email:"+p.ID))
		problem(w, 503, "email_delivery_failed", "The verification email could not be sent. Your bound email was not changed.")
		return
	}
	s.audit(r, p, "identity.email.verification_requested", p.TenantID, p.ID)
	respond(w, 202, map[string]string{"message": "Verification email sent. Your bound email changes only after confirmation."})
}

func (s *Service) verifyBoundEmail(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token string }
	if !decode(r, &in) || in.Token == "" {
		invalidToken(w)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w)
		return
	}
	defer tx.Rollback()
	var id, email, previous string
	err = tx.QueryRowContext(r.Context(), `select user_id::text,email,previous_email from user_email_verifications where token_hash=$1 and expires_at>now() for update`, digest(in.Token)).Scan(&id, &email, &previous)
	if err != nil {
		invalidToken(w)
		return
	}
	result, err := tx.ExecContext(r.Context(), `update users set email=$2,email_verified_at=now(),updated_at=now() where id=$1 and coalesce(email,'')=$3 and status='active'`, id, email, previous)
	if err != nil {
		problem(w, 409, "email_in_use", "This email can no longer be bound. Request a new verification from your account settings.")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		invalidToken(w)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `delete from password_reset_tokens where user_id=$1`, id); err == nil {
		_, err = tx.ExecContext(r.Context(), `delete from user_email_verifications where user_id=$1`, id)
	}
	if err != nil {
		internal(w)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w)
		return
	}
	respond(w, 200, map[string]string{"message": "Your bound email has been verified. You can now use it to sign in and recover your password."})
}
