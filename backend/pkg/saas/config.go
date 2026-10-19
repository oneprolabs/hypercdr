// Package saas provides single-organization registration and account access.
package saas

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/pkg/platform"
)

type Mailer interface {
	Send(context.Context, string, string, string) error
}
type IdentityProvider interface {
	Authenticate(context.Context, string, string) (platform.Identity, bool, error)
	CreateSession(context.Context, string, time.Duration) (platform.IdentitySession, error)
	AuthenticateSession(context.Context, string) (platform.Identity, bool, error)
	DeleteSession(context.Context, string) error
}
type Config struct {
	SaaS, Signup                               bool
	Origin, GoogleClientID, GoogleClientSecret string
	Mailer                                     Mailer
}
type Service struct {
	db              *sql.DB
	cfg             Config
	identity        IdentityProvider
	google          *googleProvider
	emailConfigured func() bool
}

func ConfigFromEnvironment() (Config, error) {
	mode := strings.TrimSpace(os.Getenv("HCDR_DEPLOYMENT_MODE"))
	if mode == "" {
		mode = "saas"
	}
	if mode != "" && mode != "private" && mode != "saas" {
		return Config{}, errors.New("invalid deployment mode")
	}
	enabled := os.Getenv("HCDR_SAAS_SIGNUP_ENABLED")
	if enabled == "" {
		enabled = "true"
	}
	if enabled != "" && enabled != "true" && enabled != "false" {
		return Config{}, errors.New("HCDR_SAAS_SIGNUP_ENABLED must be true or false")
	}
	c := Config{SaaS: mode == "saas", Signup: mode == "saas" && enabled == "true", Origin: strings.TrimRight(os.Getenv("HCDR_PUBLIC_BASE_URL"), "/"), GoogleClientID: os.Getenv("HCDR_GOOGLE_CLIENT_ID"), GoogleClientSecret: os.Getenv("HCDR_GOOGLE_CLIENT_SECRET")}
	if c.Origin == "" {
		c.Origin = "https://localhost:12443"
	}
	if c.Signup || c.GoogleClientID != "" || c.GoogleClientSecret != "" {
		u, e := url.Parse(c.Origin)
		if e != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()))) || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, errors.New("SaaS identity requires an HTTPS HCDR_PUBLIC_BASE_URL origin")
		}
	}
	if (c.GoogleClientID == "") != (c.GoogleClientSecret == "") {
		return c, errors.New("configure both Google client ID and secret")
	}

	return c, nil
}
func New(db *sql.DB, cfg Config, identity IdentityProvider) *Service {
	s := &Service{db: db, cfg: cfg, identity: identity}
	if cfg.GoogleClientID != "" {
		s.google = newGoogleProvider(cfg)
	}
	return s
}
func (s *Service) Routes() []platform.Route {
	const auth = "/api/v1/saas/auth"
	routes := []platform.Route{}
	public := func(pattern string, h func(*Service, http.ResponseWriter, *http.Request)) {
		routes = append(routes, platform.Route{Pattern: pattern, Public: true, Handler: func(w http.ResponseWriter, r *http.Request, _ platform.Principal) {
			s.withCurrentOrigin(w, r, func(current *Service) {
				current.public(w, r, func(w http.ResponseWriter, r *http.Request) { h(current, w, r) })
			})
		}})
	}
	private := func(pattern string, h func(*Service, http.ResponseWriter, *http.Request, platform.Principal)) {
		routes = append(routes, platform.Route{Pattern: pattern, Handler: func(w http.ResponseWriter, r *http.Request, p platform.Principal) {
			w.Header().Set("Cache-Control", "no-store")
			r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
			s.withCurrentOrigin(w, r, func(current *Service) {
				if r.Method != "GET" && r.Header.Get("Origin") != current.cfg.Origin {
					originProblem(w, current.cfg.Origin)
					return
				}
				h(current, w, r, p)
			})
		}})
	}
	public("GET "+auth+"/config", (*Service).configuration)
	public("POST "+auth+"/signup/request", (*Service).requestSignup)
	public("POST "+auth+"/signup/verification", (*Service).verificationInfo)
	public("POST "+auth+"/complete", (*Service).complete)
	public("POST "+auth+"/invitation", (*Service).invitationInfo)
	public("POST "+auth+"/google/start", (*Service).googleStart)
	public("GET "+auth+"/google/callback", (*Service).googleCallback)
	public("POST "+auth+"/google/session", (*Service).googleSession)
	public("POST "+auth+"/google/profile", (*Service).googleProfile)
	private("GET /api/v1/saas/organization", (*Service).organization)
	private("PATCH /api/v1/saas/organization", (*Service).organization)
	private("GET /api/v1/saas/members", (*Service).members)
	private("PATCH /api/v1/saas/members/{id}", (*Service).updateMember)
	private("GET /api/v1/saas/invitations", (*Service).listInvitations)
	private("POST /api/v1/saas/invitations", (*Service).invite)
	private("POST /api/v1/saas/invitations/{id}/revoke", (*Service).revokeInvitation)
	private("POST /api/v1/saas/google/link", (*Service).linkGoogle)
	private("GET /api/v1/saas/google/identity", (*Service).googleIdentity)
	return routes
}
func (s *Service) public(w http.ResponseWriter, r *http.Request, h http.HandlerFunc) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method == "POST" {
		if r.Header.Get("Origin") != s.cfg.Origin {
			originProblem(w, s.cfg.Origin)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			problem(w, 415, "json_required", "Use a JSON request.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !s.allow(r.Context(), "ip:"+host, 100, time.Hour) {
			problem(w, 429, "too_many_attempts", "Too many attempts. Please try again later.")
			return
		}
	}
	h(w, r)
}
func (s *Service) allow(ctx context.Context, key string, max int, ttl time.Duration) bool {
	var n int
	err := s.db.QueryRowContext(ctx, `insert into saas_auth_rate_limits(bucket,count,expires_at) values($1,1,now()+$2::interval) on conflict(bucket) do update set count=case when saas_auth_rate_limits.expires_at<now() then 1 else saas_auth_rate_limits.count+1 end,expires_at=case when saas_auth_rate_limits.expires_at<now() then excluded.expires_at else saas_auth_rate_limits.expires_at end returning count`, digest(key), fmt.Sprintf("%d seconds", int(ttl.Seconds()))).Scan(&n)
	return err == nil && n <= max
}
func (s *Service) configuration(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]any{"publicUrl": s.cfg.Origin, "saas": s.cfg.SaaS, "signupEnabled": s.cfg.Signup, "emailConfigured": s.mailConfigured(), "googleEnabled": s.google != nil})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]string{"error": code, "message": message})
}
func decode(r *http.Request, v any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return false
	}
	return d.Decode(new(any)) == io.EOF
}
func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func randomToken() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func emailAddress(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	a, e := mail.ParseAddress(v)
	return v, e == nil && a.Address == v && len(v) <= 254
}
func setCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/api/v1/saas/auth/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func cookie(r *http.Request, name string) string {
	c, e := r.Cookie(name)
	if e != nil {
		return ""
	}
	return c.Value
}

// BindSystemEmail uses the same live SMTP settings as password recovery and test email.
func (s *Service) BindSystemEmail(configured func() bool, send func(context.Context, string, string, string) error) {
	s.emailConfigured = configured
	s.cfg.Mailer = mailFunc(send)
}

type mailFunc func(context.Context, string, string, string) error

func (f mailFunc) Send(ctx context.Context, to, subject, body string) error {
	return f(ctx, to, subject, body)
}
func (s *Service) mailConfigured() bool {
	if s.emailConfigured != nil {
		return s.emailConfigured()
	}
	return s.cfg.Mailer != nil
}
