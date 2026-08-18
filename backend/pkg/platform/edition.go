package platform

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Edition identifies the product assembled around the shared HyperCDR core.
type Edition string

const (
	EditionCommunity  Edition = "community"
	EditionEnterprise Edition = "enterprise"
)

// Capability describes one edition-level product capability. Authorization is
// still enforced by the underlying API; capabilities are product discovery,
// not a security boundary.
type Capability struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

// LicenseStatus is deliberately provider-neutral. The enterprise repository
// can later replace its development provider with signed-license validation.
type LicenseStatus struct {
	Mode   string `json:"mode"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ProductInfo is returned by the public product-info endpoint.
type ProductInfo struct {
	Product      string                `json:"product"`
	Edition      Edition               `json:"edition"`
	Capabilities map[string]Capability `json:"capabilities"`
	License      LicenseStatus         `json:"license"`
}

// Principal is the authenticated identity presented to an edition authorizer.
// It intentionally contains no storage or HTTP server implementation types.
type Principal struct {
	ID          string
	TenantID    string
	Email       string
	Role        string
	SystemAdmin bool
}

// Identity is the edition-neutral authenticated account contract. Enterprise
// supplies its own implementation so multi-user credentials and sessions do
// not depend on Community-owned identity tables.
type Identity struct {
	ID                 string `json:"id"`
	TenantID           string `json:"tenantId"`
	TenantName         string `json:"tenantName"`
	Email              string `json:"email"`
	DisplayName        string `json:"displayName,omitempty"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	AuthProvider       string `json:"authProvider"`
	TimeZone           string `json:"timeZone,omitempty"`
	SystemAdmin        bool   `json:"systemAdmin,omitempty"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

type IdentitySession struct {
	Token     string
	ExpiresAt time.Time
}

type IdentityProfileUpdate struct {
	ID, Email, DisplayName, TimeZone string
}

// IdentityProvider owns interactive login state for an assembled edition.
// ResetPassword returns false for an invalid or expired token.
type IdentityProvider interface {
	Authenticate(context.Context, string, string) (Identity, bool, error)
	CreateSession(context.Context, string, time.Duration) (IdentitySession, error)
	AuthenticateSession(context.Context, string) (Identity, bool, error)
	DeleteSession(context.Context, string) error
	UpdateProfile(context.Context, IdentityProfileUpdate) (Identity, bool, error)
	SetPassword(context.Context, string, string, bool) (Identity, bool, error)
	CreatePasswordResetToken(context.Context, string, time.Duration) (string, bool, error)
	ResetPassword(context.Context, string, string) (bool, error)
}

type AuditEvent struct {
	TenantID, ActorID, Actor, Action, ResourceType, ResourceID, ResourceName, Result, Message string
	HTTPStatus                                                                                int
}

type AuditSink interface {
	RecordAudit(context.Context, AuditEvent) error
}

type DiagnosticEvent struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenantId,omitempty"`
	Scope       string         `json:"scope"`
	Level       string         `json:"level"`
	Component   string         `json:"component"`
	Operation   string         `json:"operation,omitempty"`
	Message     string         `json:"message"`
	ClusterID   string         `json:"clusterId,omitempty"`
	TaskID      string         `json:"taskId,omitempty"`
	CommandID   string         `json:"commandId,omitempty"`
	RequestID   string         `json:"requestId,omitempty"`
	ErrorCode   string         `json:"errorCode,omitempty"`
	Status      string         `json:"status,omitempty"`
	Fingerprint string         `json:"-"`
	DurationMS  int64          `json:"durationMs,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	EventAt     time.Time      `json:"eventAt"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type DiagnosticSink interface {
	RecordDiagnostic(context.Context, DiagnosticEvent) (DiagnosticEvent, error)
}

// AuthorizationRequest describes an authenticated API request after Community
// authentication and baseline authorization have succeeded.
type AuthorizationRequest struct {
	Method    string
	Path      string
	Principal Principal
}

// AuthorizationDecision can further restrict a request. Edition authorizers
// cannot grant access rejected by Community authentication or authorization.
type AuthorizationDecision struct {
	Allowed bool
	Code    string
	Message string
}

// Migration is an edition-owned, forward-only database change. Community
// migrations always complete before these migrations are applied.
type Migration struct {
	Version string
	SQL     string
}

// Route is an edition-owned API endpoint mounted into the authenticated
// Community HTTP pipeline. The principal has already passed Community session,
// password-change, administrator, and system-administrator checks applicable
// to the request path. Edition handlers must still enforce their own RBAC.
type Route struct {
	Pattern string
	Handler func(http.ResponseWriter, *http.Request, Principal)
}

// Authorizer is the stable edition boundary for Enterprise governance policy.
type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) AuthorizationDecision
}

// Options controls edition assembly without exposing internal server types.
type Options struct {
	Edition Edition
	// AgentNamespace pins an edition to its isolated in-cluster control-plane
	// namespace. When set, it is authoritative over legacy persisted settings.
	AgentNamespace         string
	Capabilities           map[string]Capability
	License                LicenseStatus
	Authorizer             Authorizer
	IdentityProvider       IdentityProvider
	AuditSink              AuditSink
	DiagnosticSink         DiagnosticSink
	DiagnosticLogRetention time.Duration
	Migrations             []Migration
	Routes                 []Route
}

func CommunityOptions() Options {
	return Options{
		Edition: EditionCommunity,
		Capabilities: map[string]Capability{
			"coreDR":           {ID: "coreDR", Enabled: true, Source: "community"},
			"basicDiagnostics": {ID: "basicDiagnostics", Enabled: true, Source: "community"},
			"basicAudit":       {ID: "basicAudit", Enabled: true, Source: "community"},
			"basicIdentity":    {ID: "basicIdentity", Enabled: true, Source: "community"},
			"advancedIdentity": {ID: "advancedIdentity", Enabled: false, Source: "enterprise"},
			"advancedTenancy":  {ID: "advancedTenancy", Enabled: false, Source: "enterprise"},
			"advancedAudit":    {ID: "advancedAudit", Enabled: false, Source: "enterprise"},
			"centralizedLogs":  {ID: "centralizedLogs", Enabled: false, Source: "enterprise"},
		},
		License:                LicenseStatus{Mode: "open-source", Status: "not-required", Detail: "Apache License 2.0"},
		Authorizer:             communityAuthorizer{},
		DiagnosticLogRetention: 30 * 24 * time.Hour,
	}
}

// communityAuthorizer keeps the open-source edition on its fixed default
// tenant while retaining local administrator and operator user management.
// Tenant reads remain available because user-management responses reference
// the default tenant metadata.
type communityAuthorizer struct{}

func (communityAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) AuthorizationDecision {
	if strings.HasPrefix(request.Path, "/api/v1/tenants") {
		return AuthorizationDecision{
			Allowed: false,
			Code:    "community_single_administrator",
			Message: "HyperCDR Community uses one built-in administrator. Multi-user and tenant management are available in HyperCDR Enterprise.",
		}
	}
	if strings.HasPrefix(request.Path, "/api/v1/users") && request.Method != http.MethodGet && request.Method != http.MethodPatch && !(request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/password")) {
		return AuthorizationDecision{Allowed: false, Code: "community_single_administrator", Message: "HyperCDR Community cannot create or delete additional users."}
	}
	return AuthorizationDecision{Allowed: true}
}

func normalizeOptions(options Options) Options {
	if options.Edition == "" {
		options = CommunityOptions()
	}
	if options.Capabilities == nil {
		options.Capabilities = map[string]Capability{}
	}
	if options.DiagnosticLogRetention <= 0 {
		options.DiagnosticLogRetention = 30 * 24 * time.Hour
	}
	return options
}
