package httpserver

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

type Router struct {
	cfg                    config.Config
	logger                 *slog.Logger
	mux                    *http.ServeMux
	store                  store.Store
	hub                    *sessionHub
	captchaMu              sync.Mutex
	captchas               map[string]captchaChallenge
	oauthMu                sync.Mutex
	oauthStates            map[string]time.Time
	inventoryMu            sync.Mutex
	inventory              map[string]inventoryRequestStatus
	imageDigestMu          sync.Mutex
	imageDigests           map[string]imageDigestCacheEntry
	releaseCatalogMu       sync.Mutex
	releaseCatalogItems    []store.PlatformRelease
	releaseCatalogAt       time.Time
	releaseCatalogFailure  time.Time
	logRequestMu           sync.Mutex
	logRequests            map[string]chan protocol.LogReportPayload
	backupContentRequestMu sync.Mutex
	backupContentRequests  map[string]chan protocol.BackupContentReportPayload
	contentIndexMu         sync.Mutex
	contentIndexing        map[string]struct{}
	contentIndexSlots      chan struct{}
	taskDispatchMu         sync.Mutex
	logCollectMu           sync.Mutex
	logMaintMu             sync.Mutex
	logMaintRun            bool
	logCleanupAt           time.Time
	cceRegistrationMu      sync.Mutex
	cceRegistrationUploads map[string]cceKubeconfigUpload
	diagnosticLogRetention time.Duration
	extensionRoutes        []ExtensionRoute
	logRetryAfter          map[string]time.Time
	schedulerOnce          sync.Once
	productInfo            ProductInfo
	productInfoProvider    func() ProductInfo
	editionAuthorizer      EditionAuthorizer
	editionAdmission       EditionAdmissionController
	editionMetering        EditionMeteringObserver
	identityProvider       EditionIdentityProvider
	auditSink              EditionAuditSink
}

type ProductInfo struct {
	Product      string `json:"product"`
	Edition      string `json:"edition"`
	Capabilities any    `json:"capabilities"`
	License      any    `json:"license"`
}

type EditionPrincipal struct {
	ID, TenantID, Email, Role string
	SystemAdmin               bool
}

type EditionAuthorizationRequest struct {
	Method    string
	Path      string
	Principal EditionPrincipal
}

type EditionAuthorizationDecision struct {
	Allowed bool
	Code    string
	Message string
}

type EditionAuthorizer func(context.Context, EditionAuthorizationRequest) EditionAuthorizationDecision
type EditionAdmissionRequest struct {
	Operation   string
	TenantID    string
	ClusterID   string
	WorkerNodes int
	Required    EditionLicenseUsageDelta
	ReleaseDate time.Time
}
type EditionLicenseUsageDelta struct {
	WorkerNodes int
	Clusters    int
	Tenants     int
}
type EditionAdmissionController func(context.Context, EditionAdmissionRequest) EditionAuthorizationDecision
type EditionMeteredNode struct {
	Name     string
	Billable bool
}
type EditionMeteringEvent struct {
	Operation string
	TenantID  string
	ClusterID string
	Nodes     []EditionMeteredNode
}
type EditionMeteringObserver func(context.Context, EditionMeteringEvent) error
type EditionAgentTaskDispatcher func(context.Context, string, string, string, time.Time) error

type RouterOption func(*Router)

func WithEditionAdmissionController(controller EditionAdmissionController) RouterOption {
	return func(router *Router) { router.editionAdmission = controller }
}

func WithEditionMeteringObserver(observer EditionMeteringObserver) RouterOption {
	return func(router *Router) { router.editionMetering = observer }
}

type ExtensionRoute struct {
	Pattern string
	Handler func(http.ResponseWriter, *http.Request, EditionPrincipal)
}

type EditionIdentity struct {
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

type EditionIdentitySession struct {
	Token     string
	ExpiresAt time.Time
}

type EditionIdentityProfileUpdate struct{ ID, Email, DisplayName, TimeZone string }

type EditionIdentityProvider interface {
	Authenticate(context.Context, string, string) (EditionIdentity, bool, error)
	CreateSession(context.Context, string, time.Duration) (EditionIdentitySession, error)
	AuthenticateSession(context.Context, string) (EditionIdentity, bool, error)
	DeleteSession(context.Context, string) error
	UpdateProfile(context.Context, EditionIdentityProfileUpdate) (EditionIdentity, bool, error)
	SetPassword(context.Context, string, string, bool) (EditionIdentity, bool, error)
	CreatePasswordResetToken(context.Context, string, time.Duration) (string, bool, error)
	ResetPassword(context.Context, string, string) (bool, error)
}

type EditionAuditEvent struct {
	TenantID, ActorID, Actor, Action, ResourceType, ResourceID, ResourceName, Result, Message string
	HTTPStatus                                                                                int
}
type EditionAuditSink interface {
	RecordAudit(context.Context, EditionAuditEvent) error
}

func WithDiagnosticLogRetention(retention time.Duration) RouterOption {
	return func(router *Router) {
		if retention > 0 {
			router.diagnosticLogRetention = retention
		}
	}
}

func WithExtensionRoutes(routes []ExtensionRoute) RouterOption {
	return func(router *Router) {
		router.extensionRoutes = append([]ExtensionRoute(nil), routes...)
	}
}

func WithIdentityProvider(provider EditionIdentityProvider) RouterOption {
	return func(router *Router) { router.identityProvider = provider }
}

func WithAuditSink(sink EditionAuditSink) RouterOption {
	return func(router *Router) { router.auditSink = sink }
}

func WithEditionRuntimeBinder(binder func(EditionAgentTaskDispatcher)) RouterOption {
	return func(router *Router) {
		if binder != nil {
			binder(router.dispatchControlPlaneHandover)
		}
	}
}

type requestUserContextKey struct{}
type requestIDContextKey struct{}

const (
	agentPongWait   = 90 * time.Second
	agentPingPeriod = 30 * time.Second
	imageDigestTTL  = 10 * time.Minute
	veleroCRDsPath  = "/assets/velero/v1.18.2/crds.yaml"
)

var cleanObjectStoragePrefix = deleteObjectStoragePrefix

type captchaChallenge struct {
	Code      string
	ExpiresAt time.Time
}

func (r *Router) getProductInfo(w http.ResponseWriter, _ *http.Request) {
	if r.productInfoProvider != nil {
		writeJSON(w, http.StatusOK, r.productInfoProvider())
		return
	}
	writeJSON(w, http.StatusOK, r.productInfo)
}

func WithProductInfoProvider(provider func() ProductInfo) RouterOption {
	return func(router *Router) { router.productInfoProvider = provider }
}

type inventoryRequestStatus struct {
	RequestID   string    `json:"requestId"`
	MessageID   string    `json:"messageId,omitempty"`
	ClusterID   string    `json:"clusterId"`
	Scope       string    `json:"scope"`
	Namespace   string    `json:"namespace,omitempty"`
	Status      string    `json:"status"`
	ErrorCode   string    `json:"errorCode,omitempty"`
	Message     string    `json:"message,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	CompletedAt time.Time `json:"completedAt,omitempty"`
}

type imageDigestCacheEntry struct {
	Digest    string
	ExpiresAt time.Time
}

func NewRouter(cfg config.Config, logger *slog.Logger, repo store.Store) http.Handler {
	return NewRouterWithProductInfo(cfg, logger, repo, ProductInfo{Product: "HyperCDR", Edition: "community", Capabilities: map[string]any{}}, nil)
}

func NewRouterWithProductInfo(cfg config.Config, logger *slog.Logger, repo store.Store, productInfo ProductInfo, args ...any) http.Handler {
	var editionAuthorizer EditionAuthorizer
	router := &Router{
		cfg:                    cfg,
		logger:                 logger,
		mux:                    http.NewServeMux(),
		store:                  repo,
		hub:                    newSessionHub(),
		captchas:               map[string]captchaChallenge{},
		oauthStates:            map[string]time.Time{},
		inventory:              map[string]inventoryRequestStatus{},
		imageDigests:           map[string]imageDigestCacheEntry{},
		logRequests:            map[string]chan protocol.LogReportPayload{},
		backupContentRequests:  map[string]chan protocol.BackupContentReportPayload{},
		contentIndexing:        map[string]struct{}{},
		contentIndexSlots:      make(chan struct{}, 2),
		logRetryAfter:          map[string]time.Time{},
		cceRegistrationUploads: map[string]cceKubeconfigUpload{},
		productInfo:            productInfo,
		diagnosticLogRetention: 30 * 24 * time.Hour,
	}
	for _, arg := range args {
		switch value := arg.(type) {
		case EditionAuthorizer:
			editionAuthorizer = value
		case RouterOption:
			value(router)
		}
	}
	if router.identityProvider == nil {
		router.identityProvider = storeIdentityProvider{store: repo}
	}
	if router.auditSink == nil {
		router.auditSink = storeAuditSink{store: repo}
	}
	router.editionAuthorizer = editionAuthorizer
	router.routes()
	router.mountExtensionRoutes()
	router.startCCEKubeconfigJanitor()
	router.startScheduler()
	return router.withPlatformAuth(router.withAccessLog(router.withAuditLog(router.mux)))
}

func (r *Router) mountExtensionRoutes() {
	for _, route := range r.extensionRoutes {
		route := route
		if strings.TrimSpace(route.Pattern) == "" || route.Handler == nil {
			panic("invalid edition extension route")
		}
		r.mux.HandleFunc(route.Pattern, func(w http.ResponseWriter, req *http.Request) {
			user, ok := requestUser(req)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication_required"})
				return
			}
			route.Handler(w, req, EditionPrincipal{ID: user.ID, TenantID: user.TenantID, Email: user.Email, Role: user.Role, SystemAdmin: user.SystemAdmin})
		})
	}
}

func validReleaseToken(expected, provided string) bool {
	expected = strings.TrimSpace(expected)
	provided = strings.TrimSpace(provided)
	if expected == "" || provided == "" || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func requiresAdmin(req *http.Request) bool {
	p := req.URL.Path
	if strings.HasPrefix(p, "/api/v1/users") {
		return true
	}
	if req.Method != http.MethodGet && (strings.HasPrefix(p, "/api/v1/platform/releases") || strings.HasPrefix(p, "/api/v1/platform/upgrades")) {
		return true
	}
	return (strings.Contains(p, "/agent/upgrade") || strings.Contains(p, "/velero/upgrade")) && req.Method == http.MethodPost
}

func requiresSystemAdmin(req *http.Request) bool {
	p := req.URL.Path
	if strings.HasPrefix(p, "/api/v1/tenants") {
		return true
	}
	if strings.HasPrefix(p, "/api/v1/email-settings") {
		return true
	}
	return strings.HasPrefix(p, "/api/v1/platform/releases") || strings.HasPrefix(p, "/api/v1/platform/upgrades")
}

type backupTaskRequest struct {
	ClusterID               string                  `json:"clusterId"`
	AppID                   string                  `json:"appId"`
	ProtectionPlanID        string                  `json:"protectionPlanId"`
	SourceNamespace         string                  `json:"sourceNamespace"`
	SourceNamespaces        []string                `json:"sourceNamespaces"`
	Scope                   string                  `json:"scope"`
	IncludedResources       []string                `json:"includedResources"`
	LabelSelector           store.LabelSelector     `json:"labelSelector"`
	StorageRepo             string                  `json:"storageRepo"`
	ExcludedResources       []string                `json:"excludedResources"`
	ResourceSelection       store.ResourceSelection `json:"resourceSelection"`
	IncludeClusterResources bool                    `json:"includeClusterResources"`
	Trigger                 string                  `json:"trigger"`
	RequestedBy             string                  `json:"-"`
}

func requestActor(req *http.Request) string {
	if user, ok := requestUser(req); ok {
		return user.Email
	}
	return "System"
}

func tenantVisible(req *http.Request, tenantID string) bool {
	user, ok := requestUser(req)
	return !ok || tenantID == user.TenantID
}

func (r *Router) clusterVisible(req *http.Request, clusterID string) bool {
	clusters, err := r.store.ListClusters()
	if err != nil {
		return false
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return tenantVisible(req, cluster.TenantID)
		}
	}
	return false
}

func (r *Router) tenantGuard(kind string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		user, ok := requestUser(req)
		if !ok {
			next(w, req)
			return
		}
		id := req.PathValue("id")
		allowed := false
		switch kind {
		case "cluster", "cluster-log":
			items, _ := r.store.ListClusters()
			for _, item := range items {
				if item.ID == id && (item.TenantID == user.TenantID || kind == "cluster-log" && user.SystemAdmin) {
					allowed = true
					break
				}
			}
		case "storage":
			item, found, _ := r.store.GetStorageRepository(id)
			allowed = found && item.TenantID == user.TenantID
		case "policy":
			items, _ := r.store.ListPolicies()
			for _, item := range items {
				if item.ID == id && item.TenantID == user.TenantID {
					allowed = true
					break
				}
			}
		case "tag":
			items, _ := r.store.ListTags()
			for _, item := range items {
				if item.ID == id && item.TenantID == user.TenantID {
					allowed = true
					break
				}
			}
		case "plan":
			item, found, _ := r.store.GetProtectionPlan(id)
			allowed = found && item.TenantID == user.TenantID
		case "application":
			app, found, _ := r.store.GetApplication(id)
			if found {
				clusters, _ := r.store.ListClusters()
				for _, cluster := range clusters {
					if cluster.ID == app.ClusterID && cluster.TenantID == user.TenantID {
						allowed = true
						break
					}
				}
			}
		case "task":
			items, _ := r.store.ListTasks("")
			for _, item := range items {
				if item.ID == id && item.TenantID == user.TenantID {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource_not_found"})
			return
		}
		next(w, req)
	}
}

func (r *Router) updateCommunityAdminRecoveryEmail(w http.ResponseWriter, req *http.Request) {
	actor, ok := requestUser(req)
	if !ok || !actor.SystemAdmin || actor.ID != req.PathValue("id") {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if decodeJSON(req, &body) != nil || !validUserEmail(body.Email) {
		writeJSON(w, 400, map[string]any{"error": "email_invalid"})
		return
	}
	email, found, err := r.store.SetAdminRecoveryEmail(actor.ID, body.Email)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "recovery_email_update_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"recoveryEmail": email, "verified": true})
}

func (r *Router) updateCommunityAdmin(w http.ResponseWriter, req *http.Request) {
	actor, ok := requestUser(req)
	if !ok || !actor.SystemAdmin || actor.ID != req.PathValue("id") {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	var body struct {
		DisplayName string `json:"displayName"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	item, found, err := r.store.UpdateUser(store.UserUpdateInput{ID: actor.ID, TenantID: actor.TenantID, Email: actor.Email, DisplayName: body.DisplayName, Role: "admin", Status: "active", TimeZone: actor.TimeZone})
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "update_user_failed"})
		return
	}
	item.Role = "System Administrator"
	writeJSON(w, 200, item)
}

func (r *Router) resetCommunityAdminPassword(w http.ResponseWriter, req *http.Request) {
	actor, ok := requestUser(req)
	if !ok || !actor.SystemAdmin || actor.ID != req.PathValue("id") {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if decodeJSON(req, &body) != nil || !validUserPassword(body.Password) {
		writeJSON(w, 400, map[string]any{"error": "password_invalid"})
		return
	}
	_, found, err := r.store.SetUserPassword(actor.ID, body.Password, false)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "password_update_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"updated": true})
}
