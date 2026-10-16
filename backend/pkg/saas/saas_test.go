package saas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hypercdr-platform/platform/backend/internal/testdb"
	"hypercdr-platform/platform/backend/pkg/platform"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type captureMail struct {
	mu       sync.Mutex
	messages []string
	fail     bool
}

func (m *captureMail) Send(_ context.Context, _, _, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return fmt.Errorf("mail unavailable")
	}
	m.messages = append(m.messages, body)
	return nil
}
func (m *captureMail) token(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		t.Fatal("no verification email")
	}
	v := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(m.messages[len(m.messages)-1])
	if len(v) != 2 {
		t.Fatal("verification link missing")
	}
	return v[1]
}
func newTestService(t *testing.T) (*Service, *captureMail) {
	t.Helper()
	m := &captureMail{}
	s, close, e := Open(context.Background(), testdb.New(t), Config{SaaS: true, Signup: true, Origin: "https://platform.test", Mailer: m})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(close)
	return s, m
}
func call(t *testing.T, s *Service, method, path string, body any, p platform.Principal, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		if e := json.NewEncoder(&b).Encode(body); e != nil {
			t.Fatal(e)
		}
	}
	r := httptest.NewRequest(method, path, &b)
	r.Header.Set("Origin", s.cfg.Origin)
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	mux := http.NewServeMux()
	for _, route := range s.Routes() {
		route := route
		mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, r *http.Request) { route.Handler(w, r, p) })
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func signup(t *testing.T, s *Service, m *captureMail, email, org string) platform.Identity {
	t.Helper()
	w := call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": email}, platform.Principal{})
	if w.Code != 202 {
		t.Fatalf("request signup: %d %s", w.Code, w.Body.String())
	}
	w = call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": m.token(t), "organizationName": org, "password": "Password123", "displayName": "Test"}, platform.Principal{})
	if w.Code != 200 {
		t.Fatalf("complete signup: %d %s", w.Code, w.Body.String())
	}
	var out struct{ User platform.Identity }
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out.User
}
func principal(u platform.Identity) platform.Principal {
	return platform.Principal{ID: u.ID, TenantID: u.TenantID, Email: u.Email, Role: u.Role, SystemAdmin: u.SystemAdmin}
}
func TestSignupOrganizationAndInvitationIsolation(t *testing.T) {
	s, m := newTestService(t)
	a := signup(t, s, m, "a@example.test", "Organization A")
	b := signup(t, s, m, "b@example.test", "Organization B")
	if a.TenantID == b.TenantID || a.Role != "admin" || a.MustChangePassword {
		t.Fatal("invalid signup identity")
	}
	w := call(t, s, "GET", "/api/v1/saas/organization", nil, principal(a))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Organization A") {
		t.Fatalf("organization details: %d %s", w.Code, w.Body.String())
	}
	w = call(t, s, "POST", "/api/v1/saas/invitations", map[string]string{"email": "member@example.test", "roleId": "operator"}, principal(a))
	if w.Code != 201 {
		t.Fatalf("invite: %d %s", w.Code, w.Body.String())
	}
	token := m.token(t)
	w = call(t, s, "GET", "/api/v1/saas/invitations", nil, principal(b))
	if strings.Contains(w.Body.String(), "member@example.test") {
		t.Fatal("invitation leaked to other organization")
	}
	w = call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": token, "password": "Password123"}, platform.Principal{})
	if w.Code != 200 {
		t.Fatalf("accept invitation: %d %s", w.Code, w.Body.String())
	}
	var out struct{ User platform.Identity }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.User.TenantID != a.TenantID || out.User.Role != "operator" {
		t.Fatal("invitation scope/role changed")
	}
	w = call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": token, "password": "Password123"}, platform.Principal{})
	if w.Code != 400 {
		t.Fatal("invitation replay accepted")
	}
	w = call(t, s, "POST", "/api/v1/saas/invitations", map[string]string{"email": a.Email, "roleId": "operator"}, principal(b))
	if w.Code != 409 {
		t.Fatal("existing account allowed to join another organization")
	}
	w = call(t, s, "PATCH", "/api/v1/saas/members/"+a.ID, map[string]string{"role": "operator", "status": "active"}, principal(b))
	if w.Code != 404 {
		t.Fatal("cross-organization member edit accepted")
	}
	w = call(t, s, "PATCH", "/api/v1/saas/members/"+a.ID, map[string]string{"role": "operator", "status": "active"}, principal(a))
	if w.Code != 409 {
		t.Fatalf("last administrator not protected: %d", w.Code)
	}
	w = call(t, s, "GET", "/api/v1/saas/members", nil, principal(b))
	if strings.Contains(w.Body.String(), a.Email) || strings.Contains(w.Body.String(), out.User.Email) {
		t.Fatal("members leaked")
	}
	w = call(t, s, "POST", "/api/v1/saas/invitations", map[string]string{"email": "another@example.test", "roleId": "admin"}, principal(out.User))
	if w.Code != 403 {
		t.Fatal("member can invite administrators")
	}
}
func TestEmailFailureTokensAndSignupReplay(t *testing.T) {
	s, m := newTestService(t)
	m.fail = true
	w := call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "a@example.test"}, platform.Principal{})
	if w.Code != 503 {
		t.Fatalf("mail failure: %d", w.Code)
	}
	var count int
	if e := s.db.QueryRow(`select count(*) from saas_onboarding_tokens`).Scan(&count); e != nil || count != 0 {
		t.Fatalf("failed mail left tokens: %d %v", count, e)
	}
	m.fail = false
	signup(t, s, m, "a@example.test", "A")
	w = call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": m.token(t), "password": "Password123", "organizationName": "B"}, platform.Principal{})
	if w.Code != 400 {
		t.Fatal("signup replay accepted")
	}
}
func TestDisabledSessionsAndLastAdminConcurrency(t *testing.T) {
	s, m := newTestService(t)
	a := signup(t, s, m, "a@example.test", "A")
	w := call(t, s, "POST", "/api/v1/saas/invitations", map[string]string{"email": "b@example.test", "roleId": "admin"}, principal(a))
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	w = call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": m.token(t), "password": "Password123"}, platform.Principal{})
	var out struct{ User platform.Identity }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	session, e := s.identity.CreateSession(context.Background(), out.User.ID, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, u := range []platform.Identity{a, out.User} {
		wg.Add(1)
		go func(u platform.Identity) {
			defer wg.Done()
			v := call(t, s, "PATCH", "/api/v1/saas/members/"+u.ID, map[string]string{"role": "operator", "status": "disabled"}, principal(u))
			codes <- v.Code
		}(u)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent admin removals: success=%d conflict=%d", success, conflict)
	}
	var status string
	_ = s.db.QueryRow(`select status from users where id=$1`, out.User.ID).Scan(&status)
	if status == "disabled" {
		_, ok, e := s.identity.AuthenticateSession(context.Background(), session.Token)
		if e != nil || ok {
			t.Fatal("disabled member session still active")
		}
	}
}
