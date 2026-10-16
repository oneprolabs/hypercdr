package saas

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
	"hypercdr-platform/platform/backend/pkg/platform"
)

// This provider signs real JWTs and serves JWKS and OAuth token responses. It is
// injected only in tests; production endpoints remain fixed to Google.
type signedProvider struct {
	key       *rsa.PrivateKey
	server    *httptest.Server
	claims    map[string]any
	challenge string
	exchanges int
}

func newSignedProvider(t *testing.T, s *Service) *signedProvider {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	p := &signedProvider{key: key}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/keys" {
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
			return
		}
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		p.exchanges++
		_ = r.ParseForm()
		hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("code") != "valid-code" || base64.RawURLEncoding.EncodeToString(hash[:]) != p.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.key, KeyID: "test-key"}}, nil)
		if e != nil {
			t.Error(e)
			return
		}
		body, _ := json.Marshal(p.claims)
		obj, e := signer.Sign(body)
		if e != nil {
			t.Error(e)
			return
		}
		jwt, _ := obj.CompactSerialize()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-access", "token_type": "Bearer", "expires_in": 300, "id_token": jwt})
	}))
	t.Cleanup(p.server.Close)
	client := p.server.Client()
	s.google = &googleProvider{OAuth: oauth2.Config{ClientID: "test-client", ClientSecret: "test-secret", RedirectURL: s.cfg.Origin + "/api/v1/saas/auth/google/callback", Scopes: []string{"openid", "email"}, Endpoint: oauth2.Endpoint{AuthURL: p.server.URL + "/authorize", TokenURL: p.server.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}, Verifier: oidc.NewVerifier(p.server.URL, oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), p.server.URL+"/keys"), &oidc.Config{ClientID: "test-client"}), Client: client}
	return p
}
func (p *signedProvider) start(t *testing.T, s *Service, invitation string) (url.Values, *http.Cookie) {
	t.Helper()
	w := call(t, s, "POST", "/api/v1/saas/auth/google/start", map[string]string{"invitation": invitation}, platform.Principal{})
	expect(t, w, 200)
	var data map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &data)
	u, e := url.Parse(data["url"])
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("nonce") == "" {
		t.Fatal("missing PKCE/state/nonce")
	}
	p.challenge = q.Get("code_challenge")
	p.claims = map[string]any{"iss": p.server.URL, "aud": "test-client", "sub": "google-subject", "email": "google@example.test", "email_verified": true, "nonce": q.Get("nonce"), "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
	return q, w.Result().Cookies()[0]
}
func callback(t *testing.T, s *Service, q url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, s, "GET", "/api/v1/saas/auth/google/callback?state="+url.QueryEscape(q.Get("state"))+"&code=valid-code", nil, platform.Principal{}, cookies...)
}
func redirectError(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	expect(t, w, 303)
	u, _ := url.Parse(w.Header().Get("Location"))
	if u.Query().Get("oauth_error") != code {
		t.Fatalf("redirect %s", u)
	}
}
func responseCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing cookie %s", name)
	return nil
}
func TestGoogleSignedOnboardingAndSingleUseSession(t *testing.T) {
	s, _ := newTestService(t)
	p := newSignedProvider(t, s)
	q, browser := p.start(t, s, "")
	w := callback(t, s, q, browser)
	expect(t, w, 303)
	if w.Header().Get("Location") != s.cfg.Origin+"/signup?provider=google" {
		t.Fatal(w.Header())
	}
	onboarding := responseCookie(t, w, "hcdr_onboarding")
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/google/profile", map[string]string{}, platform.Principal{}, browser, onboarding), 200)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"organizationName": "Google organization"}, platform.Principal{}, browser, onboarding), 200)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"organizationName": "Replay"}, platform.Principal{}, browser, onboarding), 400)
	redirectError(t, callback(t, s, q, browser), "authorization_expired")
	q, browser = p.start(t, s, "")
	w = callback(t, s, q, browser)
	expect(t, w, 303)
	ticket := responseCookie(t, w, "hcdr_google_session")
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/google/session", map[string]string{}, platform.Principal{}, ticket), 400)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/google/session", map[string]string{}, platform.Principal{}, ticket, browser), 200)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/google/session", map[string]string{}, platform.Principal{}, ticket, browser), 400)
}
func TestGoogleRejectsUntrustedClaimsAndMissingBrowser(t *testing.T) {
	s, _ := newTestService(t)
	p := newSignedProvider(t, s)
	for _, tc := range []struct {
		name, key string
		value     any
	}{{"issuer", "iss", "https://evil.test"}, {"audience", "aud", "other-client"}, {"nonce", "nonce", "wrong"}, {"expired", "exp", time.Now().Add(-time.Hour).Unix()}, {"unverified", "email_verified", false}, {"subject", "sub", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			q, b := p.start(t, s, "")
			p.claims[tc.key] = tc.value
			redirectError(t, callback(t, s, q, b), "google_verification_failed")
		})
	}
	q, b := p.start(t, s, "")
	before := p.exchanges
	redirectError(t, callback(t, s, q), "authorization_expired")
	if p.exchanges != before {
		t.Fatal("missing browser reached token endpoint")
	}
	// The rightful browser can still use its unconsumed authorization request.
	expect(t, callback(t, s, q, b), 303)
	q, b = p.start(t, s, "")
	p.challenge = "wrong-pkce"
	redirectError(t, callback(t, s, q, b), "google_verification_failed")
	q, b = p.start(t, s, "")
	s.db.Exec(`update saas_oauth_requests set expires_at=now()-interval '1 minute'`)
	redirectError(t, callback(t, s, q, b), "authorization_expired")
}
func TestGoogleDoesNotLinkByEmailOrBypassPrivateSignup(t *testing.T) {
	s, m := newTestService(t)
	signup(t, s, m, "google@example.test", "Password organization")
	p := newSignedProvider(t, s)
	q, b := p.start(t, s, "")
	redirectError(t, callback(t, s, q, b), "account_link_required")
	q, b = p.start(t, s, "")
	p.claims["email"] = "new@example.test"
	s.cfg.Signup = false
	redirectError(t, callback(t, s, q, b), "signup_disabled")
}
func TestGoogleInvitationRequiresMatchingVerifiedEmail(t *testing.T) {
	s, m := newTestService(t)
	owner := signup(t, s, m, "owner@example.test", "Inviting organization")
	p := newSignedProvider(t, s)
	expect(t, call(t, s, "POST", "/api/v1/saas/invitations", map[string]string{"email": "invited@example.test", "roleId": "operator"}, principal(owner)), 201)
	invite := m.token(t)
	s.cfg.Signup = false // Private deployments still allow explicitly invited Google identities.
	q, b := p.start(t, s, invite)
	redirectError(t, callback(t, s, q, b), "invitation_identity_mismatch")
	q, b = p.start(t, s, invite)
	p.claims["email"] = "invited@example.test"
	w := callback(t, s, q, b)
	expect(t, w, 303)
	onboarding := responseCookie(t, w, "hcdr_onboarding")
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{}, platform.Principal{}, b, onboarding), 400)
	expect(t, call(t, s, "POST", "/api/v1/saas/auth/complete", map[string]string{"invitation": invite}, platform.Principal{}, b, onboarding), 200)
	var tenant string
	if e := s.db.QueryRow(`select tenant_id::text from users where email='invited@example.test'`).Scan(&tenant); e != nil || tenant != owner.TenantID {
		t.Fatal("wrong invitation organization", e)
	}
}

func TestGoogleRejectsInvalidSignatureAndCancelledAuthorization(t *testing.T) {
	s, _ := newTestService(t)
	p := newSignedProvider(t, s)
	q, b := p.start(t, s, "")
	wrong, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	p.key = wrong
	redirectError(t, callback(t, s, q, b), "google_verification_failed")
	q, b = p.start(t, s, "")
	before := p.exchanges
	w := call(t, s, "GET", "/api/v1/saas/auth/google/callback?state="+q.Get("state")+"&error=access_denied", nil, platform.Principal{}, b)
	redirectError(t, w, "authorization_cancelled")
	if p.exchanges != before {
		t.Fatal("cancelled authorization exchanged code")
	}
	redirectError(t, callback(t, s, q, b), "authorization_expired")
}
func TestExplicitGoogleLinkRequiresPasswordAndRejectsDisabledUser(t *testing.T) {
	s, m := newTestService(t)
	owner := signup(t, s, m, "password@example.test", "Linked organization")
	p := newSignedProvider(t, s)
	expect(t, call(t, s, "POST", "/api/v1/saas/google/link", map[string]string{"password": "wrong"}, principal(owner)), 403)
	w := call(t, s, "POST", "/api/v1/saas/google/link", map[string]string{"password": "Password123"}, principal(owner))
	expect(t, w, 200)
	var data map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &data)
	u, _ := url.Parse(data["url"])
	q := u.Query()
	p.challenge = q.Get("code_challenge")
	p.claims = map[string]any{"iss": p.server.URL, "aud": "test-client", "sub": "explicit-google-subject", "email": "different-verified@example.test", "email_verified": true, "nonce": q.Get("nonce"), "exp": time.Now().Add(time.Hour).Unix()}
	b := responseCookie(t, w, "hcdr_google_browser")
	expect(t, callback(t, s, q, b), 303)
	var linked string
	if e := s.db.QueryRow(`select user_id::text from saas_external_identities where subject='explicit-google-subject'`).Scan(&linked); e != nil || linked != owner.ID {
		t.Fatal("explicit linking failed", e)
	}
	_, e := s.db.Exec(`update users set status='disabled' where id=$1`, owner.ID)
	if e != nil {
		t.Fatal(e)
	}
	q, b = p.start(t, s, "")
	p.claims["sub"] = "explicit-google-subject"
	p.claims["email"] = "changed-email@example.test"
	redirectError(t, callback(t, s, q, b), "account_unavailable")
}

func expect(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("got %d want %d: %s", w.Code, code, w.Body.String())
	}
}
