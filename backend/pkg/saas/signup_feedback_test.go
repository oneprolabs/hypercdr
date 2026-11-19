package saas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/pkg/platform"
)

func TestSignupRegisteredEmailFeedback(t *testing.T) {
	s, mail := newTestService(t)
	user := signup(t, s, mail, "existing@example.test", "Existing")
	if _, err := s.db.Exec(`update users set email='bound@example.test' where id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{" EXISTING@EXAMPLE.TEST ", "BOUND@example.test"} {
		response := call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": email}, platform.Principal{})
		if response.Code != 409 || !strings.Contains(response.Body.String(), "email_already_registered") {
			t.Fatalf("registered address: %d %s", response.Code, response.Body)
		}
	}
	if len(mail.messages) != 1 {
		t.Fatal("registration sent mail to an existing account")
	}
}

func TestSignupDeliveryFeedbackAndLimits(t *testing.T) {
	s, mail := newTestService(t)
	request := func() *httptest.ResponseRecorder {
		return call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "delivery@example.test"}, platform.Principal{})
	}
	mail.fail = true
	for attempt := 0; attempt < 3; attempt++ {
		response := request()
		if response.Code != 503 || !strings.Contains(response.Body.String(), "email_delivery_failed") {
			t.Fatalf("delivery failure: %d %s", response.Code, response.Body)
		}
	}
	var tokens int
	if err := s.db.QueryRow(`select count(*) from saas_onboarding_tokens`).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatalf("failed sends left tokens: %d %v", tokens, err)
	}
	mail.fail = false
	response := request()
	if response.Code != 202 || !strings.Contains(response.Body.String(), "Verification email sent to delivery@example.test") || !strings.Contains(response.Body.String(), "30 minutes") {
		t.Fatalf("success feedback: %d %s", response.Code, response.Body)
	}
	response = request()
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatalf("resend limit: %d %s", response.Code, response.Body)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := s.db.Exec(`update saas_auth_rate_limits set expires_at=now()-interval '1 second' where bucket=$1`, digest("signup-resend:delivery@example.test")); err != nil {
			t.Fatal(err)
		}
		if response = request(); response.Code != 202 {
			t.Fatalf("successful-send quota consumed by delivery failures: %d %s", response.Code, response.Body)
		}
	}
	if _, err := s.db.Exec(`update saas_auth_rate_limits set expires_at=now()-interval '1 second' where bucket=$1`, digest("signup-resend:delivery@example.test")); err != nil {
		t.Fatal(err)
	}
	response = request()
	if response.Code != 429 || len(mail.messages) != 3 {
		t.Fatalf("hourly send limit: %d, mails=%d", response.Code, len(mail.messages))
	}
}

func TestSignupDeliveryFailureCannotBypassAttemptLimit(t *testing.T) {
	s, mail := newTestService(t)
	mail.fail = true
	for attempt := 0; attempt < 11; attempt++ {
		response := call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "fail@example.test"}, platform.Principal{})
		want := 503
		if attempt == 10 {
			want = 429
		}
		if response.Code != want {
			t.Fatalf("attempt %d: got %d want %d", attempt, response.Code, want)
		}
	}
}

func TestSignupConcurrentRequestsSendOnce(t *testing.T) {
	s, mail := newTestService(t)
	var group sync.WaitGroup
	results := make(chan int, 2)
	for attempt := 0; attempt < 2; attempt++ {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "once@example.test"}, platform.Principal{}).Code
		}()
	}
	group.Wait()
	close(results)
	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[202] != 1 || counts[429] != 1 || len(mail.messages) != 1 {
		t.Fatalf("concurrent send: statuses=%v mails=%d", counts, len(mail.messages))
	}
}

func TestSignupDistinctLinksDoNotCreateOrphanOrganizations(t *testing.T) {
	s, mail := newTestService(t)
	request := func() string {
		expect(t, call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "twice@example.test"}, platform.Principal{}), 202)
		return mail.token(t)
	}
	first := request()
	if _, err := s.db.Exec(`update saas_auth_rate_limits set expires_at=now()-interval '1 second' where bucket=$1`, digest("signup-resend:twice@example.test")); err != nil {
		t.Fatal(err)
	}
	second := request()
	var before int
	if err := s.db.QueryRow(`select count(*) from resource_scopes`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for index, token := range []string{first, second} {
		response := call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": token, "organizationName": "No orphan", "password": "Password123"}, platform.Principal{})
		want := 200
		if index == 1 {
			want = 409
		}
		if response.Code != want {
			t.Fatalf("link %d: %d %s", index, response.Code, response.Body)
		}
	}
	var after int
	if err := s.db.QueryRow(`select count(*) from resource_scopes`).Scan(&after); err != nil || after != before+1 {
		t.Fatalf("orphan organization: before=%d after=%d err=%v", before, after, err)
	}
}

func TestSignupConcurrentCompletionAndExpiredLink(t *testing.T) {
	s, mail := newTestService(t)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "concurrent@example.test"}, platform.Principal{}), 202)
	token := mail.token(t)
	var before int
	if err := s.db.QueryRow(`select count(*) from resource_scopes`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan int, 2)
	for attempt := 0; attempt < 2; attempt++ {
		group.Add(1)
		go func() {
			defer group.Done()
			response := call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": token, "organizationName": "Concurrent", "password": "Password123"}, platform.Principal{})
			results <- response.Code
		}()
	}
	group.Wait()
	close(results)
	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[200] != 1 || counts[400] != 1 {
		t.Fatalf("duplicate completion: %v", counts)
	}
	var after int
	if err := s.db.QueryRow(`select count(*) from resource_scopes`).Scan(&after); err != nil || after != before+1 {
		t.Fatalf("duplicate or orphan organization: before=%d after=%d err=%v", before, after, err)
	}
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "expired@example.test"}, platform.Principal{}), 202)
	if _, err := s.db.Exec(`update saas_onboarding_tokens set expires_at=now()-interval '1 second' where email='expired@example.test'`); err != nil {
		t.Fatal(err)
	}
	response := call(t, s, "POST", "/api/v1/saas/auth/signup/verification", map[string]string{"token": mail.token(t)}, platform.Principal{})
	if response.Code != 400 || !strings.Contains(response.Body.String(), "link_invalid") {
		t.Fatalf("expired link: %d %s", response.Code, response.Body)
	}
}

type signupFailingSession struct {
	IdentityProvider
	verify bool
}

func (provider signupFailingSession) CreateSession(ctx context.Context, user string, ttl time.Duration) (platform.IdentitySession, error) {
	if !provider.verify {
		return platform.IdentitySession{}, errors.New("test session failure")
	}
	return provider.IdentityProvider.CreateSession(ctx, user, ttl)
}

func (provider signupFailingSession) AuthenticateSession(context.Context, string) (platform.Identity, bool, error) {
	return platform.Identity{}, false, errors.New("test verification failure")
}

func TestSignupAccountCreatedDespiteSessionFailure(t *testing.T) {
	for _, verify := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "verify"}[verify], func(t *testing.T) {
			s, mail := newTestService(t)
			expect(t, call(t, s, "POST", "/api/v1/saas/auth/signup/request", map[string]string{"email": "session@example.test"}, platform.Principal{}), 202)
			original := s.identity
			s.identity = signupFailingSession{IdentityProvider: original, verify: verify}
			response := call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"token": mail.token(t), "organizationName": "Created", "password": "Password123"}, platform.Principal{})
			var failure map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || response.Code != 503 || failure["error"] != "account_created_login_failed" {
				t.Fatalf("session failure: %d %s", response.Code, response.Body)
			}
			if _, ok, err := original.Authenticate(context.Background(), "session@example.test", "Password123"); err != nil || !ok {
				t.Fatalf("created account cannot sign in: %v", err)
			}
		})
	}
}
