package saas

import (
	"context"
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/platform"
)

func TestBoundEmailAdminAndRegisteredAccountsShareOneModel(t *testing.T) {
	s, m := newTestService(t)
	p := systemPrincipal(t, s)
	if p.Username != "admin" || p.Email != "" {
		t.Fatalf("unexpected fresh admin: %#v", p)
	}
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": "admin-owner@example.test", "password": "wrong"}, p), 400)
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": "admin-owner@example.test", "password": store.DefaultAdminPassword}, p), 202)
	token := m.token(t)
	if _, ok, e := s.identity.Authenticate(context.Background(), "admin-owner@example.test", store.DefaultAdminPassword); e != nil || ok {
		t.Fatalf("unverified email accepted: %v", e)
	}
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/email/verify", map[string]string{"token": token}, platform.Principal{}), 200)
	for _, login := range []string{"admin", "ADMIN-OWNER@example.test"} {
		u, ok, e := s.identity.Authenticate(context.Background(), login, store.DefaultAdminPassword)
		if e != nil || !ok || u.ID != p.ID || u.Username != "admin" || u.Email != "admin-owner@example.test" || u.EmailVerifiedAt == nil {
			t.Fatalf("login %s failed: %#v %v", login, u, e)
		}
	}
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/email/verify", map[string]string{"token": token}, platform.Principal{}), 400)
	// New registrations cannot take another user's bound email.
	var conflict bool
	e := s.db.QueryRow(`select exists(select 1 from users where lower(username)='admin-owner@example.test' or lower(email)='admin-owner@example.test')`).Scan(&conflict)
	if e != nil || !conflict {
		t.Fatal("bound address was not reserved")
	}
}

func TestBoundEmailChangePreservesLoginAndInvalidatesOldRecovery(t *testing.T) {
	s, m := newTestService(t)
	u := signup(t, s, m, "original@example.test", "Owner")
	u.Username = "owner-account"
	if _, e := s.db.Exec(`update users set username=$1 where id=$2`, u.Username, u.ID); e != nil {
		t.Fatal(e)
	}
	p := principal(u)
	v := signup(t, s, m, "other@example.test", "Other")
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": v.Email, "password": "Password123"}, p), 409)
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": "updated@example.test", "password": "Password123"}, p), 202)
	old := m.token(t)
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": "final@example.test", "password": "Password123"}, p), 202)
	token := m.token(t)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/email/verify", map[string]string{"token": old}, platform.Principal{}), 400)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/email/verify", map[string]string{"token": token}, platform.Principal{}), 200)
	for _, login := range []string{u.Username, "final@example.test"} {
		got, ok, e := s.identity.Authenticate(context.Background(), login, "Password123")
		if e != nil || !ok || got.ID != u.ID || got.TenantID != u.TenantID {
			t.Fatalf("login changed organization or failed: %v", e)
		}
	}
	var count int
	if e := s.db.QueryRow(`select count(*) from users where email=$1`, u.Email).Scan(&count); e != nil || count != 0 {
		t.Fatal("old address remains a recovery destination")
	}
	// Profile edits cannot bypass verified binding.
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": "final@example.test", "password": "Password123"}, principal(v)), 409)
}

func TestBoundEmailExpiryConflictAndDeliveryFailure(t *testing.T) {
	s, m := newTestService(t)
	u := signup(t, s, m, "bind-owner@example.test", "Owner")
	u.Username = "bind-owner-account"
	if _, e := s.db.Exec(`update users set username=$1 where id=$2`, u.Username, u.ID); e != nil {
		t.Fatal(e)
	}
	p := principal(u)
	request := func(email string) string {
		expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": email, "password": "Password123"}, p), 202)
		return m.token(t)
	}
	expired := request("expired@example.test")
	if _, e := s.db.Exec(`update user_email_verifications set expires_at=now()-interval '1 minute'`); e != nil {
		t.Fatal(e)
	}
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/email/verify", map[string]string{"token": expired}, platform.Principal{}), 400)
	conflict := request("taken-later@example.test")
	signup(t, s, m, "taken-later@example.test", "Other")
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/email/verify", map[string]string{"token": conflict}, platform.Principal{}), 409)
	m.fail = true
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": "delivery@example.test", "password": "Password123"}, p), 503)
	var email string
	if e := s.db.QueryRow(`select email from users where id=$1`, u.ID).Scan(&email); e != nil || email != u.Email {
		t.Fatal("failed verification changed email")
	}
}

func TestEmailLoginCannotChangeBoundEmail(t *testing.T) {
	s, m := newTestService(t)
	u := signup(t, s, m, "fixed@example.test", "Fixed email")
	expect(t, call(t, s, "POST", "/api/v1/saas/account/email", map[string]string{"email": "new@example.test", "password": "Password123"}, principal(u)), 409)
	var email string
	if e := s.db.QueryRow(`select email from users where id=$1`, u.ID).Scan(&email); e != nil || email != u.Username {
		t.Fatal("fixed account email changed")
	}
	// Previously issued links must not bypass the new policy.
	_, e := s.db.Exec(`insert into user_email_verifications(token_hash,user_id,email,previous_email,expires_at) values($1,$2,'new@example.test',$3,now()+interval '10 minutes')`, digest("old-link"), u.ID, u.Email)
	if e != nil {
		t.Fatal(e)
	}
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/email/verify", map[string]string{"token": "old-link"}, platform.Principal{}), 400)
}
