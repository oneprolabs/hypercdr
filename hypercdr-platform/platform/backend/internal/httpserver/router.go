package httpserver

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hypercdr-platform/platform/backend/internal/buildinfo"
	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/internal/veleroassets"

	"github.com/gorilla/websocket"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Router struct {
	cfg           config.Config
	logger        *slog.Logger
	mux           *http.ServeMux
	store         store.Store
	hub           *sessionHub
	captchaMu     sync.Mutex
	captchas      map[string]captchaChallenge
	oauthMu       sync.Mutex
	oauthStates   map[string]time.Time
	inventoryMu   sync.Mutex
	inventory     map[string]inventoryRequestStatus
	imageDigestMu sync.Mutex
	imageDigests  map[string]imageDigestCacheEntry
	logRequestMu  sync.Mutex
	logRequests   map[string]chan protocol.LogReportPayload
	schedulerOnce sync.Once
}

type requestUserContextKey struct{}
type requestIDContextKey struct{}

const (
	agentPongWait   = 90 * time.Second
	agentPingPeriod = 30 * time.Second
	imageDigestTTL  = 10 * time.Minute
)

var cleanObjectStoragePrefix = deleteObjectStoragePrefix

type captchaChallenge struct {
	Code      string
	ExpiresAt time.Time
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
	router := &Router{
		cfg:          cfg,
		logger:       logger,
		mux:          http.NewServeMux(),
		store:        repo,
		hub:          newSessionHub(),
		captchas:     map[string]captchaChallenge{},
		oauthStates:  map[string]time.Time{},
		inventory:    map[string]inventoryRequestStatus{},
		imageDigests: map[string]imageDigestCacheEntry{},
		logRequests:  map[string]chan protocol.LogReportPayload{},
	}
	router.routes()
	router.startScheduler()
	return router.withPlatformAuth(router.withAccessLog(router.withAuditLog(router.mux)))
}

func (r *Router) withPlatformAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path
		publicAuth := path == "/api/v1/auth/captcha" || path == "/api/v1/auth/login" || path == "/api/v1/auth/forgot-password" || path == "/api/v1/auth/reset-password" || path == "/api/v1/auth/config"
		if _, isMemory := r.store.(*store.MemoryStore); isMemory || !strings.HasPrefix(path, "/api/v1/") || publicAuth || path == "/api/v1/agent-tokens/validate" {
			next.ServeHTTP(w, req)
			return
		}
		pipelineReleaseMutation := req.Method == http.MethodPost && (path == "/api/v1/platform/releases" || path == "/api/v1/component-releases")
		if pipelineReleaseMutation && validReleaseToken(r.cfg.ReleaseToken, req.Header.Get("X-HyperCDR-Release-Token")) {
			pipeline := store.User{Email: "release-pipeline", Role: "admin", Status: "active"}
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, pipeline)))
			return
		}
		header := strings.TrimSpace(req.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication_required", "message": "Sign in to continue."})
			return
		}
		user, ok, err := r.store.AuthenticatePlatformSession(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			r.logger.Error("session authentication failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "session_check_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "session_expired", "message": "Your session has expired. Sign in again."})
			return
		}
		passwordChangeAllowed := path == "/api/v1/auth/me" || path == "/api/v1/auth/change-password" || path == "/api/v1/auth/logout"
		if user.MustChangePassword && !passwordChangeAllowed {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "password_change_required", "message": "Change your temporary password before continuing."})
			return
		}
		if requiresAdmin(req) && user.Role != "admin" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "administrator_required", "message": "Administrator permission is required."})
			return
		}
		if requiresSystemAdmin(req) && !user.SystemAdmin && user.Email != "release-pipeline" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "system_administrator_required", "message": "System administrator permission is required."})
			return
		}
		next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user)))
	})
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
	if req.Method != http.MethodGet && (strings.HasPrefix(p, "/api/v1/platform/releases") || strings.HasPrefix(p, "/api/v1/platform/upgrades") || strings.HasPrefix(p, "/api/v1/component-releases")) {
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
	return strings.HasPrefix(p, "/api/v1/platform/releases") || strings.HasPrefix(p, "/api/v1/platform/upgrades") || strings.HasPrefix(p, "/api/v1/component-releases")
}

type backupTaskRequest struct {
	ClusterID               string              `json:"clusterId"`
	AppID                   string              `json:"appId"`
	ProtectionPlanID        string              `json:"protectionPlanId"`
	SourceNamespace         string              `json:"sourceNamespace"`
	SourceNamespaces        []string            `json:"sourceNamespaces"`
	Scope                   string              `json:"scope"`
	IncludedResources       []string            `json:"includedResources"`
	LabelSelector           store.LabelSelector `json:"labelSelector"`
	StorageRepo             string              `json:"storageRepo"`
	ExcludedResources       []string            `json:"excludedResources"`
	IncludeClusterResources bool                `json:"includeClusterResources"`
	Trigger                 string              `json:"trigger"`
	RequestedBy             string              `json:"-"`
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
		case "cluster":
			items, _ := r.store.ListClusters()
			for _, item := range items {
				if item.ID == id && item.TenantID == user.TenantID {
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

func (r *Router) routes() {
	r.mux.HandleFunc("GET /healthz", r.healthz)
	r.mux.HandleFunc("GET /readyz", r.readyz)
	r.mux.HandleFunc("GET /api/v1/platform/version", r.platformVersion)
	r.mux.HandleFunc("GET /api/v1/platform/releases", r.listPlatformReleases)
	r.mux.HandleFunc("GET /api/v1/platform/releases/discover", r.discoverPlatformReleases)
	r.mux.HandleFunc("POST /api/v1/platform/releases", r.createPlatformRelease)
	r.mux.HandleFunc("POST /api/v1/platform/releases/{id}/activate", r.activatePlatformRelease)
	r.mux.HandleFunc("GET /api/v1/platform/upgrades", r.listPlatformUpgrades)
	r.mux.HandleFunc("GET /api/v1/platform/upgrades/precheck", r.precheckPlatformUpgrade)
	r.mux.HandleFunc("POST /api/v1/platform/upgrades", r.createPlatformUpgrade)
	r.mux.HandleFunc("GET /api/v1/auth/captcha", r.createCaptcha)
	r.mux.HandleFunc("POST /api/v1/auth/login", r.login)
	r.mux.HandleFunc("POST /api/v1/auth/forgot-password", r.forgotPassword)
	r.mux.HandleFunc("POST /api/v1/auth/reset-password", r.resetPassword)
	r.mux.HandleFunc("GET /api/v1/auth/config", r.authConfig)
	r.mux.HandleFunc("POST /api/v1/auth/logout", r.logout)
	r.mux.HandleFunc("GET /api/v1/auth/me", r.currentUser)
	r.mux.HandleFunc("GET /api/v1/audit-logs", r.listAuditLogs)
	r.mux.HandleFunc("GET /api/v1/diagnostic-logs", r.listDiagnosticLogs)
	r.mux.HandleFunc("GET /api/v1/diagnostic-logs/export", r.exportDiagnosticLogs)
	r.mux.HandleFunc("GET /api/v1/diagnostic-log-sources", r.diagnosticLogSources)
	r.mux.HandleFunc("POST /api/v1/clusters/{id}/logs/collect", r.tenantGuard("cluster", r.collectClusterLogs))
	r.mux.HandleFunc("PATCH /api/v1/auth/me", r.updateCurrentUser)
	r.mux.HandleFunc("POST /api/v1/auth/change-password", r.changeOwnPassword)
	r.mux.HandleFunc("GET /api/v1/users", r.listUsers)
	r.mux.HandleFunc("POST /api/v1/users", r.createUser)
	r.mux.HandleFunc("PATCH /api/v1/users/{id}", r.updateUser)
	r.mux.HandleFunc("DELETE /api/v1/users/{id}", r.deleteUser)
	r.mux.HandleFunc("POST /api/v1/users/{id}/password", r.resetUserPassword)
	r.mux.HandleFunc("GET /api/v1/tenants", r.listTenants)
	r.mux.HandleFunc("POST /api/v1/tenants", r.createTenant)
	r.mux.HandleFunc("GET /api/v1/tenants/{id}", r.getTenant)
	r.mux.HandleFunc("PATCH /api/v1/tenants/{id}", r.updateTenant)
	r.mux.HandleFunc("DELETE /api/v1/tenants/{id}", r.deleteTenant)
	r.mux.HandleFunc("GET /api/v1/email-settings", r.getEmailSettings)
	r.mux.HandleFunc("PUT /api/v1/email-settings", r.updateEmailSettings)
	r.mux.HandleFunc("POST /api/v1/email-settings/test", r.testEmailSettings)
	r.mux.HandleFunc("GET /api/v1/clusters", r.listClusters)
	r.mux.HandleFunc("PATCH /api/v1/clusters/{id}", r.tenantGuard("cluster", r.updateCluster))
	r.mux.HandleFunc("DELETE /api/v1/clusters/{id}", r.tenantGuard("cluster", r.deleteCluster))
	r.mux.HandleFunc("POST /api/v1/clusters/{id}/default", r.tenantGuard("cluster", r.setDefaultCluster))
	r.mux.HandleFunc("POST /api/v1/clusters/{id}/force-cleanup", r.tenantGuard("cluster", r.forceCleanupCluster))
	r.mux.HandleFunc("GET /api/v1/clusters/{id}/unregister/precheck", r.tenantGuard("cluster", r.precheckUnregisterCluster))
	r.mux.HandleFunc("POST /api/v1/clusters/{id}/unregister", r.tenantGuard("cluster", r.unregisterCluster))
	r.mux.HandleFunc("POST /api/v1/clusters/{id}/agent/upgrade", r.tenantGuard("cluster", r.upgradeClusterAgent))
	r.mux.HandleFunc("POST /api/v1/clusters/{id}/velero/upgrade", r.tenantGuard("cluster", r.upgradeClusterVelero))
	r.mux.HandleFunc("GET /api/v1/component-releases", r.listComponentReleases)
	r.mux.HandleFunc("POST /api/v1/component-releases", r.createComponentRelease)
	r.mux.HandleFunc("POST /api/v1/component-releases/{id}/activate", r.activateComponentRelease)
	r.mux.HandleFunc("GET /api/v1/component-releases/discover", r.discoverComponentReleases)
	r.mux.HandleFunc("POST /api/v1/clusters/{id}/inventory/request", r.tenantGuard("cluster", r.requestClusterInventory))
	r.mux.HandleFunc("GET /api/v1/clusters/{id}/inventory/requests/{requestId}", r.tenantGuard("cluster", r.getClusterInventoryRequest))
	r.mux.HandleFunc("GET /api/v1/applications", r.listApplications)
	r.mux.HandleFunc("PATCH /api/v1/applications/{id}", r.tenantGuard("application", r.updateApplication))
	r.mux.HandleFunc("PUT /api/v1/applications/{id}/tags", r.tenantGuard("application", r.setApplicationTags))
	r.mux.HandleFunc("GET /api/v1/tags", r.listTags)
	r.mux.HandleFunc("POST /api/v1/tags", r.createTag)
	r.mux.HandleFunc("PATCH /api/v1/tags/{id}", r.tenantGuard("tag", r.updateTag))
	r.mux.HandleFunc("DELETE /api/v1/tags/{id}", r.tenantGuard("tag", r.deleteTag))
	r.mux.HandleFunc("GET /api/v1/storage-repositories", r.listStorageRepositories)
	r.mux.HandleFunc("POST /api/v1/storage-repositories", r.createStorageRepository)
	r.mux.HandleFunc("PATCH /api/v1/storage-repositories/{id}", r.tenantGuard("storage", r.updateStorageRepository))
	r.mux.HandleFunc("DELETE /api/v1/storage-repositories/{id}", r.tenantGuard("storage", r.deleteStorageRepository))
	r.mux.HandleFunc("POST /api/v1/storage-repositories/test", r.testStorageRepositoryDraft)
	r.mux.HandleFunc("POST /api/v1/storage-repositories/{id}/sync", r.tenantGuard("storage", r.syncStorageRepository))
	r.mux.HandleFunc("POST /api/v1/storage-repositories/{id}/test", r.tenantGuard("storage", r.testStorageRepository))
	r.mux.HandleFunc("GET /api/v1/policies", r.listPolicies)
	r.mux.HandleFunc("POST /api/v1/policies", r.createPolicy)
	r.mux.HandleFunc("PATCH /api/v1/policies/{id}", r.tenantGuard("policy", r.updatePolicy))
	r.mux.HandleFunc("DELETE /api/v1/policies/{id}", r.tenantGuard("policy", r.deletePolicy))
	r.mux.HandleFunc("GET /api/v1/protection-plans", r.listProtectionPlans)
	r.mux.HandleFunc("POST /api/v1/protection-plans", r.createProtectionPlan)
	r.mux.HandleFunc("POST /api/v1/protection-plans/{id}/activate", r.tenantGuard("plan", r.activateProtectionPlan))
	r.mux.HandleFunc("POST /api/v1/protection-plans/{id}/storage/reconfigure", r.tenantGuard("plan", r.reconfigureProtectionPlanStorage))
	r.mux.HandleFunc("DELETE /api/v1/protection-plans/{id}", r.tenantGuard("plan", r.deleteProtectionPlan))
	r.mux.HandleFunc("GET /api/v1/restore-points", r.listRestorePoints)
	r.mux.HandleFunc("POST /api/v1/restore-points/delete", r.deleteRestorePoints)
	r.mux.HandleFunc("GET /api/v1/tasks", r.listTasks)
	r.mux.HandleFunc("GET /api/v1/tasks/{id}/events", r.tenantGuard("task", r.listTaskEvents))
	r.mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", r.tenantGuard("task", r.cancelTask))
	r.mux.HandleFunc("POST /api/v1/tasks/backup", r.createBackupTask)
	r.mux.HandleFunc("POST /api/v1/tasks/restore", r.createRestoreTask)
	r.mux.HandleFunc("POST /api/v1/tasks/drill", r.createDrillTask)
	r.mux.HandleFunc("POST /api/v1/tasks/takeover", r.createTakeoverTask)
	r.mux.HandleFunc("POST /api/v1/agent-tokens", r.createAgentToken)
	r.mux.HandleFunc("POST /api/v1/agent-tokens/validate", r.validateAgentToken)
	r.mux.HandleFunc("GET /prepare-node.sh", r.prepareNodeScript)
	r.mux.HandleFunc("GET /install.sh", r.installScript)
	r.mux.HandleFunc("GET /assets/velero/v1.17.1/crds.yaml", r.veleroCRDs)
	r.mux.HandleFunc("GET /assets/registry/ca.crt", r.registryCA)
	r.mux.HandleFunc("GET /ws/agent", r.agentWebSocket)
	if strings.TrimSpace(r.cfg.FrontendDir) != "" {
		r.mux.HandleFunc("GET /", r.frontend)
	}
}

type auditResponseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *auditResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.body.Len() < 64*1024 {
		remaining := 64*1024 - w.body.Len()
		if len(body) < remaining {
			remaining = len(body)
		}
		_, _ = w.body.Write(body[:remaining])
	}
	return w.ResponseWriter.Write(body)
}

func (r *Router) withAuditLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		user, authenticated := requestUser(req)
		if !authenticated || !isAuditedMutation(req) {
			next.ServeHTTP(w, req)
			return
		}
		action, resourceType, pathResourceID, recognized := auditOperation(req)
		if !recognized {
			next.ServeHTTP(w, req)
			return
		}
		recorder := &auditResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, req)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		response := map[string]any{}
		_ = json.Unmarshal(recorder.body.Bytes(), &response)
		resourceID := auditResponseString(response, "id")
		if resourceID == "" {
			resourceID = pathResourceID
		}
		resourceName := auditResponseString(response, "name")
		if resourceName == "" {
			resourceName = auditResponseString(response, "displayName")
		}
		if resourceName == "" {
			resourceName = auditResponseString(response, "email")
		}
		if payload, ok := response["payload"].(map[string]any); ok && resourceName == "" {
			resourceName = auditResponseString(payload, "sourceNamespace")
		}
		result := "Success"
		message := auditResponseString(response, "message")
		if status >= http.StatusBadRequest {
			result = "Failed"
			if message == "" {
				message = auditResponseString(response, "error")
			}
		}
		_, err := r.store.CreateAuditLog(store.AuditLogInput{ActorID: user.ID, Actor: user.Email, Action: action, ResourceType: resourceType, ResourceID: validAuditUUID(resourceID), ResourceName: resourceName, Result: result, Message: message, Payload: map[string]any{"httpStatus": status}})
		if err != nil {
			r.logger.Error("write audit log failed", "error", err, "action", action, "actor", user.Email)
		}
	})
}

func isAuditedMutation(req *http.Request) bool {
	if req.Method != http.MethodPost && req.Method != http.MethodPatch && req.Method != http.MethodPut && req.Method != http.MethodDelete {
		return false
	}
	path := req.URL.Path
	return path != "/api/v1/auth/login" && !strings.HasPrefix(path, "/api/v1/agent-tokens") && !strings.HasPrefix(path, "/api/v1/audit-logs")
}

func auditOperation(req *http.Request) (string, string, string, bool) {
	path := req.URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	resourceType, resourceID := "Platform", ""
	if len(parts) >= 3 {
		resourceType = strings.ReplaceAll(parts[2], "-", " ")
	}
	if len(parts) >= 4 {
		resourceID = parts[3]
	}
	action := map[string]string{
		"POST /api/v1/auth/logout": "Sign Out", "PATCH /api/v1/auth/me": "Update Profile", "POST /api/v1/auth/change-password": "Change Password",
		"POST /api/v1/users": "Create User", "PATCH /api/v1/users": "Update User", "DELETE /api/v1/users": "Delete User", "POST /api/v1/users/password": "Reset User Password",
		"PATCH /api/v1/clusters": "Update Cluster", "DELETE /api/v1/clusters": "Delete Cluster", "POST /api/v1/clusters/default": "Set Default Cluster", "POST /api/v1/clusters/force-cleanup": "Force Clean Cluster", "POST /api/v1/clusters/unregister": "Unregister Cluster", "POST /api/v1/clusters/agent/upgrade": "Upgrade Comm Agent", "POST /api/v1/clusters/velero/upgrade": "Upgrade Velero Agent", "POST /api/v1/clusters/inventory/request": "Refresh Cluster Inventory",
		"PATCH /api/v1/applications": "Update Application", "PUT /api/v1/applications/tags": "Update Application Tags",
		"POST /api/v1/tags": "Create Tag", "PATCH /api/v1/tags": "Update Tag", "DELETE /api/v1/tags": "Delete Tag",
		"POST /api/v1/storage-repositories": "Create Storage", "PATCH /api/v1/storage-repositories": "Update Storage", "DELETE /api/v1/storage-repositories": "Delete Storage", "POST /api/v1/storage-repositories/test": "Test Storage Connection", "POST /api/v1/storage-repositories/sync": "Sync Storage",
		"POST /api/v1/policies": "Create Policy", "PATCH /api/v1/policies": "Update Policy", "DELETE /api/v1/policies": "Delete Policy",
		"POST /api/v1/protection-plans": "Create DR Configuration", "POST /api/v1/protection-plans/storage/reconfigure": "Reconfigure DR Storage", "DELETE /api/v1/protection-plans": "Delete DR Configuration",
		"POST /api/v1/restore-points/delete": "Delete Restore Point", "POST /api/v1/tasks/cancel": "Cancel Task", "POST /api/v1/tasks/backup": "Start Sync", "POST /api/v1/tasks/restore": "Start Restore", "POST /api/v1/tasks/drill": "Start Drill", "POST /api/v1/tasks/takeover": "Start Takeover",
		"POST /api/v1/component-releases": "Register Component Version", "POST /api/v1/component-releases/activate": "Publish Component Version", "POST /api/v1/platform/releases": "Register Platform Version", "POST /api/v1/platform/releases/activate": "Publish Platform Version", "POST /api/v1/platform/upgrades": "Start Platform Upgrade",
	}
	normalized := make([]string, 0, len(parts))
	for _, segment := range parts {
		if validAuditUUID(segment) == "" {
			normalized = append(normalized, segment)
		}
	}
	lookup := req.Method + " /" + strings.Join(normalized, "/")
	if value := action[lookup]; value != "" {
		return value, strings.Title(resourceType), resourceID, true
	}
	return "", "", "", false
}

func auditResponseString(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func validAuditUUID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 36 && strings.Count(value, "-") == 4 {
		return value
	}
	return ""
}

func (r *Router) listAuditLogs(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	items, err := r.store.ListAuditLogs(1000, 0)
	if err != nil {
		r.logger.Error("list audit logs failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_audit_logs_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	if offset >= len(items) {
		items = items[:0]
	} else {
		end := offset + limit
		if end > len(items) {
			end = len(items)
		}
		items = items[offset:end]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) healthz(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (r *Router) readyz(w http.ResponseWriter, req *http.Request) {
	status := "degraded"
	if r.cfg.DatabaseURL != "" {
		status = "ok"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      status,
		"databaseSet": r.cfg.DatabaseURL != "",
	})
}

func (r *Router) platformVersion(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": buildinfo.Version, "gitCommit": buildinfo.GitCommit, "buildTime": buildinfo.BuildTime,
		"databaseSchemaVersion": buildinfo.SchemaVersion, "deployMode": r.cfg.DeployMode,
	})
}

func (r *Router) listPlatformReleases(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPlatformReleases()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_platform_releases_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) registryTags(ctx context.Context, image string) (string, string, []string, error) {
	registry, repository, _, ok := splitContainerImage(image)
	if !ok {
		return "", "", nil, fmt.Errorf("invalid image")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	tokenURL := fmt.Sprintf("https://%s/service/token?service=harbor-registry&scope=repository:%s:pull", registry, repository)
	tokenReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return "", "", nil, err
	}
	defer tokenResp.Body.Close()
	var tokenBody struct {
		Token string `json:"token"`
	}
	if tokenResp.StatusCode >= 300 || json.NewDecoder(tokenResp.Body).Decode(&tokenBody) != nil || tokenBody.Token == "" {
		return "", "", nil, fmt.Errorf("registry token failed")
	}
	tagsReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s/v2/%s/tags/list", registry, repository), nil)
	tagsReq.Header.Set("Authorization", "Bearer "+tokenBody.Token)
	tagsResp, err := client.Do(tagsReq)
	if err != nil {
		return "", "", nil, err
	}
	defer tagsResp.Body.Close()
	var result struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if tagsResp.StatusCode >= 300 || json.NewDecoder(tagsResp.Body).Decode(&result) != nil {
		return "", "", nil, fmt.Errorf("registry tags failed")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result.Tags)))
	return registry, result.Name, result.Tags, nil
}

func (r *Router) discoverPlatformReleases(w http.ResponseWriter, req *http.Request) {
	registry := strings.TrimRight(r.cfg.ImageRegistry, "/")
	apiImage := registry + "/platform-api:latest"
	frontendImage := registry + "/platform-frontend:latest"
	upgraderImage := registry + "/platform-upgrader:latest"
	_, _, apiTags, err := r.registryTags(req.Context(), apiImage)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": "platform_api_discovery_failed", "message": err.Error()})
		return
	}
	_, _, frontendTags, err := r.registryTags(req.Context(), frontendImage)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": "platform_frontend_discovery_failed", "message": err.Error()})
		return
	}
	_, _, upgraderTags, err := r.registryTags(req.Context(), upgraderImage)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": "platform_upgrader_discovery_failed", "message": err.Error()})
		return
	}
	front := map[string]bool{}
	for _, tag := range frontendTags {
		front[tag] = true
	}
	upgrader := map[string]bool{}
	for _, tag := range upgraderTags {
		upgrader[tag] = true
	}
	versions := []string{}
	for _, tag := range apiTags {
		if front[tag] && upgrader[tag] {
			versions = append(versions, tag)
		}
	}
	writeJSON(w, 200, map[string]any{"registry": registry, "versions": versions})
}

func (r *Router) createPlatformRelease(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Version, DatabaseSchemaVersion, MinimumAgentVersion, ReleaseNotes string
		RollbackSupported                                                 bool
	}
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Version) == "" {
		writeJSON(w, 400, map[string]any{"error": "version_required"})
		return
	}
	registry := strings.TrimRight(r.cfg.ImageRegistry, "/")
	apiImage := registry + "/platform-api:" + body.Version
	frontendImage := registry + "/platform-frontend:" + body.Version
	upgraderImage := registry + "/platform-upgrader:" + body.Version
	apiDigest, err := r.resolveImageDigest(req.Context(), apiImage)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": "platform_api_image_unavailable", "message": err.Error()})
		return
	}
	frontendDigest, err := r.resolveImageDigest(req.Context(), frontendImage)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": "platform_frontend_image_unavailable", "message": err.Error()})
		return
	}
	if _, err = r.resolveImageDigest(req.Context(), upgraderImage); err != nil {
		writeJSON(w, 502, map[string]any{"error": "platform_upgrader_image_unavailable", "message": err.Error()})
		return
	}
	schema := strings.TrimSpace(body.DatabaseSchemaVersion)
	if schema == "" {
		schema = buildinfo.SchemaVersion
	}
	item, err := r.store.UpsertPlatformRelease(store.PlatformReleaseInput{Version: body.Version, APIImage: apiImage, APIImageDigest: apiDigest, FrontendImage: frontendImage, FrontendImageDigest: frontendDigest, DatabaseSchemaVersion: schema, MinimumAgentVersion: body.MinimumAgentVersion, RollbackSupported: body.RollbackSupported, ReleaseNotes: body.ReleaseNotes, Status: "candidate"})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "create_platform_release_failed"})
		return
	}
	writeJSON(w, 201, item)
}
func (r *Router) activatePlatformRelease(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPlatformReleases()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_platform_releases_failed"})
		return
	}
	id := req.PathValue("id")
	var selected store.PlatformRelease
	for _, v := range items {
		if v.ID == id {
			selected = v
		}
	}
	if selected.ID == "" {
		writeJSON(w, 404, map[string]any{"error": "platform_release_not_found"})
		return
	}
	apiDigest, e1 := r.resolveImageDigest(req.Context(), selected.APIImage)
	frontDigest, e2 := r.resolveImageDigest(req.Context(), selected.FrontendImage)
	if e1 != nil || e2 != nil || apiDigest != selected.APIImageDigest || frontDigest != selected.FrontendImageDigest {
		writeJSON(w, 409, map[string]any{"error": "platform_release_images_changed"})
		return
	}
	item, ok, err := r.store.ActivatePlatformRelease(id, "admin")
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "activate_platform_release_failed"})
		return
	}
	if !ok {
		writeJSON(w, 404, map[string]any{"error": "platform_release_not_found"})
		return
	}
	writeJSON(w, 200, item)
}

func (r *Router) platformPrecheck(releaseID string) ([]map[string]any, bool, store.PlatformRelease) {
	items, _ := r.store.ListPlatformReleases()
	var release store.PlatformRelease
	for _, v := range items {
		if v.ID == releaseID {
			release = v
		}
	}
	clusters, _ := r.store.ListClusters()
	tasks, _ := r.store.ListTasks("")
	activeTasks := 0
	for _, t := range tasks {
		if !isTerminalTaskStatus(t.Status) {
			activeTasks++
		}
	}
	offline := 0
	for _, c := range clusters {
		if !r.hub.has(c.ID) {
			offline++
		}
	}
	checks := []map[string]any{{"id": "release", "label": "Release package is registered", "passed": release.ID != ""}, {"id": "mode", "label": "Formal deployment mode", "passed": r.cfg.DeployMode != "development", "detail": r.cfg.DeployMode}, {"id": "tasks", "label": "No active DR tasks", "passed": activeTasks == 0, "detail": activeTasks}, {"id": "agents", "label": "All registered agents are online", "passed": offline == 0, "detail": offline}, {"id": "version", "label": "Target differs from running version", "passed": release.Version != "" && release.Version != buildinfo.Version}}
	passed := true
	for _, c := range checks {
		if ok, _ := c["passed"].(bool); !ok {
			passed = false
		}
	}
	return checks, passed, release
}
func (r *Router) precheckPlatformUpgrade(w http.ResponseWriter, req *http.Request) {
	checks, passed, _ := r.platformPrecheck(req.URL.Query().Get("releaseId"))
	writeJSON(w, 200, map[string]any{"passed": passed, "checks": checks, "currentVersion": buildinfo.Version})
}
func (r *Router) listPlatformUpgrades(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPlatformUpgradeJobs()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_platform_upgrades_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNilSlice(items)})
}
func (r *Router) createPlatformUpgrade(w http.ResponseWriter, req *http.Request) {
	var body struct {
		ReleaseID string `json:"releaseId"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	checks, passed, release := r.platformPrecheck(body.ReleaseID)
	if !passed {
		writeJSON(w, 409, map[string]any{"error": "platform_precheck_failed", "checks": checks})
		return
	}
	job, err := r.store.CreatePlatformUpgradeJob(store.PlatformUpgradeJobInput{Release: release, FromVersion: buildinfo.Version, RequestedBy: "admin"})
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "platform_upgrade_active", "message": err.Error()})
		return
	}
	writeJSON(w, 202, job)
}

func (r *Router) frontend(w http.ResponseWriter, req *http.Request) {
	frontendDir := strings.TrimSpace(r.cfg.FrontendDir)
	if frontendDir == "" {
		http.NotFound(w, req)
		return
	}

	cleanPath := filepath.Clean(strings.TrimPrefix(req.URL.Path, "/"))
	if cleanPath == "." {
		cleanPath = "index.html"
	}
	fullPath := filepath.Join(frontendDir, cleanPath)
	if rel, err := filepath.Rel(frontendDir, fullPath); err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, req)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		fullPath = filepath.Join(frontendDir, "index.html")
	}
	http.ServeFile(w, req, fullPath)
}

func (r *Router) createCaptcha(w http.ResponseWriter, req *http.Request) {
	code, err := randomDigits(4)
	if err != nil {
		r.logger.Error("failed to generate captcha", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "captcha_generation_failed"})
		return
	}
	id := store.NewPublicID()
	now := time.Now().UTC()
	r.captchaMu.Lock()
	for captchaID, challenge := range r.captchas {
		if now.After(challenge.ExpiresAt) {
			delete(r.captchas, captchaID)
		}
	}
	r.captchas[id] = captchaChallenge{Code: code, ExpiresAt: now.Add(2 * time.Minute)}
	r.captchaMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"id":        id,
		"image":     captchaImageDataURL(code),
		"expiresAt": now.Add(2 * time.Minute).Format(time.RFC3339),
	})
}

func (r *Router) login(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		CaptchaID   string `json:"captchaId"`
		CaptchaCode string `json:"captchaCode"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(body.CaptchaID) == "" || strings.TrimSpace(body.CaptchaCode) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "captcha_required", "message": "Verification code is required"})
		return
	}

	if !r.consumeCaptcha(body.CaptchaID, body.CaptchaCode) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "captcha_invalid", "message": "Verification code is incorrect"})
		return
	}

	user, ok, err := r.store.AuthenticateUser(store.UserAuthInput{
		Email:    body.Email,
		Password: body.Password,
	})
	if err != nil {
		r.logger.Error("failed to authenticate user", "email", body.Email, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "login_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_credentials", "message": "Username or password is incorrect"})
		return
	}

	session, err := r.store.CreatePlatformSession(user.ID, time.Hour)
	if err != nil {
		r.logger.Error("failed to create platform session", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "session_create_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
		"session": map[string]any{
			"token":     session.Token,
			"expiresAt": session.ExpiresAt.Format(time.RFC3339),
		},
	})
}

func bearerToken(req *http.Request) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Header.Get("Authorization")), "Bearer "))
}
func requestUser(req *http.Request) (store.User, bool) {
	u, ok := req.Context().Value(requestUserContextKey{}).(store.User)
	return u, ok
}

func (r *Router) logout(w http.ResponseWriter, req *http.Request) {
	_ = r.store.DeletePlatformSession(bearerToken(req))
	writeJSON(w, http.StatusOK, map[string]any{"loggedOut": true})
}
func (r *Router) currentUser(w http.ResponseWriter, req *http.Request) {
	u, ok := requestUser(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication_required"})
		return
	}
	writeJSON(w, http.StatusOK, u)
}
func (r *Router) updateCurrentUser(w http.ResponseWriter, req *http.Request) {
	u, ok := requestUser(req)
	if !ok {
		return
	}
	var body struct {
		DisplayName string  `json:"displayName"`
		Email       string  `json:"email"`
		TimeZone    *string `json:"timeZone"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(body.Email) == "" {
		body.Email = u.Email
	}
	timeZone := u.TimeZone
	if body.TimeZone != nil {
		timeZone = strings.TrimSpace(*body.TimeZone)
		if timeZone != "" {
			if _, err := time.LoadLocation(timeZone); err != nil {
				writeJSON(w, 400, map[string]any{"error": "invalid_time_zone", "message": "Select a valid IANA time zone."})
				return
			}
		}
	}
	updated, _, err := r.store.UpdateUser(store.UserUpdateInput{ID: u.ID, TenantID: u.TenantID, Email: body.Email, DisplayName: body.DisplayName, Role: u.Role, Status: u.Status, TimeZone: timeZone})
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "user_update_failed", "message": err.Error()})
		return
	}
	writeJSON(w, 200, updated)
}
func (r *Router) changeOwnPassword(w http.ResponseWriter, req *http.Request) {
	u, ok := requestUser(req)
	if !ok {
		return
	}
	var body struct{ CurrentPassword, NewPassword string }
	if decodeJSON(req, &body) != nil || !validUserPassword(body.NewPassword) {
		writeJSON(w, 400, map[string]any{"error": "password_invalid", "message": "Password must be 8 to 128 characters."})
		return
	}
	if body.CurrentPassword == body.NewPassword {
		writeJSON(w, 400, map[string]any{"error": "password_unchanged", "message": "New password must be different from the temporary password."})
		return
	}
	if _, valid, _ := r.store.AuthenticateUser(store.UserAuthInput{Email: u.Email, Password: body.CurrentPassword}); !valid {
		writeJSON(w, 400, map[string]any{"error": "current_password_invalid", "message": "Current password is incorrect."})
		return
	}
	updated, found, err := r.store.SetUserPassword(u.ID, body.NewPassword, false)
	if err != nil || !found {
		writeJSON(w, 500, map[string]any{"error": "password_update_failed"})
		return
	}
	writeJSON(w, 200, updated)
}

func (r *Router) listUsers(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListUsers()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_users_failed"})
		return
	}
	if actor, ok := requestUser(req); ok && !actor.SystemAdmin {
		filtered := make([]store.User, 0, len(items))
		for _, item := range items {
			if item.TenantID == actor.TenantID && !item.SystemAdmin {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, 200, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) listTenants(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListTenants()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_tenants_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNilSlice(items)})
}
func (r *Router) getTenant(w http.ResponseWriter, req *http.Request) {
	item, found, err := r.store.GetTenant(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "get_tenant_failed"})
		return
	}
	if !found {
		writeJSON(w, 404, map[string]any{"error": "tenant_not_found"})
		return
	}
	writeJSON(w, 200, item)
}
func (r *Router) createTenant(w http.ResponseWriter, req *http.Request) {
	var body store.TenantInput
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, 400, map[string]any{"error": "tenant_name_required"})
		return
	}
	item, err := r.store.CreateTenant(body)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "create_tenant_failed", "message": err.Error()})
		return
	}
	writeJSON(w, 201, item)
}
func (r *Router) updateTenant(w http.ResponseWriter, req *http.Request) {
	var body store.TenantInput
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, 400, map[string]any{"error": "tenant_name_required"})
		return
	}
	item, found, err := r.store.UpdateTenant(req.PathValue("id"), body)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "update_tenant_failed", "message": err.Error()})
		return
	}
	if !found {
		writeJSON(w, 404, map[string]any{"error": "tenant_not_found"})
		return
	}
	writeJSON(w, 200, item)
}
func (r *Router) deleteTenant(w http.ResponseWriter, req *http.Request) {
	deleted, inUse, err := r.store.DeleteTenant(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "delete_tenant_failed"})
		return
	}
	if inUse {
		writeJSON(w, 409, map[string]any{"error": "tenant_in_use", "message": "Remove all users and resources from this tenant before deleting it."})
		return
	}
	if !deleted {
		writeJSON(w, 404, map[string]any{"error": "tenant_not_found"})
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}

type emailSettingsRequest struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Security    string `json:"security"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	SenderName  string `json:"senderName"`
	SenderEmail string `json:"senderEmail"`
}

func (r *Router) getEmailSettings(w http.ResponseWriter, req *http.Request) {
	item, found, err := r.store.GetEmailSettings()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_failed"})
		return
	}
	if !found {
		item = store.EmailSettings{Port: 587, Security: "starttls", SenderName: "HyperCDR"}
	}
	writeJSON(w, 200, item)
}
func validateEmailSettings(body emailSettingsRequest) (string, string) {
	if body.Port < 1 || body.Port > 65535 {
		return "invalid_port", "Port must be between 1 and 65535."
	}
	if body.Security != "none" && body.Security != "starttls" && body.Security != "tls" {
		return "invalid_security", "Select TLS, STARTTLS, or None."
	}
	if strings.TrimSpace(body.Host) == "" || !validUserEmail(body.SenderEmail) {
		return "email_settings_incomplete", "SMTP server and a valid sender email are required."
	}
	return "", ""
}
func (r *Router) updateEmailSettings(w http.ResponseWriter, req *http.Request) {
	var body emailSettingsRequest
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if code, message := validateEmailSettings(body); code != "" {
		writeJSON(w, 400, map[string]any{"error": code, "message": message})
		return
	}
	current, _, _ := r.store.GetEmailSettings()
	ciphertext := current.PasswordCiphertext
	if body.Password != "" {
		var err error
		ciphertext, err = r.encryptSetting(body.Password)
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": "email_password_encrypt_failed"})
			return
		}
	}
	actor, _ := requestUser(req)
	item, err := r.store.UpsertEmailSettings(store.EmailSettingsInput{Enabled: true, Host: strings.TrimSpace(body.Host), Port: body.Port, Security: body.Security, Username: strings.TrimSpace(body.Username), PasswordCiphertext: ciphertext, SenderName: strings.TrimSpace(body.SenderName), SenderEmail: strings.TrimSpace(body.SenderEmail), UpdatedBy: actor.ID})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "email_settings_update_failed"})
		return
	}
	writeJSON(w, 200, item)
}
func (r *Router) testEmailSettings(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Recipient string `json:"recipient"`
	}
	if decodeJSON(req, &body) != nil || !validUserEmail(body.Recipient) {
		writeJSON(w, 400, map[string]any{"error": "invalid_recipient"})
		return
	}
	settings, found, err := r.store.GetEmailSettings()
	if err != nil || !found {
		writeJSON(w, 409, map[string]any{"error": "email_not_configured"})
		return
	}
	if err = r.sendConfiguredEmail(settings, strings.TrimSpace(body.Recipient), "HyperCDR email test", "Your HyperCDR SMTP settings are working correctly."); err != nil {
		r.logger.Warn("SMTP test failed", "error", err)
		writeJSON(w, 502, map[string]any{"error": "smtp_test_failed", "message": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"sent": true})
}

func (r *Router) encryptSetting(value string) (string, error) {
	key := sha256.Sum256([]byte(r.cfg.SecretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}
func (r *Router) decryptSetting(value string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(r.cfg.SecretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted setting")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(plain), err
}
func (r *Router) createUser(w http.ResponseWriter, req *http.Request) {
	var body struct {
		TenantID    string `json:"tenantId"`
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		Role        string `json:"role"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if !validUserEmail(body.Email) {
		writeJSON(w, 400, map[string]any{"error": "email_invalid", "message": "Enter a valid email address"})
		return
	}
	if !validUserPassword(body.Password) {
		writeJSON(w, 400, map[string]any{"error": "password_invalid", "message": "Password must be 8 to 128 characters"})
		return
	}
	actor, _ := requestUser(req)
	tenantID := strings.TrimSpace(body.TenantID)
	if !actor.SystemAdmin {
		tenantID = actor.TenantID
	}
	if tenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant_required", "message": "Select a tenant for this user."})
		return
	}
	tenant, found, err := r.store.GetTenant(tenantID)
	if err != nil || !found || tenant.Status != "active" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant_invalid", "message": "Select an active tenant for this user."})
		return
	}
	u, err := r.store.CreateUser(tenantID, body.Email, body.Password)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "create_user_failed", "message": err.Error()})
		return
	}
	role := body.Role
	if role != "admin" {
		role = "operator"
	}
	u, _, err = r.store.UpdateUser(store.UserUpdateInput{ID: u.ID, TenantID: tenantID, Email: u.Email, DisplayName: body.DisplayName, Role: role, Status: "active", TimeZone: u.TimeZone})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "create_user_failed"})
		return
	}
	writeJSON(w, 201, u)
}
func (r *Router) updateUser(w http.ResponseWriter, req *http.Request) {
	current, found, err := r.store.GetUser(req.PathValue("id"))
	if err != nil || !found {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	actor, hasActor := requestUser(req)
	if hasActor && !actor.SystemAdmin && (current.SystemAdmin || current.TenantID != actor.TenantID) {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	var body struct {
		TenantID    string `json:"tenantId"`
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Role        string `json:"role"`
		Status      string `json:"status"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	if current.Email == store.DefaultAdminEmail {
		body.Role = "admin"
		body.Status = "active"
		body.Email = store.DefaultAdminEmail
	} else if !validUserEmail(body.Email) {
		writeJSON(w, 400, map[string]any{"error": "email_invalid", "message": "Enter a valid email address"})
		return
	}
	if body.Role != "admin" {
		body.Role = "operator"
	}
	if body.Status != "disabled" {
		body.Status = "active"
	}
	tenantID := current.TenantID
	if actor.SystemAdmin && !current.SystemAdmin {
		tenantID = strings.TrimSpace(body.TenantID)
		if tenantID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant_required", "message": "Select a tenant for this user."})
			return
		}
		tenant, tenantFound, tenantErr := r.store.GetTenant(tenantID)
		if tenantErr != nil || !tenantFound || tenant.Status != "active" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant_invalid", "message": "Select an active tenant for this user."})
			return
		}
	}
	updated, _, err := r.store.UpdateUser(store.UserUpdateInput{ID: current.ID, TenantID: tenantID, Email: body.Email, DisplayName: body.DisplayName, Role: body.Role, Status: body.Status, TimeZone: current.TimeZone})
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "update_user_failed", "message": err.Error()})
		return
	}
	writeJSON(w, 200, updated)
}
func (r *Router) deleteUser(w http.ResponseWriter, req *http.Request) {
	u, found, _ := r.store.GetUser(req.PathValue("id"))
	if !found {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	actor, hasActor := requestUser(req)
	if hasActor && !actor.SystemAdmin && (u.SystemAdmin || u.TenantID != actor.TenantID) {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	if u.Email == store.DefaultAdminEmail {
		writeJSON(w, 409, map[string]any{"error": "admin_user_protected", "message": "The admin user cannot be deleted."})
		return
	}
	deleted, err := r.store.DeleteUser(u.ID)
	if err != nil || !deleted {
		if err != nil {
			r.logger.Error("failed to delete user", "user_id", u.ID, "error", err)
		}
		writeJSON(w, 500, map[string]any{"error": "delete_user_failed", "message": "User could not be deleted. Please try again."})
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}
func (r *Router) resetUserPassword(w http.ResponseWriter, req *http.Request) {
	var body struct{ Password string }
	if decodeJSON(req, &body) != nil || !validUserPassword(body.Password) {
		writeJSON(w, 400, map[string]any{"error": "password_invalid"})
		return
	}
	target, found, err := r.store.GetUser(req.PathValue("id"))
	if err != nil || !found {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	actor, hasActor := requestUser(req)
	if hasActor && !actor.SystemAdmin && (target.SystemAdmin || target.TenantID != actor.TenantID) {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	u, found, err := r.store.SetUserPassword(target.ID, body.Password, true)
	if err != nil || !found {
		writeJSON(w, 404, map[string]any{"error": "user_not_found"})
		return
	}
	writeJSON(w, 200, u)
}

func (r *Router) consumeCaptcha(id, code string) bool {
	r.captchaMu.Lock()
	challenge, ok := r.captchas[id]
	delete(r.captchas, id)
	r.captchaMu.Unlock()
	return ok && time.Now().UTC().Before(challenge.ExpiresAt) && challenge.Code == strings.TrimSpace(code)
}

func validUserPassword(password string) bool { return len(password) >= 8 && len(password) <= 128 }

func validUserEmail(value string) bool {
	email := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(email)
	at := strings.LastIndexByte(email, '@')
	if err != nil || address.Address != email || at <= 0 || at == len(email)-1 || len(email) > 254 {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

func (r *Router) authConfig(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"googleEnabled": strings.TrimSpace(r.cfg.GoogleClientID) != "" && strings.TrimSpace(r.cfg.GoogleClientSecret) != "",
		"timeZone":      serverTimeZone(),
	})
}

func serverTimeZone() string {
	if value := strings.TrimSpace(os.Getenv("TZ")); value != "" {
		if _, err := time.LoadLocation(value); err == nil {
			return value
		}
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if index := strings.LastIndex(target, "/zoneinfo/"); index >= 0 {
			value := target[index+len("/zoneinfo/"):]
			if _, err := time.LoadLocation(value); err == nil {
				return value
			}
		}
	}
	if raw, err := os.ReadFile("/etc/timezone"); err == nil {
		if value := strings.TrimSpace(string(raw)); value != "" {
			if _, err := time.LoadLocation(value); err == nil {
				return value
			}
		}
	}
	return "UTC"
}

func serverLocation() *time.Location {
	name := serverTimeZone()
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}

func (r *Router) registerUser(w http.ResponseWriter, req *http.Request) {
	var body struct{ Email, Password, CaptchaID, CaptchaCode string }
	if decodeJSON(req, &body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if !strings.Contains(email, "@") || len(email) > 254 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email_invalid", "message": "Enter a valid email address"})
		return
	}
	if !validUserPassword(body.Password) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password_invalid", "message": "Password must be 8 to 128 characters"})
		return
	}
	if !r.consumeCaptcha(body.CaptchaID, body.CaptchaCode) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "captcha_invalid", "message": "Verification code is incorrect"})
		return
	}
	u, err := r.store.CreateUser(store.DefaultTenantID, email, body.Password)
	if errors.Is(err, store.ErrUserExists) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "user_exists", "message": "An account with this email already exists"})
		return
	}
	if err != nil {
		r.logger.Error("failed to register user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "registration_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": u, "message": "Account created. You can now sign in."})
}

func (r *Router) forgotPassword(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	response := map[string]any{"message": "If the email address is registered, we will send password reset instructions."}
	if !validUserEmail(body.Email) {
		writeJSON(w, http.StatusOK, response)
		return
	}
	mailSettings, mailConfigured := r.effectiveEmailSettings()
	if !mailConfigured && !r.cfg.PasswordResetRevealToken {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "password_reset_unavailable",
			"message": "Email password recovery is not configured. Contact the system administrator.",
		})
		return
	}
	token, found, err := r.store.CreatePasswordResetToken(body.Email, 15*time.Minute)
	if err != nil {
		r.logger.Error("failed to create reset token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "reset_request_failed"})
		return
	}
	if found && mailConfigured {
		if err := r.sendPasswordResetEmail(mailSettings, strings.ToLower(strings.TrimSpace(body.Email)), token); err != nil {
			r.logger.Error("failed to send password reset email", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "reset_email_failed", "message": "Password reset email could not be sent. Please contact the administrator."})
			return
		}
	}
	if found && r.cfg.PasswordResetRevealToken {
		response["resetToken"] = token
		response["expiresInSeconds"] = 900
	}
	writeJSON(w, http.StatusOK, response)
}

func (r *Router) effectiveEmailSettings() (store.EmailSettings, bool) {
	if item, found, err := r.store.GetEmailSettings(); err == nil && found {
		return item, strings.TrimSpace(item.Host) != "" && strings.TrimSpace(item.SenderEmail) != ""
	}
	if strings.TrimSpace(r.cfg.SMTPHost) == "" {
		return store.EmailSettings{}, false
	}
	fromName, fromEmail := "HyperCDR", r.cfg.SMTPFrom
	if parsed, err := mail.ParseAddress(r.cfg.SMTPFrom); err == nil {
		fromName, fromEmail = parsed.Name, parsed.Address
	}
	password, _ := r.encryptSetting(r.cfg.SMTPPassword)
	port, _ := strconv.Atoi(r.cfg.SMTPPort)
	return store.EmailSettings{Enabled: true, Host: r.cfg.SMTPHost, Port: port, Security: "starttls", Username: r.cfg.SMTPUsername, PasswordCiphertext: password, SenderName: fromName, SenderEmail: fromEmail}, true
}
func (r *Router) sendPasswordResetEmail(settings store.EmailSettings, recipient, token string) error {
	base := strings.TrimRight(r.cfg.PublicBaseURL, "/")
	resetURL := base + "/?auth=reset&reset_token=" + url.QueryEscape(token)
	return r.sendConfiguredEmail(settings, recipient, "Reset your HyperCDR password", "Use this link within 15 minutes to reset your HyperCDR password:\r\n"+resetURL+"\r\n\r\nIf you did not request this, you can ignore this email.")
}
func (r *Router) sendConfiguredEmail(settings store.EmailSettings, recipient, subject, body string) error {
	password, err := r.decryptSetting(settings.PasswordCiphertext)
	if err != nil && settings.PasswordConfigured {
		return err
	}
	fromHeader := (&mail.Address{Name: settings.SenderName, Address: settings.SenderEmail}).String()
	message := []byte("From: " + fromHeader + "\r\nTo: " + recipient + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	address := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	var auth smtp.Auth
	if settings.Username != "" {
		auth = smtp.PlainAuth("", settings.Username, password, settings.Host)
	}
	if settings.Security == "starttls" {
		return smtp.SendMail(address, auth, settings.SenderEmail, []string{recipient}, message)
	}
	var conn net.Conn
	if settings.Security == "tls" {
		conn, err = tls.Dial("tcp", address, &tls.Config{ServerName: settings.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = net.DialTimeout("tcp", address, 15*time.Second)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, settings.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(settings.SenderEmail); err != nil {
		return err
	}
	if err = client.Rcpt(recipient); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(message); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (r *Router) resetPassword(w http.ResponseWriter, req *http.Request) {
	var body struct{ Token, Password string }
	if decodeJSON(req, &body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if !validUserPassword(body.Password) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "password_invalid", "message": "Password must be 8 to 128 characters"})
		return
	}
	_, err := r.store.ResetPassword(strings.TrimSpace(body.Token), body.Password)
	if errors.Is(err, store.ErrResetInvalid) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "reset_invalid", "message": "Reset link is invalid or expired"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "reset_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "Password updated. You can now sign in."})
}

func (r *Router) googleStart(w http.ResponseWriter, req *http.Request) {
	if r.cfg.GoogleClientID == "" || r.cfg.GoogleClientSecret == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "google_not_configured", "message": "Google sign-in is not configured"})
		return
	}
	state := store.NewPublicID() + store.NewPublicID()
	r.oauthMu.Lock()
	r.oauthStates[state] = time.Now().UTC().Add(10 * time.Minute)
	r.oauthMu.Unlock()
	callback := r.cfg.PublicBaseURL + "/api/v1/auth/google/callback"
	q := url.Values{"client_id": {r.cfg.GoogleClientID}, "redirect_uri": {callback}, "response_type": {"code"}, "scope": {"openid email profile"}, "state": {state}, "prompt": {"select_account"}}
	http.Redirect(w, req, "https://accounts.google.com/o/oauth2/v2/auth?"+q.Encode(), http.StatusFound)
}

func (r *Router) googleCallback(w http.ResponseWriter, req *http.Request) {
	state := req.URL.Query().Get("state")
	r.oauthMu.Lock()
	expiry, ok := r.oauthStates[state]
	delete(r.oauthStates, state)
	r.oauthMu.Unlock()
	if !ok || time.Now().UTC().After(expiry) {
		http.Redirect(w, req, "/?auth_error=google_state", http.StatusFound)
		return
	}
	callback := r.cfg.PublicBaseURL + "/api/v1/auth/google/callback"
	form := url.Values{"code": {req.URL.Query().Get("code")}, "client_id": {r.cfg.GoogleClientID}, "client_secret": {r.cfg.GoogleClientSecret}, "redirect_uri": {callback}, "grant_type": {"authorization_code"}}
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_exchange", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token) != nil {
		http.Redirect(w, req, "/?auth_error=google_exchange", http.StatusFound)
		return
	}
	userReq, _ := http.NewRequestWithContext(req.Context(), http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	userReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_profile", http.StatusFound)
		return
	}
	defer userResp.Body.Close()
	var profile struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if userResp.StatusCode != 200 || json.NewDecoder(io.LimitReader(userResp.Body, 1<<20)).Decode(&profile) != nil || !profile.EmailVerified {
		http.Redirect(w, req, "/?auth_error=google_profile", http.StatusFound)
		return
	}
	u, err := r.store.FindOrCreateGoogleUser(profile.Email)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_account", http.StatusFound)
		return
	}
	if u.Status != "active" {
		http.Redirect(w, req, "/?auth_error=google_account_disabled", http.StatusFound)
		return
	}
	session, err := r.store.CreatePlatformSession(u.ID, time.Hour)
	if err != nil {
		http.Redirect(w, req, "/?auth_error=google_session", http.StatusFound)
		return
	}
	payload, _ := json.Marshal(map[string]any{"user": u, "session": map[string]any{"token": session.Token, "expiresAt": session.ExpiresAt.Format(time.RFC3339)}})
	http.Redirect(w, req, "/#google_auth="+url.QueryEscape(base64.RawURLEncoding.EncodeToString(payload)), http.StatusFound)
}

func randomDigits(length int) (string, error) {
	var builder strings.Builder
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + n.Int64()))
	}
	return builder.String(), nil
}

func captchaImageDataURL(code string) string {
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="118" height="40" viewBox="0 0 118 40">
<defs><linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#f9fcff"/><stop offset="1" stop-color="#ddecf9"/></linearGradient></defs>
<rect width="118" height="40" rx="4" fill="url(#bg)"/>
<path d="M4 28 C24 6, 48 36, 72 14 S102 30, 114 10" fill="none" stroke="#0e7490" stroke-width="1.2" opacity=".45"/>
<path d="M2 12 C25 30, 54 4, 88 24 S108 18, 116 30" fill="none" stroke="#be185d" stroke-width="1" opacity=".28"/>
<g font-family="Georgia, 'Times New Roman', serif" font-size="24" font-weight="700">
<text x="16" y="28" fill="#1d4ed8" transform="rotate(-10 16 28)">%c</text>
<text x="40" y="27" fill="#0f766e" transform="rotate(7 40 27)">%c</text>
<text x="64" y="29" fill="#be123c" transform="rotate(-5 64 29)">%c</text>
<text x="88" y="27" fill="#4338ca" transform="rotate(9 88 27)">%c</text>
</g>
<g fill="#1e3a8a" opacity=".24"><circle cx="18" cy="11" r="1"/><circle cx="34" cy="33" r="1"/><circle cx="61" cy="9" r="1"/><circle cx="82" cy="34" r="1"/><circle cx="104" cy="16" r="1"/></g>
</svg>`, code[0], code[1], code[2], code[3])
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func validReleaseComponent(component string) bool {
	return component == "comm-agent" || component == "velero"
}

func (r *Router) componentTarget(ctx context.Context, component string) (store.ComponentRelease, error) {
	if active, ok, err := r.store.GetActiveComponentRelease(component); err != nil || ok {
		return active, err
	}
	image, version := strings.TrimSpace(r.cfg.AgentImage), imageVersion(r.cfg.AgentImage)
	if component == "velero" {
		image, version = strings.TrimSpace(r.cfg.VeleroImage), strings.TrimSpace(r.cfg.VeleroVersion)
	}
	if image == "" {
		return store.ComponentRelease{}, fmt.Errorf("%s target image is not configured", component)
	}
	digest, err := r.resolveImageDigest(ctx, image)
	if err != nil {
		return store.ComponentRelease{}, err
	}
	item, err := r.store.UpsertComponentRelease(store.ComponentReleaseInput{Component: component, Version: version, Image: image, ImageDigest: digest, Status: "active", ReleaseNotes: "Initialized from platform deployment configuration", PublishedBy: "system"})
	if err == nil {
		return item, nil
	}
	if active, ok, getErr := r.store.GetActiveComponentRelease(component); getErr == nil && ok {
		return active, nil
	}
	return store.ComponentRelease{}, err
}

func (r *Router) listComponentReleases(w http.ResponseWriter, req *http.Request) {
	component := strings.TrimSpace(req.URL.Query().Get("component"))
	if component != "" && !validReleaseComponent(component) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "component_invalid"})
		return
	}
	components := []string{component}
	if component == "" {
		components = []string{"comm-agent", "velero"}
	}
	for _, name := range components {
		if _, err := r.componentTarget(req.Context(), name); err != nil {
			r.logger.Warn("failed to initialize component release", "component", name, "error", err)
		}
	}
	items, err := r.store.ListComponentReleases(component)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_component_releases_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) createComponentRelease(w http.ResponseWriter, req *http.Request) {
	var body struct{ Component, Version, Image, ReleaseNotes string }
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	body.Component, body.Image = strings.TrimSpace(body.Component), strings.TrimSpace(body.Image)
	if !validReleaseComponent(body.Component) || body.Image == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "component_or_image_invalid"})
		return
	}
	digest, err := r.resolveImageDigest(req.Context(), body.Image)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "image_unavailable", "message": err.Error()})
		return
	}
	if strings.TrimSpace(body.Version) == "" {
		body.Version = imageVersion(body.Image)
	}
	item, err := r.store.UpsertComponentRelease(store.ComponentReleaseInput{Component: body.Component, Version: strings.TrimSpace(body.Version), Image: body.Image, ImageDigest: digest, Status: "candidate", ReleaseNotes: strings.TrimSpace(body.ReleaseNotes)})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_component_release_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (r *Router) activateComponentRelease(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	items, err := r.store.ListComponentReleases("")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_component_releases_failed"})
		return
	}
	var selected store.ComponentRelease
	for _, item := range items {
		if item.ID == id {
			selected = item
			break
		}
	}
	if selected.ID == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "component_release_not_found"})
		return
	}
	if active, found, activeErr := r.store.GetActiveComponentRelease(selected.Component); activeErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "active_component_release_failed"})
		return
	} else if found {
		if comparison, comparable := compareNumericVersions(selected.Version, active.Version); comparable && comparison < 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "component_downgrade_not_allowed", "message": "The selected version is older than the current target version."})
			return
		}
	}
	digest, err := r.resolveImageDigest(req.Context(), selected.Image)
	if err != nil || digest != selected.ImageDigest {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "release_image_changed", "message": "The image is unavailable or its digest no longer matches the validated candidate."})
		return
	}
	item, ok, err := r.store.ActivateComponentRelease(id, "admin")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "activate_component_release_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "component_release_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (r *Router) discoverComponentReleases(w http.ResponseWriter, req *http.Request) {
	component := strings.TrimSpace(req.URL.Query().Get("component"))
	if !validReleaseComponent(component) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "component_invalid"})
		return
	}
	image := r.cfg.AgentImage
	if component == "velero" {
		image = r.cfg.VeleroImage
	}
	registry, repository, _, ok := splitContainerImage(image)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "registry_image_invalid"})
		return
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // nolint:gosec
	tokenURL := fmt.Sprintf("https://%s/service/token?service=harbor-registry&scope=repository:%s:pull", registry, repository)
	tokenResp, err := client.Get(tokenURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "registry_unavailable"})
		return
	}
	defer tokenResp.Body.Close()
	var tokenBody struct {
		Token string `json:"token"`
	}
	if tokenResp.StatusCode >= 300 || json.NewDecoder(tokenResp.Body).Decode(&tokenBody) != nil || tokenBody.Token == "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "registry_token_failed"})
		return
	}
	tagsReq, _ := http.NewRequestWithContext(req.Context(), http.MethodGet, fmt.Sprintf("https://%s/v2/%s/tags/list", registry, repository), nil)
	tagsReq.Header.Set("Authorization", "Bearer "+tokenBody.Token)
	tagsResp, err := client.Do(tagsReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "registry_unavailable"})
		return
	}
	defer tagsResp.Body.Close()
	var result struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if tagsResp.StatusCode >= 300 || json.NewDecoder(tagsResp.Body).Decode(&result) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "registry_tags_failed"})
		return
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result.Tags)))
	writeJSON(w, http.StatusOK, map[string]any{"component": component, "repository": result.Name, "registry": registry, "tags": result.Tags})
}

func (r *Router) listClusters(w http.ResponseWriter, req *http.Request) {
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("failed to list clusters", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	visibleClusters := clusters[:0]
	for _, item := range clusters {
		if tenantVisible(req, item.TenantID) {
			visibleClusters = append(visibleClusters, item)
		}
	}
	clusters = visibleClusters
	agentTarget, agentTargetErr := r.componentTarget(req.Context(), "comm-agent")
	if agentTargetErr != nil {
		r.logger.Warn("failed to load comm-agent target release", "error", agentTargetErr)
	}
	veleroTarget, veleroTargetErr := r.componentTarget(req.Context(), "velero")
	if veleroTargetErr != nil {
		r.logger.Warn("failed to load velero target release", "error", veleroTargetErr)
	}
	latestAgentVersion, latestAgentImage, latestAgentDigest := agentTarget.Version, agentTarget.Image, agentTarget.ImageDigest
	latestVeleroImage, latestVeleroDigest := veleroTarget.Image, veleroTarget.ImageDigest
	upgradeTasks, taskErr := r.store.ListTasks("")
	if taskErr != nil {
		r.logger.Warn("failed to load agent upgrade status", "error", taskErr)
	}
	for i := range clusters {
		if r.hub.has(clusters[i].ID) {
			clusters[i].ConnectionStatus = "online"
		} else {
			clusters[i].ConnectionStatus = "offline"
		}
		clusters[i].LatestAgentVersion = latestAgentVersion
		clusters[i].LatestAgentImage = latestAgentImage
		clusters[i].LatestAgentImageDigest = latestAgentDigest
		agentDigestMismatch := clusters[i].AgentImageDigest != "" && latestAgentDigest != "" && clusters[i].AgentImageDigest != latestAgentDigest
		clusters[i].AgentUpgradeAvailable = upgradeTargetIsNewer(clusters[i].AgentVersion, latestAgentVersion, agentDigestMismatch)
		clusters[i].LatestVeleroVersion = veleroTarget.Version
		clusters[i].LatestVeleroImage = latestVeleroImage
		clusters[i].LatestVeleroImageDigest = latestVeleroDigest
		veleroRuntimeReported := clusters[i].VeleroImageDigest != "" && clusters[i].VeleroNodeAgentImageDigest != ""
		veleroDigestMismatch := latestVeleroDigest != "" && veleroRuntimeReported && (clusters[i].VeleroImageDigest != latestVeleroDigest || clusters[i].VeleroNodeAgentImageDigest != latestVeleroDigest)
		clusters[i].VeleroUpgradeAvailable = upgradeTargetIsNewer(clusters[i].VeleroVersion, veleroTarget.Version, veleroDigestMismatch)
		for _, task := range upgradeTasks {
			if task.ClusterID != clusters[i].ID || isTerminalTaskStatus(task.Status) {
				continue
			}
			if task.Type == "agent-upgrade" {
				clusters[i].AgentUpgradeStatus = "upgrading"
				clusters[i].AgentUpgradeProgress = task.Progress
			}
			if task.Type == "velero-upgrade" {
				clusters[i].VeleroUpgradeStatus = "upgrading"
				clusters[i].VeleroUpgradeProgress = task.Progress
			}
		}
		if clusters[i].AgentUpgradeAvailable && clusters[i].AgentUpgradeStatus == "" {
			clusters[i].AgentUpgradeStatus = "available"
		}
		if clusters[i].VeleroUpgradeAvailable && clusters[i].VeleroUpgradeStatus == "" {
			clusters[i].VeleroUpgradeStatus = "available"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": nonNilSlice(clusters),
	})
}

func upgradeTargetIsNewer(currentVersion, targetVersion string, digestMismatch bool) bool {
	comparison, comparable := compareNumericVersions(targetVersion, currentVersion)
	if !comparable {
		return digestMismatch
	}
	return comparison > 0 || comparison == 0 && digestMismatch
}

func compareNumericVersions(left, right string) (int, bool) {
	leftParts := numericVersionParts(left)
	rightParts := numericVersionParts(right)
	if len(leftParts) == 0 || len(rightParts) == 0 {
		return 0, false
	}
	length := max(len(leftParts), len(rightParts))
	for i := 0; i < length; i++ {
		leftPart, rightPart := 0, 0
		if i < len(leftParts) {
			leftPart = leftParts[i]
		}
		if i < len(rightParts) {
			rightPart = rightParts[i]
		}
		if leftPart < rightPart {
			return -1, true
		}
		if leftPart > rightPart {
			return 1, true
		}
	}
	return 0, true
}

func numericVersionParts(version string) []int {
	var parts []int
	for index := 0; index < len(version); {
		if version[index] < '0' || version[index] > '9' {
			index++
			continue
		}
		value := 0
		for index < len(version) && version[index] >= '0' && version[index] <= '9' {
			value = value*10 + int(version[index]-'0')
			index++
		}
		parts = append(parts, value)
	}
	return parts
}

func isTerminalTaskStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "failed", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func (r *Router) completeAgentUpgradeAfterHeartbeat(cluster store.Cluster) {
	tasks, err := r.store.ListTasks(cluster.ID)
	if err != nil {
		r.logger.Warn("failed to reconcile agent upgrade after heartbeat", "cluster_id", cluster.ID, "error", err)
		return
	}
	for _, task := range tasks {
		if task.Type != "agent-upgrade" || isTerminalTaskStatus(task.Status) {
			continue
		}
		expectedDigest := strings.TrimSpace(stringPayload(task.Payload, "expectedDigest"))
		expectedImage := strings.TrimSpace(stringPayload(task.Payload, "image"))
		expectedVersion := strings.TrimSpace(stringPayload(task.Payload, "version"))
		digestMatches := expectedDigest != "" && strings.TrimSpace(cluster.AgentImageDigest) == expectedDigest
		identityMatches := expectedDigest == "" && expectedImage != "" && cluster.AgentImage == expectedImage && (expectedVersion == "" || cluster.AgentVersion == expectedVersion)
		if !digestMatches && !identityMatches {
			continue
		}
		if _, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID: task.ID, Status: "succeeded", Progress: 100, MarkDone: true,
		}); err != nil {
			r.logger.Warn("failed to complete verified agent upgrade", "cluster_id", cluster.ID, "task_id", task.ID, "error", err)
			continue
		}
		_ = r.addTaskEventIfChanged(store.TaskEventInput{
			TaskID: task.ID, Level: "info", Reason: "completed",
			Message: "new agent reconnected and reported the expected image digest",
			Payload: map[string]any{"image": cluster.AgentImage, "digest": cluster.AgentImageDigest, "version": cluster.AgentVersion},
		})
	}
}

func (r *Router) completeVeleroUpgradeAfterHeartbeat(cluster store.Cluster) {
	tasks, err := r.store.ListTasks(cluster.ID)
	if err != nil {
		return
	}
	for _, task := range tasks {
		if task.Type != "velero-upgrade" || isTerminalTaskStatus(task.Status) {
			continue
		}
		expected := strings.TrimSpace(stringPayload(task.Payload, "expectedDigest"))
		allNodesReady := cluster.VeleroNodeAgentDesired > 0 && cluster.VeleroNodeAgentReady == cluster.VeleroNodeAgentDesired
		if expected == "" || cluster.VeleroImageDigest != expected || cluster.VeleroNodeAgentImageDigest != expected || !cluster.VeleroServerReady || !allNodesReady {
			continue
		}
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "succeeded", Progress: 100, MarkDone: true})
		_ = r.addTaskEventIfChanged(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "completed", Message: "velero server and all scheduled node agents report the expected image digest"})
	}
}

func (r *Router) resolveImageDigest(ctx context.Context, image string) (string, error) {
	image = strings.TrimSpace(image)
	now := time.Now()
	r.imageDigestMu.Lock()
	if r.imageDigests == nil {
		r.imageDigests = map[string]imageDigestCacheEntry{}
	}
	cached, ok := r.imageDigests[image]
	if ok && cached.Digest != "" && cached.ExpiresAt.After(now) {
		r.imageDigestMu.Unlock()
		return cached.Digest, nil
	}
	r.imageDigestMu.Unlock()

	registry, repository, reference, ok := splitContainerImage(image)
	if !ok {
		return "", fmt.Errorf("invalid image reference %q", image)
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // nolint:gosec -- lab Harbor commonly uses an internal CA.
	}
	tokenURL := fmt.Sprintf("https://%s/service/token?service=harbor-registry&scope=repository:%s:pull", registry, repository)
	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return "", err
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode >= 300 {
		return "", fmt.Errorf("registry token request failed: %s", tokenResp.Status)
	}
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenBody); err != nil {
		return "", err
	}
	if tokenBody.Token == "" {
		return "", errors.New("registry token is empty")
	}
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repository, reference)
	manifestReq, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
	if err != nil {
		return "", err
	}
	manifestReq.Header.Set("Authorization", "Bearer "+tokenBody.Token)
	manifestReq.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json")
	manifestResp, err := client.Do(manifestReq)
	if err != nil {
		return "", err
	}
	defer manifestResp.Body.Close()
	if manifestResp.StatusCode >= 300 {
		return "", fmt.Errorf("registry manifest request failed: %s", manifestResp.Status)
	}
	digest := strings.TrimSpace(manifestResp.Header.Get("Docker-Content-Digest"))
	if digest == "" {
		return "", errors.New("registry manifest digest is empty")
	}
	r.imageDigestMu.Lock()
	r.imageDigests[image] = imageDigestCacheEntry{Digest: digest, ExpiresAt: now.Add(imageDigestTTL)}
	r.imageDigestMu.Unlock()
	return digest, nil
}

func splitContainerImage(image string) (registry string, repository string, reference string, ok bool) {
	image = strings.TrimSpace(image)
	if image == "" {
		return "", "", "", false
	}
	if strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		return "", "", "", false
	}
	firstSlash := strings.IndexByte(image, '/')
	if firstSlash <= 0 || firstSlash == len(image)-1 {
		return "", "", "", false
	}
	registry = image[:firstSlash]
	remainder := image[firstSlash+1:]
	reference = "latest"
	if at := strings.LastIndex(remainder, "@"); at >= 0 {
		repository = remainder[:at]
		reference = remainder[at+1:]
	} else if colon := strings.LastIndex(remainder, ":"); colon >= 0 && colon > strings.LastIndex(remainder, "/") {
		repository = remainder[:colon]
		reference = remainder[colon+1:]
	} else {
		repository = remainder
	}
	return registry, repository, reference, registry != "" && repository != "" && reference != ""
}

func imageVersion(image string) string {
	_, _, reference, ok := splitContainerImage(image)
	if !ok {
		return strings.TrimSpace(image)
	}
	if strings.HasPrefix(reference, "sha256:") && len(reference) > 19 {
		return "sha256:" + reference[7:19]
	}
	return reference
}

func (r *Router) updateCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	var input store.ClusterUpdateInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	input.ID = clusterID
	cluster, ok, err := r.store.UpdateCluster(input)
	if err != nil {
		r.logger.Error("failed to update cluster", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_cluster_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (r *Router) setDefaultCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	cluster, ok, err := r.store.SetDefaultCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to set default cluster", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "set_default_cluster_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (r *Router) requestClusterInventory(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	var body struct {
		RequestID                  string `json:"requestId"`
		Scope                      string `json:"scope"`
		Namespace                  string `json:"namespace"`
		IncludeDetails             bool   `json:"includeDetails"`
		Reason                     string `json:"reason"`
		IncludeRecentVeleroObjects bool   `json:"includeRecentVeleroObjects"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("failed to list clusters for inventory request", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	found := false
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	if body.RequestID == "" {
		body.RequestID = store.NewPublicID()
	}
	if body.Scope == "" {
		body.Scope = "summary"
	}
	if body.Reason == "" {
		body.Reason = "user_refresh"
	}
	now := time.Now().UTC()
	conn, ok := r.hub.get(clusterID)
	if !ok {
		status := inventoryRequestStatus{
			RequestID:   body.RequestID,
			ClusterID:   clusterID,
			Scope:       body.Scope,
			Namespace:   body.Namespace,
			Status:      "failed",
			ErrorCode:   "AGENT_OFFLINE",
			Message:     "agent is not connected; inventory request was not sent",
			CreatedAt:   now,
			CompletedAt: now,
		}
		r.setInventoryRequestStatus(status)
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "agent_offline",
			"message":   "agent is not connected; inventory request was not sent",
			"requestId": body.RequestID,
		})
		return
	}
	messageID := store.NewPublicID()
	r.setInventoryRequestStatus(inventoryRequestStatus{
		RequestID: body.RequestID,
		MessageID: messageID,
		ClusterID: clusterID,
		Scope:     body.Scope,
		Namespace: body.Namespace,
		Status:    "pending",
		CreatedAt: now,
	})
	message := protocol.Message[protocol.InventoryRequestPayload]{
		Version:     protocol.Version,
		MessageID:   messageID,
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformInventoryRequest,
		ClusterID:   clusterID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.InventoryRequestPayload{
			RequestID:                  body.RequestID,
			Scope:                      body.Scope,
			Namespace:                  body.Namespace,
			IncludeDetails:             body.IncludeDetails,
			Reason:                     body.Reason,
			IncludeRecentVeleroObjects: body.IncludeRecentVeleroObjects,
		},
	}
	if err := conn.WriteJSON(message); err != nil {
		r.logger.Error("failed to dispatch inventory request", "cluster_id", clusterID, "request_id", body.RequestID, "error", err)
		failed := inventoryRequestStatus{
			RequestID:   body.RequestID,
			MessageID:   messageID,
			ClusterID:   clusterID,
			Scope:       body.Scope,
			Namespace:   body.Namespace,
			Status:      "failed",
			ErrorCode:   "DISPATCH_FAILED",
			Message:     err.Error(),
			CreatedAt:   now,
			CompletedAt: time.Now().UTC(),
		}
		r.setInventoryRequestStatus(failed)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":    "queued_failed",
			"warning":   "inventory request could not be sent: " + err.Error(),
			"messageId": messageID,
			"requestId": body.RequestID,
		})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":    "sent",
		"messageId": messageID,
		"requestId": body.RequestID,
		"scope":     body.Scope,
		"namespace": body.Namespace,
	})
}

func (r *Router) getClusterInventoryRequest(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	requestID := req.PathValue("requestId")
	if clusterID == "" || requestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id_required"})
		return
	}
	status, ok := r.getInventoryRequestStatus(requestID)
	if !ok || status.ClusterID != clusterID {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "inventory_request_not_found"})
		return
	}
	if status.Status == "pending" && time.Since(status.CreatedAt) > 20*time.Second {
		status.Status = "timeout"
		status.ErrorCode = "INVENTORY_REQUEST_TIMEOUT"
		status.Message = "agent did not respond before timeout"
		status.CompletedAt = time.Now().UTC()
		r.setInventoryRequestStatus(status)
	}
	writeJSON(w, http.StatusOK, status)
}

func (r *Router) setInventoryRequestStatus(status inventoryRequestStatus) {
	if status.RequestID == "" {
		return
	}
	r.inventoryMu.Lock()
	defer r.inventoryMu.Unlock()
	r.inventory[status.RequestID] = status
}

func (r *Router) getInventoryRequestStatus(requestID string) (inventoryRequestStatus, bool) {
	r.inventoryMu.Lock()
	defer r.inventoryMu.Unlock()
	status, ok := r.inventory[requestID]
	return status, ok
}

func (r *Router) completeInventoryRequest(clusterID string, payload protocol.InventoryReportPayload) {
	if payload.RequestID == "" {
		return
	}
	status, ok := r.getInventoryRequestStatus(payload.RequestID)
	if !ok || status.ClusterID != clusterID {
		return
	}
	status.Status = "succeeded"
	status.Message = "inventory updated"
	status.CompletedAt = time.Now().UTC()
	r.setInventoryRequestStatus(status)
}

func (r *Router) failInventoryRequest(clusterID string, payload protocol.MessageErrorPayload) {
	if payload.RequestID == "" {
		return
	}
	status, ok := r.getInventoryRequestStatus(payload.RequestID)
	if !ok || status.ClusterID != clusterID {
		return
	}
	status.Status = "failed"
	status.ErrorCode = payload.ErrorCode
	status.Message = payload.Message
	status.CompletedAt = time.Now().UTC()
	r.setInventoryRequestStatus(status)
}

func (r *Router) deleteCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if req.URL.Query().Get("force") != "true" {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "cluster_unregister_required",
			"message": "use POST /api/v1/clusters/{id}/unregister for agent self-uninstall, or DELETE with force=true for platform-only cleanup",
		})
		return
	}
	r.hub.close(clusterID)
	ok, err := r.store.DeleteCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to delete cluster", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_cluster_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "clusterId": clusterID})
}

func (r *Router) forceCleanupCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if !r.clusterExists(clusterID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	audit, err := r.auditClusterUnregister(clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "force_remove_precheck_failed", "message": err.Error()})
		return
	}
	if audit.TargetPlanCount > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "cluster_is_dr_target", "message": "This cluster is still used as a DR target. Change or remove those DR configurations before Force Remove.", "precheck": audit})
		return
	}
	r.hub.close(clusterID)
	ok, err := r.store.DeleteCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to force cleanup cluster platform records", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_cluster_failed", "message": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "cleaned",
		"clusterId": clusterID,
		"warning":   "Platform records were removed. Cluster-side resources and backup objects were not deleted and may require manual cleanup.",
	})
}

type unregisterAudit struct {
	ClusterID            string   `json:"clusterId"`
	AgentOnline          bool     `json:"agentOnline"`
	DefaultCluster       bool     `json:"defaultCluster"`
	SourcePlanCount      int      `json:"sourcePlanCount"`
	TargetPlanCount      int      `json:"targetPlanCount"`
	RestorePointCount    int      `json:"restorePointCount"`
	StorageRepositoryIDs []string `json:"storageRepositoryIds"`
	ActiveTaskCount      int      `json:"activeTaskCount"`
	ActiveTaskTypes      []string `json:"activeTaskTypes"`
	UnregisterActive     bool     `json:"unregisterActive"`
	ObjectStorageNeeded  bool     `json:"objectStorageNeeded"`
	Stage                string   `json:"stage"`
	Allowed              bool     `json:"allowed"`
	Blockers             []string `json:"blockers"`
}

func (r *Router) auditClusterUnregister(clusterID string) (unregisterAudit, error) {
	audit := unregisterAudit{
		ClusterID:            clusterID,
		StorageRepositoryIDs: []string{},
		ActiveTaskTypes:      []string{},
		Blockers:             []string{},
		Allowed:              true,
		Stage:                "registered",
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		return audit, err
	}
	found := false
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			found = true
			audit.DefaultCluster = cluster.IsDefault
			break
		}
	}
	if !found {
		return audit, errors.New("cluster not found")
	}
	_, audit.AgentOnline = r.hub.get(clusterID)
	plans, err := r.store.ListProtectionPlans("")
	if err != nil {
		return audit, err
	}
	repositoryIDs := map[string]struct{}{}
	for _, plan := range plans {
		if plan.SourceClusterID == clusterID {
			audit.SourcePlanCount++
			if plan.StorageRepoID != "" {
				repositoryIDs[plan.StorageRepoID] = struct{}{}
			}
		}
		if plan.TargetClusterID == clusterID {
			audit.TargetPlanCount++
		}
	}
	repositories, err := r.store.ListStorageRepositories()
	if err != nil {
		return audit, err
	}
	for _, repository := range repositories {
		if _, ok, bindingErr := r.store.GetClusterStorageBinding(clusterID, repository.ID, clusterID); bindingErr != nil {
			return audit, bindingErr
		} else if ok {
			repositoryIDs[repository.ID] = struct{}{}
		}
	}
	points, err := r.store.ListRestorePoints(store.RestorePointFilter{ClusterID: clusterID, IncludeDeleted: true})
	if err != nil {
		return audit, err
	}
	for _, point := range points {
		if strings.EqualFold(point.Status, "deleted") {
			continue
		}
		audit.RestorePointCount++
		if point.StorageRepoID != "" {
			repositoryIDs[point.StorageRepoID] = struct{}{}
		}
	}
	for id := range repositoryIDs {
		audit.StorageRepositoryIDs = append(audit.StorageRepositoryIDs, id)
	}
	sort.Strings(audit.StorageRepositoryIDs)
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return audit, err
	}
	types := map[string]struct{}{}
	for _, task := range tasks {
		if !isActiveTaskStatus(task.Status) {
			continue
		}
		if task.Type == "unregister" {
			audit.UnregisterActive = true
		} else {
			audit.ActiveTaskCount++
			types[task.Type] = struct{}{}
		}
	}
	for taskType := range types {
		audit.ActiveTaskTypes = append(audit.ActiveTaskTypes, taskType)
	}
	sort.Strings(audit.ActiveTaskTypes)
	audit.ObjectStorageNeeded = len(audit.StorageRepositoryIDs) > 0
	switch {
	case audit.ActiveTaskCount > 0:
		audit.Stage = "active_tasks"
	case audit.TargetPlanCount > 0:
		audit.Stage = "target_in_use"
	case audit.RestorePointCount > 0:
		audit.Stage = "protected_with_restore_points"
	case audit.SourcePlanCount > 0:
		audit.Stage = "configured_without_restore_points"
	default:
		audit.Stage = "registered_without_dr"
	}
	if audit.UnregisterActive {
		audit.Blockers = append(audit.Blockers, "An unregister task is already active for this cluster.")
	}
	if !audit.AgentOnline {
		audit.Blockers = append(audit.Blockers, "The cluster agent is offline. Reconnect it for normal unregister, or use Force Remove if the cluster is permanently unavailable.")
	}
	if audit.ActiveTaskCount > 0 {
		audit.Blockers = append(audit.Blockers, "Wait for active cluster tasks to finish before unregistering.")
	}
	if audit.TargetPlanCount > 0 {
		audit.Blockers = append(audit.Blockers, "This cluster is used as a DR target. Change or remove those DR configurations first.")
	}
	audit.Allowed = len(audit.Blockers) == 0
	return audit, nil
}

func (r *Router) precheckUnregisterCluster(w http.ResponseWriter, req *http.Request) {
	audit, err := r.auditClusterUnregister(req.PathValue("id"))
	if err != nil {
		if err.Error() == "cluster not found" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "unregister_precheck_failed", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, audit)
}

func (r *Router) unregisterCluster(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	audit, err := r.auditClusterUnregister(clusterID)
	if err != nil {
		if err.Error() == "cluster not found" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "unregister_precheck_failed", "message": err.Error()})
		return
	}
	if !audit.Allowed {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "unregister_precheck_blocked", "message": strings.Join(audit.Blockers, " "), "precheck": audit})
		return
	}

	var body struct {
		DeleteVelero     *bool  `json:"deleteVelero"`
		DeleteNamespace  *bool  `json:"deleteNamespace"`
		DeleteBackupData bool   `json:"deleteBackupData"`
		Reason           string `json:"reason"`
	}
	if req.Body != nil {
		if err := decodeJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
			return
		}
	}
	deleteVelero := true
	if body.DeleteVelero != nil {
		deleteVelero = *body.DeleteVelero
	}
	deleteNamespace := true
	if body.DeleteNamespace != nil {
		deleteNamespace = *body.DeleteNamespace
	}
	if audit.RestorePointCount > 0 && !body.DeleteBackupData {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "backup_data_decision_required",
			"message":  "This cluster has restore points. Confirm deletion of backup data, or remove/archive its DR configurations before unregistering.",
			"precheck": audit,
		})
		return
	}
	cleanupObjectStorage := audit.ObjectStorageNeeded && (audit.RestorePointCount == 0 || body.DeleteBackupData)
	namespace := r.agentNamespace()
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID: clusterID,
		Type:      "unregister",
		Status:    "queued",
		CommandID: commandID,
		Payload: map[string]any{
			"requestedBy":          requestActor(req),
			"clusterId":            clusterID,
			"namespace":            namespace,
			"deleteVelero":         deleteVelero,
			"deleteNamespace":      deleteNamespace,
			"deleteBackupData":     body.DeleteBackupData,
			"cleanupObjectStorage": cleanupObjectStorage,
			"storageRepositoryIds": audit.StorageRepositoryIDs,
			"unregisterStage":      "prechecking",
			"reason":               body.Reason,
		},
	})
	if err != nil {
		r.logger.Error("failed to create unregister task", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	if cleanupObjectStorage {
		go r.cleanupAndDispatchUnregister(task, audit.StorageRepositoryIDs)
		writeJSON(w, http.StatusAccepted, task)
		return
	}

	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; unregister will be dispatched after reconnect",
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "agent is offline; unregister task remains queued",
		})
		return
	}

	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch unregister task", "cluster_id", clusterID, "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "unregister task created but dispatch failed",
		})
		return
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 20,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "unregister task dispatched to agent",
	})
	writeJSON(w, http.StatusAccepted, task)
}

func (r *Router) cleanupAndDispatchUnregister(task store.Task, repositoryIDs []string) {
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "running", Progress: 10})
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "cleaning_object_storage", Message: "cleaning backup data before cluster-side uninstall"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cleanupResults, cleanupErr := r.cleanupClusterObjectStorageRepositories(ctx, task.ClusterID, repositoryIDs)
	cancel()
	if cleanupErr != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "failed", Progress: 10, ErrorCode: "OBJECT_STORAGE_CLEANUP_FAILED", ErrorMessage: cleanupErr.Error(), MarkDone: true})
		_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "error", Reason: "OBJECT_STORAGE_CLEANUP_FAILED", Message: cleanupErr.Error(), Payload: map[string]any{"cleanupResult": cleanupResults}})
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "object_storage_cleaned", Message: "backup data cleanup completed", Payload: map[string]any{"cleanupResult": cleanupResults}})
	conn, ok := r.hub.get(task.ClusterID)
	if !ok || conn == nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "queued", Progress: 10, ErrorCode: "AGENT_OFFLINE", ErrorMessage: "agent disconnected after precheck; unregister will be dispatched after reconnect"})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "queued", Progress: 10, ErrorCode: "DISPATCH_FAILED", ErrorMessage: err.Error()})
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "dispatched", Progress: 20})
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "dispatched", Message: "unregister task dispatched to agent"})
}

func (r *Router) upgradeClusterAgent(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	if clusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if !r.clusterExists(clusterID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	target, targetErr := r.componentTarget(req.Context(), "comm-agent")
	if targetErr != nil || target.Image == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "agent_image_not_configured", "message": "Target agent image is not configured"})
		return
	}
	targetImage, targetDigest := target.Image, target.ImageDigest
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID && cluster.AgentImageDigest != "" && cluster.AgentImageDigest == targetDigest {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "agent_already_current", "message": "Comm Agent already uses the target image."})
			return
		}
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_tasks_failed"})
		return
	}
	blockedTypes := map[string]bool{"backup": true, "restore": true, "drill": true, "takeover": true, "failback": true, "retention-cleanup": true, "protection-cleanup": true, "agent-upgrade": true, "velero-upgrade": true}
	for _, task := range tasks {
		if blockedTypes[task.Type] && !isTerminalTaskStatus(task.Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cluster_task_active", "message": "Wait for active backup, restore, drill, cleanup, or upgrade tasks to finish before upgrading Comm Agent."})
			return
		}
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "agent_offline", "message": "Cluster agent is offline. Reconnect the agent before upgrading."})
		return
	}
	commandID := store.NewPublicID()
	namespace := r.agentNamespace()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID: clusterID,
		Type:      "agent-upgrade",
		Status:    "queued",
		CommandID: commandID,
		Payload: map[string]any{
			"requestedBy":       requestActor(req),
			"clusterId":         clusterID,
			"namespace":         namespace,
			"image":             targetImage,
			"version":           target.Version,
			"releaseId":         target.ID,
			"expectedDigest":    targetDigest,
			"deploymentName":    "hypercdr-comm-agent",
			"containerName":     "comm-agent",
			"rolloutAnnotation": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		r.logger.Error("failed to create agent upgrade task", "cluster_id", clusterID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch agent upgrade task", "cluster_id", clusterID, "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "agent upgrade task created but dispatch failed",
		})
		return
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "agent upgrade task dispatched",
	})
	writeJSON(w, http.StatusAccepted, task)
}

func (r *Router) upgradeClusterVelero(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	clusters, listErr := r.store.ListClusters()
	if listErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_clusters_failed"})
		return
	}
	var cluster store.Cluster
	found := false
	for _, item := range clusters {
		if item.ID == clusterID {
			cluster = item
			found = true
			break
		}
	}
	if clusterID == "" || !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
		return
	}
	if cluster.VeleroImageDigest == "" || cluster.VeleroNodeAgentImageDigest == "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "velero_runtime_unreported", "message": "Upgrade the comm-agent first so it can report the Velero server and node-agent image digests."})
		return
	}
	target, targetErr := r.componentTarget(req.Context(), "velero")
	if targetErr != nil || target.Image == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "velero_image_not_configured"})
		return
	}
	targetImage, targetDigest := target.Image, target.ImageDigest
	if cluster.VeleroImageDigest == targetDigest && cluster.VeleroNodeAgentImageDigest == targetDigest && cluster.VeleroServerReady && cluster.VeleroNodeAgentDesired > 0 && cluster.VeleroNodeAgentReady == cluster.VeleroNodeAgentDesired {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "velero_already_current", "message": "Velero server and all node agents already use the target image."})
		return
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_tasks_failed"})
		return
	}
	blockedTypes := map[string]bool{"backup": true, "restore": true, "drill": true, "takeover": true, "failback": true, "retention-cleanup": true, "protection-cleanup": true, "agent-upgrade": true, "velero-upgrade": true}
	for _, task := range tasks {
		if blockedTypes[task.Type] && !isTerminalTaskStatus(task.Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "cluster_task_active", "message": "Wait for active backup, restore, drill, cleanup, or upgrade tasks to finish before upgrading Velero."})
			return
		}
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "agent_offline", "message": "Cluster agent is offline. Reconnect the agent before upgrading Velero."})
		return
	}
	task, err := r.store.CreateTask(store.TaskInput{ClusterID: clusterID, Type: "velero-upgrade", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{
		"requestedBy": requestActor(req),
		"clusterId":   clusterID, "namespace": r.agentNamespace(), "image": targetImage, "version": target.Version, "releaseId": target.ID,
		"expectedDigest": targetDigest, "deploymentName": "velero", "daemonSetName": "node-agent",
		"awsPluginImage": r.cfg.VeleroAWSPlugin, "azurePluginImage": r.cfg.VeleroAzurePlugin, "gcpPluginImage": r.cfg.VeleroGCPPlugin,
	}})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "queued", ErrorCode: "DISPATCH_FAILED", ErrorMessage: err.Error()})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "warning": "Velero upgrade task created but dispatch failed"})
		return
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "dispatched", Progress: 0})
	_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "dispatched", Message: "Velero upgrade task dispatched to cluster agent"})
	writeJSON(w, http.StatusAccepted, task)
}

func (r *Router) clusterExists(clusterID string) bool {
	clusters, err := r.store.ListClusters()
	if err != nil {
		r.logger.Error("failed to verify cluster exists", "cluster_id", clusterID, "error", err)
		return false
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return true
		}
	}
	return false
}

func (r *Router) agentNamespace() string {
	if r.cfg.AgentNamespace != "" {
		return r.cfg.AgentNamespace
	}
	return "hypercdr-agent"
}

func (r *Router) listApplications(w http.ResponseWriter, req *http.Request) {
	apps, err := r.store.ListApplications(req.URL.Query().Get("clusterId"))
	if err != nil {
		r.logger.Error("failed to list applications", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_applications_failed"})
		return
	}
	clusters, _ := r.store.ListClusters()
	allowed := map[string]bool{}
	for _, item := range clusters {
		if tenantVisible(req, item.TenantID) {
			allowed[item.ID] = true
		}
	}
	visibleApps := apps[:0]
	for _, item := range apps {
		if allowed[item.ClusterID] {
			visibleApps = append(visibleApps, item)
		}
	}
	apps = visibleApps
	writeJSON(w, http.StatusOK, map[string]any{
		"items": nonNilSlice(apps),
	})
}

func (r *Router) updateApplication(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}
	var body struct {
		ProtectionStatus string `json:"protectionStatus"`
	}
	if req.Body != nil {
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body"})
			return
		}
	}
	if blocks, message := r.applicationDRSupportBlock(id, body.ProtectionStatus); blocks {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "application_dr_unsupported",
			"message": message,
		})
		return
	}
	app, ok, err := r.store.UpdateApplication(store.ApplicationUpdateInput{
		ID:               id,
		ProtectionStatus: body.ProtectionStatus,
	})
	if err != nil {
		r.logger.Error("failed to update application", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "application_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (r *Router) listTags(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListTags()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_tags_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	writeJSON(w, 200, map[string]any{"items": nonNilSlice(items)})
}
func (r *Router) createTag(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, 400, map[string]any{"error": "tag_name_required"})
		return
	}
	tenantID := store.DefaultTenantID
	if actor, ok := requestUser(req); ok {
		tenantID = actor.TenantID
	}
	tag, err := r.store.CreateTag(tenantID, body.Name)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "tag_name_exists", "message": "Tag name already exists."})
		return
	}
	writeJSON(w, 201, tag)
}
func (r *Router) updateTag(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, 400, map[string]any{"error": "tag_name_required"})
		return
	}
	tag, ok, err := r.store.UpdateTag(req.PathValue("id"), body.Name)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": "tag_name_exists"})
		return
	}
	if !ok {
		writeJSON(w, 404, map[string]any{"error": "tag_not_found"})
		return
	}
	writeJSON(w, 200, tag)
}
func (r *Router) deleteTag(w http.ResponseWriter, req *http.Request) {
	deleted, err := r.store.DeleteTag(req.PathValue("id"))
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "delete_tag_failed"})
		return
	}
	if !deleted {
		writeJSON(w, 404, map[string]any{"error": "tag_not_found"})
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": true})
}
func (r *Router) setApplicationTags(w http.ResponseWriter, req *http.Request) {
	var body struct {
		TagIDs []string `json:"tagIds"`
	}
	if decodeJSON(req, &body) != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	app, ok, err := r.store.SetApplicationTags(req.PathValue("id"), body.TagIDs)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "set_application_tags_failed"})
		return
	}
	if !ok {
		writeJSON(w, 404, map[string]any{"error": "application_not_found"})
		return
	}
	writeJSON(w, 200, app)
}

func (r *Router) applicationDRSupportBlock(appID string, requestedStatus string) (bool, string) {
	status := strings.TrimSpace(requestedStatus)
	if status != "pending_protection" && status != "protected" {
		return false, ""
	}
	apps, err := r.store.ListApplications("")
	if err != nil {
		r.logger.Error("failed to validate application DR support", "error", err, "id", appID)
		return false, ""
	}
	for _, app := range apps {
		if app.ID != appID {
			continue
		}
		drSupport, _ := app.ResourceSummary["drSupport"].(map[string]any)
		drSupportStatus := strings.TrimSpace(fmt.Sprint(drSupport["status"]))
		if drSupport == nil || drSupportStatus == "" || drSupportStatus == "<nil>" {
			return true, fmt.Sprintf("%s cannot enter DR setup. DR support has not been checked yet. Refresh namespace inventory and try again.", app.Namespace)
		}
		if strings.EqualFold(drSupportStatus, "unsupported") {
			return true, fmt.Sprintf("%s cannot enter DR setup. %s", app.Namespace, formatUnsupportedDRStorageMessage(drSupport))
		}
		return false, ""
	}
	return false, ""
}

func formatUnsupportedDRStorageMessage(drSupport map[string]any) string {
	detected := detectedUnsupportedStorage(drSupport)
	if detected == "" {
		detected = "unsupported persistent volume storage"
	}
	return fmt.Sprintf(
		"Storage type is not supported for DR. Supported storage: stateless namespaces, or PVCs backed by portable CSI storage such as Longhorn. Unsupported storage: local-path, hostPath, and local PV. Detected storage: %s.",
		detected,
	)
}

func detectedUnsupportedStorage(drSupport map[string]any) string {
	checks, _ := drSupport["checks"].([]any)
	seen := map[string]bool{}
	var values []string
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		if !strings.EqualFold(fmt.Sprint(check["status"]), "unsupported") {
			continue
		}
		storageClass := strings.TrimSpace(fmt.Sprint(check["storageClass"]))
		volumeType := strings.TrimSpace(fmt.Sprint(check["volumeType"]))
		provisioner := strings.TrimSpace(fmt.Sprint(check["provisioner"]))
		parts := []string{}
		if storageClass != "" && storageClass != "<nil>" {
			parts = append(parts, storageClass)
		}
		if volumeType != "" && volumeType != "<nil>" {
			parts = append(parts, volumeType)
		}
		if provisioner != "" && provisioner != "<nil>" {
			parts = append(parts, provisioner)
		}
		if len(parts) == 0 {
			continue
		}
		value := strings.Join(parts, " / ")
		if !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	return strings.Join(values, ", ")
}

func (r *Router) createAgentToken(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Description string `json:"description"`
		TTLSeconds  int    `json:"ttlSeconds"`
	}
	if req.Body != nil {
		_ = json.NewDecoder(req.Body).Decode(&body)
	}

	ttl := 30 * time.Minute
	if body.TTLSeconds > 0 {
		ttl = time.Duration(body.TTLSeconds) * time.Second
	}

	actor, _ := requestUser(req)
	tenantID := actor.TenantID
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	token, err := r.store.CreateAgentToken(tenantID, actor.ID, body.Description, ttl)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "token_create_failed",
		})
		return
	}

	baseURL := r.publicBaseURL(req)
	curlCommand := "curl -sSL "
	if strings.HasPrefix(baseURL, "https://") {
		curlCommand = "curl -k -sSL "
	}

	response := map[string]any{
		"id":        token.ID,
		"token":     token.Token,
		"expiresAt": token.ExpiresAt,
		"installCommand": curlCommand + baseURL + "/install.sh | bash -s -- --token " +
			token.Token + " --endpoint " + r.agentWSEndpoint(req) +
			" --namespace " + r.cfg.AgentNamespace +
			" --executor-mode kubernetes --install-registry-ca false",
	}
	if r.cfg.RegistryCAPath != "" {
		response["prepareNodeCommand"] = curlCommand + baseURL + "/prepare-node.sh | bash"
	}
	writeJSON(w, http.StatusCreated, response)
}

func (r *Router) validateAgentToken(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if decodeJSON(req, &body) != nil || strings.TrimSpace(body.Token) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": "TOKEN_INVALID", "message": store.ErrTokenInvalid.Error()})
		return
	}
	err := r.store.ValidateAgentToken(strings.TrimSpace(body.Token))
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": true})
		return
	}
	status, code := http.StatusUnauthorized, "TOKEN_INVALID"
	if errors.Is(err, store.ErrTokenExpired) {
		status, code = http.StatusGone, "TOKEN_EXPIRED"
	} else if errors.Is(err, store.ErrTokenUsed) {
		status, code = http.StatusConflict, "TOKEN_USED"
	} else if !errors.Is(err, store.ErrTokenInvalid) {
		r.logger.Error("failed to validate agent token", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"valid": false, "error": "TOKEN_CHECK_FAILED", "message": "install token could not be validated"})
		return
	}
	writeJSON(w, status, map[string]any{"valid": false, "error": code, "message": err.Error()})
}

func (r *Router) prepareNodeScript(w http.ResponseWriter, req *http.Request) {
	if r.cfg.RegistryCAPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "registry_ca_not_configured"})
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	script := strings.ReplaceAll(prepareNodeScriptTemplate, "{{REGISTRY_HOST}}", r.registryHost())
	script = strings.ReplaceAll(script, "{{REGISTRY_CA_URL}}", r.publicBaseURL(req)+"/assets/registry/ca.crt")
	_, _ = w.Write([]byte(script))
}

func (r *Router) installScript(w http.ResponseWriter, req *http.Request) {
	agentTarget, agentErr := r.componentTarget(req.Context(), "comm-agent")
	veleroTarget, veleroErr := r.componentTarget(req.Context(), "velero")
	if agentErr != nil || veleroErr != nil {
		r.logger.Error("failed to resolve active component releases for install script", "agent_error", agentErr, "velero_error", veleroErr)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "component_target_unavailable", "message": "Active cluster component versions are not available."})
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	script := strings.ReplaceAll(installScriptTemplate, "{{AGENT_IMAGE}}", agentTarget.Image)
	script = strings.ReplaceAll(script, "{{AGENT_NAMESPACE}}", r.cfg.AgentNamespace)
	script = strings.ReplaceAll(script, "{{AGENT_WS_ENDPOINT}}", r.agentWSEndpoint(req))
	script = strings.ReplaceAll(script, "{{TOKEN_VALIDATE_URL}}", r.publicBaseURL(req)+"/api/v1/agent-tokens/validate")
	script = strings.ReplaceAll(script, "{{VELERO_CRDS_URL}}", r.publicBaseURL(req)+"/assets/velero/v1.17.1/crds.yaml")
	script = strings.ReplaceAll(script, "{{REGISTRY_CA_URL}}", r.publicBaseURL(req)+"/assets/registry/ca.crt")
	script = strings.ReplaceAll(script, "{{VELERO_IMAGE}}", veleroTarget.Image)
	script = strings.ReplaceAll(script, "{{VELERO_AWS_PLUGIN_IMAGE}}", r.cfg.VeleroAWSPlugin)
	script = strings.ReplaceAll(script, "{{VELERO_AZURE_PLUGIN_IMAGE}}", r.cfg.VeleroAzurePlugin)
	script = strings.ReplaceAll(script, "{{VELERO_GCP_PLUGIN_IMAGE}}", r.cfg.VeleroGCPPlugin)
	_, _ = w.Write([]byte(script))
}

func (r *Router) veleroCRDs(w http.ResponseWriter, req *http.Request) {
	data, err := veleroassets.CRDsYAML()
	if err != nil {
		r.logger.Error("failed to render velero crds", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "velero_crds_failed"})
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (r *Router) registryCA(w http.ResponseWriter, req *http.Request) {
	if r.cfg.RegistryCAPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "registry_ca_not_configured"})
		return
	}
	data, err := os.ReadFile(r.cfg.RegistryCAPath)
	if err != nil {
		r.logger.Error("failed to read registry ca", "path", r.cfg.RegistryCAPath, "error", err)
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "registry_ca_not_found"})
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (r *Router) listStorageRepositories(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListStorageRepositories()
	if err != nil {
		r.logger.Error("failed to list storage repositories", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_storage_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) createStorageRepository(w http.ResponseWriter, req *http.Request) {
	var input store.StorageRepositoryInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if input.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required"})
		return
	}
	if actor, ok := requestUser(req); ok {
		input.TenantID = actor.TenantID
	}
	if code, message := validateCloudStorageInput(input, true); code != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": code, "message": message})
		return
	}
	input.Region = normalizedStoredRegion(input.Region)
	item, err := r.store.CreateStorageRepository(input)
	if err != nil {
		r.logger.Error("failed to create storage repository", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_storage_failed"})
		return
	}
	validationStatus := "connected"
	if _, testErr := probeStorageRepository(item, 5*time.Second); testErr != nil {
		validationStatus = "warning"
		r.logger.Warn("new storage repository connection test failed", "repository_id", item.ID, "error", testErr)
	}
	validatedAt := time.Now().UTC()
	updated, ok, updateErr := r.store.SetStorageRepositoryStatus(item.ID, validationStatus, validatedAt)
	if updateErr != nil {
		r.logger.Error("failed to persist new storage repository status", "repository_id", item.ID, "error", updateErr)
	} else if ok {
		item = updated
	}
	writeJSON(w, http.StatusCreated, item)
}

func (r *Router) updateStorageRepository(w http.ResponseWriter, req *http.Request) {
	var input store.StorageRepositoryInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required", "message": "Storage repository name is required."})
		return
	}
	if code, message := validateCloudStorageInput(input, false); code != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": code, "message": message})
		return
	}
	input.Region = normalizedStoredRegion(input.Region)
	item, ok, err := r.store.UpdateStorageRepository(req.PathValue("id"), input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_storage_repository_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "storage_repository_not_found"})
		return
	}
	validationStatus := "connected"
	if _, testErr := probeStorageRepository(item, 5*time.Second); testErr != nil {
		validationStatus = "warning"
		r.logger.Warn("updated storage repository connection test failed", "repository_id", item.ID, "error", testErr)
	}
	validatedAt := time.Now().UTC()
	validated, statusOK, statusErr := r.store.SetStorageRepositoryStatus(item.ID, validationStatus, validatedAt)
	if statusErr != nil {
		r.logger.Error("failed to persist updated storage repository status", "repository_id", item.ID, "error", statusErr)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_storage_status_failed"})
		return
	}
	if statusOK {
		item = validated
	}
	writeJSON(w, http.StatusOK, item)
}

func (r *Router) deleteStorageRepository(w http.ResponseWriter, req *http.Request) {
	deleted, inUse, err := r.store.DeleteStorageRepository(req.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_storage_repository_failed"})
		return
	}
	if inUse {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "storage_repository_in_use", "message": "This storage repository is used by a DR configuration and cannot be deleted."})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "storage_repository_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (r *Router) testStorageRepositoryDraft(w http.ResponseWriter, req *http.Request) {
	var input store.StorageRepositoryInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if code, message := validateCloudStorageInput(input, true); code != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": code, "message": message})
		return
	}
	if input.Endpoint == "" && !strings.EqualFold(input.Type, "Google Cloud") && !strings.EqualFold(input.Type, "GCS") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "endpoint_required"})
		return
	}
	if input.Bucket == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bucket_required"})
		return
	}
	input.Region = normalizedStorageRegion(input.Type, input.Region)
	repo := store.StorageRepository{
		Name:       input.Name,
		Type:       input.Type,
		Endpoint:   input.Endpoint,
		Bucket:     input.Bucket,
		Region:     input.Region,
		TLSEnabled: input.TLSEnabled,
		Config:     input.Config,
		Secret: map[string]string{
			"accessKey":         input.AccessKey,
			"secretKey":         input.SecretKey,
			"accountName":       input.AccountName,
			"accountKey":        input.AccountKey,
			"serviceAccountKey": input.ServiceAccountKey,
		},
	}
	probe, testErr := probeStorageRepository(repo, 5*time.Second)
	status := "connected"
	detail := "S3 bucket is reachable"
	if testErr != nil {
		status = "warning"
		detail = testErr.Error()
	}
	body := map[string]any{
		"status":     status,
		"detail":     detail,
		"reachable":  testErr == nil,
		"testedAt":   time.Now().UTC().Format(time.RFC3339Nano),
		"probe":      probe,
		"repository": repo,
	}
	if testErr != nil {
		body["error"] = detail
	}
	writeJSON(w, http.StatusOK, body)
}

func validateCloudStorageInput(input store.StorageRepositoryInput, requireCredentials bool) (string, string) {
	switch strings.ToLower(strings.TrimSpace(input.Type)) {
	case "azure", "azure blob":
		if strings.TrimSpace(input.AccountName) == "" {
			return "azure_account_name_required", "Azure storage account name is required."
		}
		if requireCredentials && strings.TrimSpace(input.AccountKey) == "" {
			return "azure_account_key_required", "Azure storage account key is required."
		}
	case "google cloud", "gcs":
		key := strings.TrimSpace(input.ServiceAccountKey)
		if key == "" {
			if requireCredentials {
				return "gcs_service_account_required", "Google Cloud service account JSON is required."
			}
			return "", ""
		}
		var credentials struct {
			Type        string `json:"type"`
			ClientEmail string `json:"client_email"`
			PrivateKey  string `json:"private_key"`
		}
		if json.Unmarshal([]byte(key), &credentials) != nil || credentials.Type != "service_account" || strings.TrimSpace(credentials.ClientEmail) == "" || strings.TrimSpace(credentials.PrivateKey) == "" {
			return "gcs_service_account_invalid", "Provide a valid Google Cloud service account JSON key."
		}
	}
	return "", ""
}

func (r *Router) testStorageRepository(w http.ResponseWriter, req *http.Request) {
	repositoryID := req.PathValue("id")
	if repositoryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "repository_id_required"})
		return
	}
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil {
		r.logger.Error("failed to get storage repository", "repository_id", repositoryID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_storage_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "storage_repository_not_found"})
		return
	}

	probe, testErr := probeStorageRepository(repo, 5*time.Second)
	status := "connected"
	detail := "S3 bucket is reachable"
	if testErr != nil {
		status = "warning"
		detail = testErr.Error()
	}
	_ = probe
	updated, _, err := r.store.SetStorageRepositoryStatus(repositoryID, status, time.Now().UTC())
	if err != nil {
		r.logger.Error("failed to update storage status", "repository_id", repositoryID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_storage_status_failed"})
		return
	}

	body := map[string]any{
		"status":          status,
		"detail":          detail,
		"repository":      updated,
		"testedAt":        time.Now().UTC().Format(time.RFC3339Nano),
		"reachable":       testErr == nil,
		"checkedType":     repo.Type,
		"checkedBucket":   repo.Bucket,
		"checkedEndpoint": repo.Endpoint,
	}
	if testErr != nil {
		body["error"] = detail
	}
	writeJSON(w, http.StatusOK, body)
}

func (r *Router) listPolicies(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListPolicies()
	if err != nil {
		r.logger.Error("failed to list policies", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_policies_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) createPolicy(w http.ResponseWriter, req *http.Request) {
	var input store.PolicyInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if actor, ok := requestUser(req); ok {
		input.TenantID = actor.TenantID
	}
	if input.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required"})
		return
	}
	item, err := r.store.CreatePolicy(input)
	if err != nil {
		r.logger.Error("failed to create policy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_policy_failed"})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (r *Router) updatePolicy(w http.ResponseWriter, req *http.Request) {
	var input store.PolicyInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name_required", "message": "Policy name is required."})
		return
	}
	item, ok, err := r.store.UpdatePolicy(req.PathValue("id"), input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_policy_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (r *Router) deletePolicy(w http.ResponseWriter, req *http.Request) {
	deleted, inUse, err := r.store.DeletePolicy(req.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_policy_failed"})
		return
	}
	if inUse {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "policy_in_use", "message": "This policy is used by a DR configuration and cannot be deleted."})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "policy_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func probeStorageRepository(repo store.StorageRepository, timeout time.Duration) (map[string]any, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	typeName := strings.ToLower(strings.TrimSpace(repo.Type))
	if repo.Endpoint == "" && typeName != "google cloud" && typeName != "gcs" {
		return nil, errors.New("endpoint is empty")
	}
	if repo.Bucket == "" {
		return nil, errors.New("bucket is empty")
	}
	if typeName == "s3" || typeName == "s3-compatible" || typeName == "s3 compatible" {
		creds := storageCredentials(repo)
		if creds == nil || strings.TrimSpace(creds.AccessKey) == "" || strings.TrimSpace(creds.SecretKey) == "" {
			return nil, errors.New("S3 access key and secret key are required")
		}
		endpoint, secure := minioEndpoint(repo)
		client, err := minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
			Secure: secure,
			Region: normalizedStorageRegion(repo.Type, repo.Region),
		})
		if err != nil {
			return map[string]any{"endpoint": endpoint, "bucket": repo.Bucket}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		for object := range client.ListObjects(ctx, repo.Bucket, minio.ListObjectsOptions{Recursive: false}) {
			if object.Err != nil {
				return map[string]any{"endpoint": endpoint, "bucket": repo.Bucket}, object.Err
			}
			break
		}
		return map[string]any{"endpoint": endpoint, "bucket": repo.Bucket, "authenticated": true}, nil
	}
	endpoint := strings.TrimRight(strings.TrimSpace(repo.Endpoint), "/")
	if typeName == "google cloud" || typeName == "gcs" {
		endpoint = "storage.googleapis.com"
	}
	scheme := "http"
	if repo.TLSEnabled {
		scheme = "https"
	}
	if strings.HasPrefix(endpoint, "http://") {
		scheme = "http"
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else if strings.HasPrefix(endpoint, "https://") {
		scheme = "https"
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	urlStyle, _ := repo.Config["urlStyle"].(string)
	if urlStyle == "" {
		urlStyle = "path"
	}
	var probeURL string
	if typeName == "azure" {
		probeURL = scheme + "://" + endpoint + "/" + repo.Bucket + "?restype=container"
	} else if typeName == "google cloud" || typeName == "gcs" {
		probeURL = "https://storage.googleapis.com/" + repo.Bucket
	} else if urlStyle == "virtual" {
		probeURL = scheme + "://" + repo.Bucket + "." + endpoint + "/?probe=1"
	} else {
		probeURL = scheme + "://" + endpoint + "/" + repo.Bucket + "?probe=1"
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Head(probeURL)
	if err != nil {
		return map[string]any{"url": probeURL, "style": urlStyle}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusForbidden {
		return map[string]any{"url": probeURL, "style": urlStyle, "statusCode": resp.StatusCode}, nil
	}
	return map[string]any{"url": probeURL, "style": urlStyle, "statusCode": resp.StatusCode},
		errors.New("unexpected status " + resp.Status)
}

type objectStorageCleanupResult struct {
	RepositoryID   string
	RepositoryName string
	Prefix         string
	ObjectsDeleted int
	BytesDeleted   int64
}

func (r *Router) cleanupClusterObjectStorage(ctx context.Context, clusterID string) ([]objectStorageCleanupResult, error) {
	audit, err := r.auditClusterUnregister(clusterID)
	if err != nil {
		return nil, err
	}
	if !audit.ObjectStorageNeeded {
		return []objectStorageCleanupResult{}, nil
	}
	return r.cleanupClusterObjectStorageRepositories(ctx, clusterID, audit.StorageRepositoryIDs)
}

func (r *Router) cleanupClusterObjectStorageRepositories(ctx context.Context, clusterID string, repositoryIDs []string) ([]objectStorageCleanupResult, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return nil, errors.New("cluster id is required for object storage cleanup")
	}
	if len(repositoryIDs) == 0 {
		return nil, errors.New("restore points exist but no storage repository is associated with them")
	}
	repositories, err := r.store.ListStorageRepositories()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(repositoryIDs))
	for _, id := range repositoryIDs {
		wanted[id] = struct{}{}
	}
	prefix := strings.TrimSuffix(storageDomainPrefix(clusterID), "/") + "/"
	results := make([]objectStorageCleanupResult, 0, len(repositoryIDs))
	for _, repo := range repositories {
		if _, ok := wanted[repo.ID]; !ok {
			continue
		}
		result, err := cleanObjectStoragePrefix(ctx, repo, prefix)
		if err != nil {
			return results, fmt.Errorf("cleanup repository %s prefix %s: %w", repo.Name, prefix, err)
		}
		results = append(results, result)
		delete(wanted, repo.ID)
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return results, fmt.Errorf("associated storage repositories not found: %s", strings.Join(missing, ", "))
	}
	return results, nil
}

func deleteObjectStoragePrefix(ctx context.Context, repo store.StorageRepository, prefix string) (objectStorageCleanupResult, error) {
	result := objectStorageCleanupResult{
		RepositoryID:   repo.ID,
		RepositoryName: repo.Name,
		Prefix:         prefix,
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return result, errors.New("refusing to delete empty object storage prefix")
	}
	if !strings.HasPrefix(prefix, "hypercdr/clusters/") || !strings.HasSuffix(prefix, "/") {
		return result, fmt.Errorf("refusing to delete unexpected object storage prefix %q", prefix)
	}
	if strings.TrimSpace(repo.Endpoint) == "" {
		return result, errors.New("storage repository endpoint is empty")
	}
	if strings.TrimSpace(repo.Bucket) == "" {
		return result, errors.New("storage repository bucket is empty")
	}
	creds := storageCredentials(repo)
	if creds == nil || creds.AccessKey == "" || creds.SecretKey == "" {
		return result, errors.New("storage repository credentials are empty")
	}
	endpoint, secure := minioEndpoint(repo)
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure: secure,
		Region: repo.Region,
	})
	if err != nil {
		return result, err
	}
	for object := range client.ListObjects(ctx, repo.Bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return result, object.Err
		}
		if err := client.RemoveObject(ctx, repo.Bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return result, err
		}
		result.ObjectsDeleted++
		result.BytesDeleted += object.Size
	}
	return result, nil
}

func minioEndpoint(repo store.StorageRepository) (string, bool) {
	endpoint := strings.TrimRight(strings.TrimSpace(repo.Endpoint), "/")
	secure := repo.TLSEnabled
	if strings.HasPrefix(endpoint, "http://") {
		secure = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else if strings.HasPrefix(endpoint, "https://") {
		secure = true
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	return endpoint, secure
}

func (r *Router) syncStorageRepository(w http.ResponseWriter, req *http.Request) {
	repositoryID := req.PathValue("id")
	if repositoryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "repository_id_required"})
		return
	}
	var body struct {
		ClusterID string `json:"clusterId"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if body.ClusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil {
		r.logger.Error("failed to get storage repository", "repository_id", repositoryID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_storage_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "storage_repository_not_found"})
		return
	}
	repo.Region = normalizedStorageRegion(repo.Type, repo.Region)
	bslName := storageDomainBSLName(repo, body.ClusterID)
	objectPrefix := storageDomainPrefix(body.ClusterID)
	config := storageConfigWithPrefix(repo, objectPrefix)
	binding, err := r.store.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
		ClusterID:       body.ClusterID,
		StorageRepoID:   repo.ID,
		SourceClusterID: body.ClusterID,
		BSLName:         bslName,
		ObjectPrefix:    objectPrefix,
		Status:          "configuring",
		RetryCount:      1,
		RepoUpdatedAt:   repo.UpdatedAt,
	})
	if err != nil {
		r.logger.Error("failed to upsert cluster storage binding", "cluster_id", body.ClusterID, "repository_id", repo.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "upsert_cluster_storage_binding_failed"})
		return
	}

	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID: body.ClusterID,
		Type:      "storage-sync",
		Status:    "queued",
		CommandID: commandID,
		Payload: map[string]any{
			"requestedBy":     requestActor(req),
			"repositoryId":    repo.ID,
			"name":            bslName,
			"displayName":     repo.Name,
			"sourceClusterId": body.ClusterID,
			"objectPrefix":    objectPrefix,
			"type":            repo.Type,
			"endpoint":        repo.Endpoint,
			"bucket":          repo.Bucket,
			"region":          repo.Region,
			"tlsEnabled":      repo.TLSEnabled,
			"secretRef":       repo.SecretRef,
			"hasSecret":       len(repo.Secret) > 0,
			"config":          config,
			"bindingId":       binding.ID,
		},
	})
	if err != nil {
		r.logger.Error("failed to create storage sync task", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}

	conn, ok := r.hub.get(body.ClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected",
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "agent is offline; task remains queued",
		})
		return
	}

	dispatch := protocol.Message[protocol.TaskDispatchPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformTaskDispatch,
		ClusterID:   body.ClusterID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.TaskDispatchPayload{
			TaskID:    task.ID,
			CommandID: commandID,
			Type:      "storage-sync",
			Deadline:  time.Now().UTC().Add(10 * time.Minute),
			StorageSync: &protocol.StorageSyncCommand{
				RepositoryID: repo.ID,
				Name:         bslName,
				Type:         repo.Type,
				Endpoint:     repo.Endpoint,
				Bucket:       repo.Bucket,
				Region:       repo.Region,
				TLSEnabled:   repo.TLSEnabled,
				SecretRef:    repo.SecretRef,
				Credentials:  storageCredentials(repo),
				Config:       config,
			},
		},
	}
	if err := conn.WriteJSON(dispatch); err != nil {
		r.logger.Error("failed to dispatch storage sync task", "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task":    task,
			"warning": "task created but dispatch failed",
		})
		return
	}

	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	writeJSON(w, http.StatusCreated, task)
}

func storageCredentials(repo store.StorageRepository) *protocol.S3Credentials {
	if len(repo.Secret) == 0 {
		return nil
	}
	credentials := &protocol.S3Credentials{
		AccessKey: repo.Secret["accessKey"], SecretKey: repo.Secret["secretKey"], AccountName: repo.Secret["accountName"], AccountKey: repo.Secret["accountKey"], ServiceAccountKey: repo.Secret["serviceAccountKey"],
	}
	if credentials.AccessKey == "" && credentials.SecretKey == "" && credentials.AccountKey == "" && credentials.ServiceAccountKey == "" {
		return nil
	}
	return credentials
}

func (r *Router) dispatchStorageSyncTask(clusterID string, repositoryID string) (store.Task, string, error) {
	return r.dispatchStorageSyncTaskForPlan(clusterID, repositoryID, "")
}

func (r *Router) dispatchStorageSyncTaskForPlan(clusterID string, repositoryID string, protectionPlanID string) (store.Task, string, error) {
	return r.dispatchStorageSyncTaskForPlanActivation(clusterID, repositoryID, protectionPlanID, "", "", false)
}

const storageSyncMaxAttempts = 3
const protectionPlanActivationTaskTimeout = 90 * time.Second

func storageDomainPrefix(sourceClusterID string) string {
	sourceClusterID = strings.TrimSpace(sourceClusterID)
	if sourceClusterID == "" {
		return "hypercdr/clusters/unknown"
	}
	return "hypercdr/clusters/" + sourceClusterID
}

func storageDomainBSLName(repo store.StorageRepository, sourceClusterID string) string {
	base := sanitizeKubernetesName(repo.Name)
	sourceID := sanitizeKubernetesName(strings.TrimSpace(sourceClusterID))
	if sourceID == "" {
		sourceID = "unknown"
	}
	name := base + "-" + sourceID
	if len(name) > 63 {
		maxBase := 63 - len(sourceID) - 1
		if maxBase < 1 {
			maxBase = 1
		}
		name = strings.Trim(base[:min(len(base), maxBase)], "-") + "-" + sourceID
	}
	return strings.Trim(name, "-")
}

func sanitizeKubernetesName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}

func storageConfigWithPrefix(repo store.StorageRepository, prefix string) map[string]any {
	config := map[string]any{}
	for key, value := range repo.Config {
		config[key] = value
	}
	config["prefix"] = prefix
	return config
}

func normalizedStorageRegion(storageType, region string) string {
	region = normalizedStoredRegion(region)
	typeName := strings.ToLower(strings.TrimSpace(storageType))
	if region == "" && (typeName == "s3" || typeName == "s3-compatible" || typeName == "s3 compatible") {
		return "us-east-1"
	}
	return region
}

func normalizedStoredRegion(region string) string {
	region = strings.TrimSpace(region)
	switch strings.ToLower(region) {
	case "n/a", "na", "-":
		return ""
	default:
		return region
	}
}

func (r *Router) dispatchStorageSyncTaskForPlanActivation(clusterID string, repositoryID string, protectionPlanID string, activationAttempt string, activationRole string, reconfigureStorage bool) (store.Task, string, error) {
	return r.dispatchStorageSyncTaskForPlanActivationAttempt(clusterID, repositoryID, protectionPlanID, clusterID, activationAttempt, activationRole, reconfigureStorage, 1)
}

func (r *Router) dispatchStorageSyncTaskForPlanActivationAttempt(clusterID string, repositoryID string, protectionPlanID string, sourceClusterID string, activationAttempt string, activationRole string, reconfigureStorage bool, retryAttempt int) (store.Task, string, error) {
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok {
		return store.Task{}, "", errors.New("storage repository not found")
	}
	repo.Region = normalizedStorageRegion(repo.Type, repo.Region)
	if sourceClusterID == "" {
		sourceClusterID = clusterID
	}
	bslName := storageDomainBSLName(repo, sourceClusterID)
	objectPrefix := storageDomainPrefix(sourceClusterID)
	config := storageConfigWithPrefix(repo, objectPrefix)
	binding, err := r.store.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
		ClusterID:       clusterID,
		StorageRepoID:   repo.ID,
		SourceClusterID: sourceClusterID,
		BSLName:         bslName,
		ObjectPrefix:    objectPrefix,
		Status:          "configuring",
		RetryCount:      retryAttempt,
		RepoUpdatedAt:   repo.UpdatedAt,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	commandID := store.NewPublicID()
	payload := map[string]any{
		"repositoryId":    repo.ID,
		"name":            bslName,
		"displayName":     repo.Name,
		"sourceClusterId": sourceClusterID,
		"objectPrefix":    objectPrefix,
		"type":            repo.Type,
		"endpoint":        repo.Endpoint,
		"bucket":          repo.Bucket,
		"region":          repo.Region,
		"tlsEnabled":      repo.TLSEnabled,
		"secretRef":       repo.SecretRef,
		"hasSecret":       len(repo.Secret) > 0,
		"config":          config,
		"bindingId":       binding.ID,
	}
	if activationAttempt != "" {
		payload["activationAttempt"] = activationAttempt
		payload["retryAttempt"] = retryAttempt
		payload["maxAttempts"] = storageSyncMaxAttempts
	}
	if activationRole != "" {
		payload["activationRole"] = activationRole
	}
	if reconfigureStorage {
		payload["reconfigureStorage"] = true
	}
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        clusterID,
		ProtectionPlanID: protectionPlanID,
		Type:             "storage-sync",
		Status:           "queued",
		CommandID:        commandID,
		Payload:          payload,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected",
		})
		return task, "agent is offline; task remains queued", nil
	}
	dispatch := protocol.Message[protocol.TaskDispatchPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformTaskDispatch,
		ClusterID:   clusterID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.TaskDispatchPayload{
			TaskID:    task.ID,
			CommandID: commandID,
			Type:      "storage-sync",
			Deadline:  time.Now().UTC().Add(10 * time.Minute),
			StorageSync: &protocol.StorageSyncCommand{
				RepositoryID: repo.ID,
				Name:         bslName,
				Type:         repo.Type,
				Endpoint:     repo.Endpoint,
				Bucket:       repo.Bucket,
				Region:       repo.Region,
				TLSEnabled:   repo.TLSEnabled,
				SecretRef:    repo.SecretRef,
				Credentials:  storageCredentials(repo),
				Config:       config,
			},
		},
	}
	if err := conn.WriteJSON(dispatch); err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		return task, "task created but dispatch failed", nil
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	return task, "", nil
}

func (r *Router) ensureStorageSynced(ctx context.Context, clusterID string, storageName string, repositoryID string, sourceClusterID string) (store.Task, error) {
	if repositoryID == "" {
		repo, ok, err := r.findStorageRepositoryByName(storageName)
		if err != nil {
			return store.Task{}, err
		}
		if !ok {
			return store.Task{}, errors.New("storage repository " + storageName + " not found")
		}
		repositoryID = repo.ID
	}
	if sourceClusterID == "" {
		sourceClusterID = clusterID
	}
	task, warning, err := r.dispatchStorageSyncTaskForPlanActivationAttempt(clusterID, repositoryID, "", sourceClusterID, "", "", false, 1)
	if err != nil {
		return task, err
	}
	if warning != "" {
		return task, errors.New(warning)
	}
	if err := r.waitForTaskSucceeded(ctx, task.ID, 2*time.Minute); err != nil {
		return task, err
	}
	return task, nil
}

func (r *Router) findStorageRepositoryByName(name string) (store.StorageRepository, bool, error) {
	repos, err := r.store.ListStorageRepositories()
	if err != nil {
		return store.StorageRepository{}, false, err
	}
	for _, repo := range repos {
		if repo.Name == name {
			return repo, true, nil
		}
	}
	return store.StorageRepository{}, false, nil
}

func (r *Router) waitForTaskSucceeded(ctx context.Context, taskID string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		task, ok, err := r.findTask(taskID)
		if err != nil {
			return err
		}
		if ok {
			switch strings.ToLower(task.Status) {
			case "succeeded", "completed", "success":
				return nil
			case "failed":
				message := task.ErrorMessage
				if message == "" {
					message = task.ErrorCode
				}
				if message == "" {
					message = "task failed"
				}
				return errors.New(message)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for storage sync task to complete")
		case <-ticker.C:
		}
	}
}

func (r *Router) findTask(taskID string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) redispatchPendingTasks(clusterID string, conn *websocket.Conn) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		r.logger.Error("failed to list tasks for redispatch", "cluster_id", clusterID, "error", err)
		return
	}
	for _, task := range tasks {
		if task.Status != "queued" && task.Status != "dispatched" {
			continue
		}
		if err := r.dispatchStoredTask(conn, task); err != nil {
			r.logger.Error("failed to redispatch task", "cluster_id", clusterID, "task_id", task.ID, "error", err)
			_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       task.ID,
				Status:       "queued",
				Progress:     task.Progress,
				ErrorCode:    "REDISPATCH_FAILED",
				ErrorMessage: err.Error(),
			})
			continue
		}
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:   task.ID,
			Status:   "dispatched",
			Progress: task.Progress,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "redispatched",
			Message: "task redispatched after agent reconnect",
		})
	}
}

func (r *Router) dispatchStoredTask(conn *websocket.Conn, task store.Task) error {
	dispatch, err := r.buildStoredTaskDispatch(task)
	if err != nil {
		return err
	}
	return conn.WriteJSON(dispatch)
}

func (r *Router) buildStoredTaskDispatch(task store.Task) (protocol.Message[protocol.TaskDispatchPayload], error) {
	commandID := task.CommandID
	if commandID == "" {
		commandID = store.NewPublicID()
	}
	payload := protocol.TaskDispatchPayload{
		TaskID:    task.ID,
		CommandID: commandID,
		Type:      task.Type,
		Deadline:  time.Now().UTC().Add(30 * time.Minute),
	}
	switch task.Type {
	case "storage-sync":
		repositoryID := stringPayload(task.Payload, "repositoryId")
		repo, ok, err := r.store.GetStorageRepository(repositoryID)
		if err != nil {
			return protocol.Message[protocol.TaskDispatchPayload]{}, err
		}
		if !ok {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("storage repository not found")
		}
		repo.Region = normalizedStorageRegion(repo.Type, repo.Region)
		name := stringPayload(task.Payload, "name")
		if name == "" {
			name = storageDomainBSLName(repo, stringPayload(task.Payload, "sourceClusterId"))
		}
		config := repo.Config
		if raw, ok := task.Payload["config"].(map[string]any); ok {
			config = raw
		}
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.StorageSync = &protocol.StorageSyncCommand{
			RepositoryID: repo.ID,
			Name:         name,
			Type:         repo.Type,
			Endpoint:     repo.Endpoint,
			Bucket:       repo.Bucket,
			Region:       repo.Region,
			TLSEnabled:   repo.TLSEnabled,
			SecretRef:    repo.SecretRef,
			Credentials:  storageCredentials(repo),
			Config:       config,
		}
	case "backup":
		sourceNamespace := stringPayload(task.Payload, "sourceNamespace")
		sourceNamespaces := stringSlicePayload(task.Payload, "sourceNamespaces")
		if len(sourceNamespaces) == 0 && sourceNamespace != "" {
			sourceNamespaces = []string{sourceNamespace}
		}
		payload.Backup = &protocol.BackupCommand{
			PlanID:                  task.ProtectionPlanID,
			Trigger:                 firstNonEmptyString(stringPayload(task.Payload, "trigger"), "manual"),
			SourceClusterID:         task.ClusterID,
			SourceNamespace:         sourceNamespace,
			SourceNamespaces:        sourceNamespaces,
			VeleroBackupName:        stringPayload(task.Payload, "veleroBackupName"),
			Scope:                   stringPayload(task.Payload, "scope"),
			IncludedResources:       stringSlicePayload(task.Payload, "includedResources"),
			LabelSelector:           protocolLabelSelector(labelSelectorPayload(task.Payload)),
			StorageRepo:             stringPayload(task.Payload, "storageRepo"),
			IncludeClusterResources: boolPayload(task.Payload, "includeClusterResources"),
			ExcludedResources:       stringSlicePayload(task.Payload, "excludedResources"),
			Hooks:                   protocol.HookSet{},
		}
	case "backup-cancel":
		targetTaskID := stringPayload(task.Payload, "targetTaskId")
		if targetTaskID == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("backup cancel target task id is required")
		}
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.BackupCancel = &protocol.BackupCancelCommand{
			PlanID:           firstNonEmptyString(stringPayload(task.Payload, "planId"), task.ProtectionPlanID),
			TargetTaskID:     targetTaskID,
			VeleroBackupName: stringPayload(task.Payload, "veleroBackupName"),
			Reason:           firstNonEmptyString(stringPayload(task.Payload, "reason"), "user_requested"),
		}
	case "retention-cleanup":
		command := retentionCleanupCommandFromPayload(task.Payload)
		if len(command.RestorePoints) == 0 {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("retention cleanup has no restore points")
		}
		payload.Deadline = time.Now().UTC().Add(30 * time.Minute)
		payload.RetentionCleanup = command
	case "protection-cleanup":
		command := protectionCleanupCommandFromPayload(task.Payload)
		if command.PlanID == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("protection cleanup plan id is required")
		}
		payload.Deadline = time.Now().UTC().Add(30 * time.Minute)
		payload.ProtectionCleanup = command
	case "agent-upgrade":
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.AgentUpgrade = &protocol.AgentUpgradeCommand{
			ClusterID:         stringPayload(task.Payload, "clusterId"),
			Namespace:         stringPayload(task.Payload, "namespace"),
			Image:             stringPayload(task.Payload, "image"),
			Version:           stringPayload(task.Payload, "version"),
			ExpectedDigest:    stringPayload(task.Payload, "expectedDigest"),
			DeploymentName:    stringPayload(task.Payload, "deploymentName"),
			ContainerName:     stringPayload(task.Payload, "containerName"),
			RolloutAnnotation: stringPayload(task.Payload, "rolloutAnnotation"),
		}
		if payload.AgentUpgrade.ClusterID == "" {
			payload.AgentUpgrade.ClusterID = task.ClusterID
		}
		if payload.AgentUpgrade.Namespace == "" {
			payload.AgentUpgrade.Namespace = r.agentNamespace()
		}
		if payload.AgentUpgrade.Image == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("agent upgrade image is required")
		}
	case "velero-upgrade":
		payload.Deadline = time.Now().UTC().Add(15 * time.Minute)
		payload.VeleroUpgrade = &protocol.VeleroUpgradeCommand{
			ClusterID: task.ClusterID, Namespace: firstNonEmptyString(stringPayload(task.Payload, "namespace"), r.agentNamespace()),
			Image: stringPayload(task.Payload, "image"), Version: stringPayload(task.Payload, "version"), ExpectedDigest: stringPayload(task.Payload, "expectedDigest"),
			DeploymentName: stringPayload(task.Payload, "deploymentName"), DaemonSetName: stringPayload(task.Payload, "daemonSetName"),
			AWSPluginImage: stringPayload(task.Payload, "awsPluginImage"), AzurePluginImage: stringPayload(task.Payload, "azurePluginImage"), GCPPluginImage: stringPayload(task.Payload, "gcpPluginImage"),
		}
		if payload.VeleroUpgrade.Image == "" {
			return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("velero upgrade image is required")
		}
	case "restore", "drill", "takeover":
		sourceNamespace := stringPayload(task.Payload, "sourceNamespace")
		sourceNamespaces := stringSlicePayload(task.Payload, "sourceNamespaces")
		if len(sourceNamespaces) == 0 && sourceNamespace != "" {
			sourceNamespaces = []string{sourceNamespace}
		}
		payload.Restore = &protocol.RestoreCommand{
			RestorePointID:       stringPayload(task.Payload, "restorePointId"),
			VeleroBackupName:     stringPayload(task.Payload, "veleroBackupName"),
			StorageRepo:          stringPayload(task.Payload, "storageRepo"),
			SourceNamespace:      sourceNamespace,
			SourceNamespaces:     sourceNamespaces,
			TargetNamespace:      stringPayload(task.Payload, "targetNamespace"),
			TargetNamespaces:     stringMapPayload(task.Payload, "targetNamespaces"),
			TargetMode:           stringPayload(task.Payload, "targetMode"),
			RestoreMode:          stringPayload(task.Payload, "restoreMode"),
			ArtifactMode:         stringPayload(task.Payload, "artifactMode"),
			ConflictPolicy:       stringPayload(task.Payload, "conflictPolicy"),
			IncludeClusterScoped: boolPayload(task.Payload, "includeClusterScoped"),
			UseTransforms:        boolPayload(task.Payload, "useTransforms"),
			TransformPreset:      stringPayload(task.Payload, "transformPreset"),
			StorageProfileMode:   stringPayload(task.Payload, "storageProfileMode"),
			AlternateProfileID:   stringPayload(task.Payload, "alternateProfileId"),
		}
	case "unregister":
		payload.Deadline = time.Now().UTC().Add(10 * time.Minute)
		payload.Unregister = &protocol.UnregisterCommand{
			ClusterID:       stringPayload(task.Payload, "clusterId"),
			Namespace:       stringPayload(task.Payload, "namespace"),
			DeleteVelero:    boolPayload(task.Payload, "deleteVelero"),
			DeleteNamespace: boolPayload(task.Payload, "deleteNamespace"),
			Reason:          stringPayload(task.Payload, "reason"),
		}
		if payload.Unregister.ClusterID == "" {
			payload.Unregister.ClusterID = task.ClusterID
		}
		if payload.Unregister.Namespace == "" {
			payload.Unregister.Namespace = r.agentNamespace()
		}
	default:
		return protocol.Message[protocol.TaskDispatchPayload]{}, errors.New("unsupported task type for redispatch")
	}
	return protocol.Message[protocol.TaskDispatchPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindRequest,
		Type:        protocol.MessagePlatformTaskDispatch,
		ClusterID:   task.ClusterID,
		Timestamp:   time.Now().UTC(),
		Payload:     payload,
	}, nil
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func boolPayload(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func stringSlicePayload(payload map[string]any, key string) []string {
	raw, ok := payload[key].([]any)
	if !ok {
		if stringsValue, ok := payload[key].([]string); ok {
			return stringsValue
		}
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}

func labelSelectorPayload(payload map[string]any) store.LabelSelector {
	raw, ok := payload["labelSelector"]
	if !ok || raw == nil {
		return store.LabelSelector{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return store.LabelSelector{}
	}
	var selector store.LabelSelector
	if err := json.Unmarshal(data, &selector); err != nil {
		return store.LabelSelector{}
	}
	return selector
}

func protocolLabelSelector(selector store.LabelSelector) protocol.LabelSelector {
	expressions := make([]protocol.LabelSelectorExpression, 0, len(selector.MatchExpressions))
	for _, expression := range selector.MatchExpressions {
		expressions = append(expressions, protocol.LabelSelectorExpression{
			Key: expression.Key, Operator: expression.Operator, Values: expression.Values,
		})
	}
	return protocol.LabelSelector{MatchLabels: selector.MatchLabels, MatchExpressions: expressions}
}

func stringMapPayload(payload map[string]any, key string) map[string]string {
	raw, ok := payload[key].(map[string]any)
	if !ok {
		if typed, ok := payload[key].(map[string]string); ok {
			return typed
		}
		return nil
	}
	values := map[string]string{}
	for key, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			values[key] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func retentionCleanupCommandFromPayload(payload map[string]any) *protocol.RetentionCleanupCommand {
	points := retentionRestorePointsFromAny(payload["restorePoints"])
	return &protocol.RetentionCleanupCommand{
		PlanID:        stringPayload(payload, "planId"),
		RestorePoints: points,
	}
}

func protectionCleanupCommandFromPayload(payload map[string]any) *protocol.ProtectionCleanupCommand {
	return &protocol.ProtectionCleanupCommand{
		PlanID:               stringPayload(payload, "planId"),
		CleanupMode:          stringPayload(payload, "cleanupMode"),
		ScheduleName:         stringPayload(payload, "scheduleName"),
		BackupNamePrefix:     stringPayload(payload, "backupNamePrefix"),
		Namespace:            stringPayload(payload, "namespace"),
		SourceNamespaces:     stringSlicePayload(payload, "sourceNamespaces"),
		StorageRepo:          stringPayload(payload, "storageRepo"),
		CleanupObjectStorage: boolPayload(payload, "cleanupObjectStorage"),
		RestorePoints:        retentionRestorePointsFromAny(payload["restorePoints"]),
		RestoreNames:         stringSlicePayload(payload, "restoreNames"),
	}
}

func retentionRestorePointsFromAny(raw any) []protocol.RetentionRestorePoint {
	points := []protocol.RetentionRestorePoint{}
	appendPoint := func(values map[string]any) {
		point := protocol.RetentionRestorePoint{
			ID:               stringFromMap(values, "id"),
			VeleroBackupName: stringFromMap(values, "veleroBackupName"),
			Namespace:        stringFromMap(values, "namespace"),
		}
		if point.ID != "" && point.VeleroBackupName != "" {
			points = append(points, point)
		}
	}
	switch values := raw.(type) {
	case []any:
		for _, item := range values {
			if point, ok := item.(protocol.RetentionRestorePoint); ok {
				if point.ID != "" && point.VeleroBackupName != "" {
					points = append(points, point)
				}
				continue
			}
			if mapped, ok := item.(map[string]any); ok {
				appendPoint(mapped)
			}
		}
	case []map[string]any:
		for _, item := range values {
			appendPoint(item)
		}
	case []protocol.RetentionRestorePoint:
		for _, point := range values {
			if point.ID != "" && point.VeleroBackupName != "" {
				points = append(points, point)
			}
		}
	}
	return points
}

func (r *Router) scheduleSyncCommandFromPayload(payload map[string]any) (*protocol.ScheduleSyncCommand, error) {
	repoName := stringPayload(payload, "storageRepo")
	if repoName == "" {
		repoID := stringPayload(payload, "storageRepoId")
		if repoID != "" {
			repo, ok, err := r.store.GetStorageRepository(repoID)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errors.New("storage repository not found")
			}
			repoName = repo.Name
		}
	}
	sourceNamespace := stringPayload(payload, "sourceNamespace")
	sourceNamespaces := stringSlicePayload(payload, "sourceNamespaces")
	if len(sourceNamespaces) == 0 && sourceNamespace != "" {
		sourceNamespaces = []string{sourceNamespace}
	}
	excludeRules := append(
		defaultExcludedResourcesForNamespaces(sourceNamespaces),
		excludeRulesPayload(payload)...,
	)
	return &protocol.ScheduleSyncCommand{
		PlanID:                  stringPayload(payload, "planId"),
		ScheduleName:            stringPayload(payload, "scheduleName"),
		Cron:                    stringPayload(payload, "cron"),
		SourceNamespace:         sourceNamespace,
		SourceNamespaces:        sourceNamespaces,
		Scope:                   stringPayload(payload, "scope"),
		LabelSelector:           "",
		StorageRepo:             repoName,
		IncludeClusterResources: boolPayload(payload, "includeClusterResources"),
		ExcludeResources:        excludeRules,
		Hooks:                   protocol.HookSet{},
	}, nil
}

func excludeRulesPayload(payload map[string]any) []protocol.ExcludeRule {
	raw, ok := payload["excludeRules"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	rules := make([]protocol.ExcludeRule, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rule := protocol.ExcludeRule{
			Group:    stringFromMap(m, "group"),
			Resource: stringFromMap(m, "resource"),
			Name:     stringFromMap(m, "name"),
			Version:  stringFromMap(m, "version"),
			Labels:   stringFromMap(m, "labels"),
		}
		if rule.Group == "" && rule.Resource == "" && rule.Name == "" && rule.Version == "" && rule.Labels == "" {
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func mapPayload(values map[string]any, key string) map[string]any {
	raw, ok := values[key].(map[string]any)
	if ok {
		return raw
	}
	stringsMap, ok := values[key].(map[string]string)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(stringsMap))
	for k, v := range stringsMap {
		out[k] = v
	}
	return out
}

func firstStringFromAny(value any) string {
	switch typed := value.(type) {
	case []string:
		if len(typed) > 0 {
			return typed[0]
		}
	case []any:
		if len(typed) > 0 {
			value, _ := typed[0].(string)
			return value
		}
	}
	return ""
}

func stringArrayFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return uniqueNonEmptyStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				values = append(values, text)
			}
		}
		return uniqueNonEmptyStrings(values)
	default:
		return nil
	}
}

func firstStringFromStrings(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseTimeFromAny(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case string:
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		var parsed int64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func veleroBackupSizeBytes(velero map[string]any) int64 {
	restorePointSize := mapFromAny(velero["restorePointSize"])
	if totalBytes := int64FromAny(restorePointSize["totalBytes"]); totalBytes > 0 {
		return totalBytes
	}
	size := mapFromAny(velero["size"])
	if totalBytes := int64FromAny(size["totalBytes"]); totalBytes > 0 {
		return totalBytes
	}
	if sizeBytes := int64FromAny(velero["sizeBytes"]); sizeBytes > 0 {
		return sizeBytes
	}
	return 0
}

func planStorageTotalBytesFromMetadata(metadata map[string]any) int64 {
	if len(metadata) == 0 {
		return 0
	}
	candidates := []map[string]any{
		mapFromAny(metadata["planStorageSize"]),
		mapFromAny(mapFromAny(metadata["velero"])["planStorageSize"]),
	}
	for _, candidate := range candidates {
		if totalBytes := int64FromAny(candidate["totalBytes"]); totalBytes != 0 {
			return totalBytes
		}
		if totalBytes := int64FromAny(candidate["total"]); totalBytes != 0 {
			return totalBytes
		}
		metadataBytes := int64FromAny(candidate["metadataBytes"])
		kopiaBytes := int64FromAny(candidate["kopiaBytes"])
		volumeBytes := int64FromAny(candidate["volumeBytes"])
		if metadataBytes != 0 || kopiaBytes != 0 || volumeBytes != 0 {
			return metadataBytes + kopiaBytes + volumeBytes
		}
	}
	return 0
}

func enrichRestorePointStorageIncrements(items []store.RestorePoint) []store.RestorePoint {
	grouped := map[string][]int{}
	for index, item := range items {
		if item.ProtectionPlanID == "" || !strings.EqualFold(item.Status, "available") {
			continue
		}
		grouped[item.ProtectionPlanID] = append(grouped[item.ProtectionPlanID], index)
	}
	for _, indexes := range grouped {
		sort.SliceStable(indexes, func(i, j int) bool {
			left := items[indexes[i]]
			right := items[indexes[j]]
			leftTime := left.CompletedAt
			if leftTime.IsZero() {
				leftTime = left.CreatedAt
			}
			rightTime := right.CompletedAt
			if rightTime.IsZero() {
				rightTime = right.CreatedAt
			}
			return leftTime.Before(rightTime)
		})
		var previousTotal int64
		for _, index := range indexes {
			currentTotal := planStorageTotalBytesFromMetadata(items[index].Metadata)
			if currentTotal == 0 {
				continue
			}
			delta := currentTotal
			hasPrevious := previousTotal != 0
			if hasPrevious {
				delta = currentTotal - previousTotal
			}
			if items[index].Metadata == nil {
				items[index].Metadata = map[string]any{}
			}
			items[index].Metadata["storageIncrementSize"] = map[string]any{
				"bytes":              delta,
				"planTotalBytes":     currentTotal,
				"previousTotalBytes": previousTotal,
				"hasPrevious":        hasPrevious,
			}
			previousTotal = currentTotal
		}
	}
	return items
}

func (r *Router) listProtectionPlans(w http.ResponseWriter, req *http.Request) {
	clusterID := req.URL.Query().Get("clusterId")
	r.reconcileProtectionPlanActivationStates(clusterID)
	items, err := r.store.ListProtectionPlans(clusterID)
	if err != nil {
		r.logger.Error("failed to list protection plans", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_protection_plans_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) createProtectionPlan(w http.ResponseWriter, req *http.Request) {
	var input store.ProtectionPlanInput
	if err := decodeJSON(req, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if actor, ok := requestUser(req); ok {
		input.TenantID = actor.TenantID
	}
	if input.TenantID != "" {
		clusters, _ := r.store.ListClusters()
		clusterAllowed := func(id string) bool {
			if id == "" {
				return true
			}
			for _, item := range clusters {
				if item.ID == id && item.TenantID == input.TenantID {
					return true
				}
			}
			return false
		}
		storageItem, storageFound, _ := r.store.GetStorageRepository(input.StorageRepoID)
		policyAllowed := input.PolicyID == ""
		if !policyAllowed {
			policies, _ := r.store.ListPolicies()
			for _, item := range policies {
				if item.ID == input.PolicyID && item.TenantID == input.TenantID {
					policyAllowed = true
					break
				}
			}
		}
		appsAllowed := true
		seenApps := map[string]bool{}
		for _, appID := range append(append([]string{}, input.AppIDs...), input.AppID) {
			if appID == "" || seenApps[appID] {
				continue
			}
			seenApps[appID] = true
			app, found, _ := r.store.GetApplication(appID)
			if !found || !clusterAllowed(app.ClusterID) {
				appsAllowed = false
				break
			}
		}
		if !clusterAllowed(input.SourceClusterID) || !clusterAllowed(input.TargetClusterID) || !storageFound || storageItem.TenantID != input.TenantID || !policyAllowed || !appsAllowed {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource_not_found", "message": "One or more selected resources are not available in this tenant."})
			return
		}
	}
	if input.SourceClusterID == "" || (input.AppID == "" && len(input.AppIDs) == 0) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "source_cluster_id_and_apps_required"})
		return
	}
	if input.StorageRepoID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "storage_repository_required"})
		return
	}
	if input.ScopeType == "" {
		input.ScopeType = "all"
	}
	if input.ScopeType != "all" && input.ScopeType != "filtered" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_scope_type", "allowed": []string{"all", "filtered"}})
		return
	}
	if input.AppID == "" && len(input.AppIDs) > 0 {
		input.AppID = input.AppIDs[0]
	}
	input.Status = "activating_storage"
	item, err := r.store.CreateProtectionPlan(input)
	if err != nil {
		r.logger.Error("failed to create protection plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_protection_plan_failed"})
		return
	}
	var activationTask *store.Task
	var activationWarning string
	tasks, warning, err := r.dispatchProtectionPlanStorageActivation(item)
	if err != nil {
		r.logger.Error("failed to sync storage repository for protection plan", "cluster_id", input.SourceClusterID, "repository_id", input.StorageRepoID, "error", err)
		item, _, _ = r.store.UpdateProtectionPlanStatus(item.ID, "storage_failed")
		writeJSON(w, http.StatusCreated, protectionPlanActivationResponse(item, nil, "storage sync dispatch failed: "+err.Error()))
		return
	} else if warning != "" {
		r.logger.Warn("storage repository sync queued with warning", "cluster_id", input.SourceClusterID, "repository_id", input.StorageRepoID, "warning", warning)
		activationWarning = warning
	}
	if len(tasks) > 0 {
		activationTask = &tasks[0]
		if !storageActivationTasksIncludeSource(tasks) {
			r.continueProtectionPlanActivationAfterStorage(store.Task{
				ID:               "source-storage-already-synced",
				ProtectionPlanID: item.ID,
				Status:           "succeeded",
				CreatedAt:        time.Now().UTC(),
			})
			if refreshed, ok, err := r.store.GetProtectionPlan(item.ID); err == nil && ok {
				item = refreshed
			}
		}
	} else {
		r.continueProtectionPlanActivationAfterStorage(store.Task{
			ID:               "storage-already-synced",
			ProtectionPlanID: item.ID,
			Status:           "succeeded",
			CreatedAt:        time.Now().UTC(),
		})
		if refreshed, ok, err := r.store.GetProtectionPlan(item.ID); err == nil && ok {
			item = refreshed
		}
	}
	writeJSON(w, http.StatusCreated, protectionPlanActivationResponse(item, activationTask, activationWarning))
}

func (r *Router) activateProtectionPlan(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}
	item, ok, err := r.store.GetProtectionPlan(id)
	if err != nil {
		r.logger.Error("failed to load protection plan for activation", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
		return
	}
	switch item.Status {
	case "activating_storage", "activating_schedule":
		writeJSON(w, http.StatusAccepted, protectionPlanActivationResponse(item, nil, "activation is already in progress"))
		return
	}
	item, _, err = r.store.UpdateProtectionPlanStatus(item.ID, "activating_storage")
	if err != nil {
		r.logger.Error("failed to mark protection plan activating", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "update_protection_plan_failed"})
		return
	}
	if item.StorageRepoID == "" {
		item, _, _ = r.store.UpdateProtectionPlanStatus(item.ID, "storage_failed")
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "storage_repository_required"})
		return
	}
	tasks, warning, err := r.dispatchProtectionPlanStorageActivation(item)
	if err != nil {
		r.logger.Error("failed to dispatch protection plan activation", "id", id, "error", err)
		item, _, _ = r.store.UpdateProtectionPlanStatus(item.ID, "storage_failed")
		writeJSON(w, http.StatusAccepted, protectionPlanActivationResponse(item, nil, "storage sync dispatch failed: "+err.Error()))
		return
	}
	var activationTask *store.Task
	if len(tasks) > 0 {
		activationTask = &tasks[0]
		if !storageActivationTasksIncludeSource(tasks) {
			r.continueProtectionPlanActivationAfterStorage(store.Task{
				ID:               "source-storage-already-synced",
				ProtectionPlanID: item.ID,
				Status:           "succeeded",
				CreatedAt:        time.Now().UTC(),
			})
			if refreshed, ok, err := r.store.GetProtectionPlan(item.ID); err == nil && ok {
				item = refreshed
			}
		}
	} else {
		r.continueProtectionPlanActivationAfterStorage(store.Task{
			ID:               "storage-already-synced",
			ProtectionPlanID: item.ID,
			Status:           "succeeded",
			CreatedAt:        time.Now().UTC(),
		})
		if refreshed, ok, err := r.store.GetProtectionPlan(item.ID); err == nil && ok {
			item = refreshed
		}
	}
	writeJSON(w, http.StatusAccepted, protectionPlanActivationResponse(item, activationTask, warning))
}

func (r *Router) dispatchProtectionPlanStorageActivation(plan store.ProtectionPlan) ([]store.Task, string, error) {
	return r.dispatchProtectionPlanStorageTasks(plan, false)
}

func storageActivationTasksIncludeSource(tasks []store.Task) bool {
	for _, task := range tasks {
		role := taskPayloadString(task.Payload, "activationRole")
		if role == "" || role == "source" {
			return true
		}
	}
	return false
}

func (r *Router) reconfigureProtectionPlanStorage(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(id)
	if err != nil {
		r.logger.Error("failed to load protection plan for storage reconfigure", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
		return
	}
	if plan.StorageRepoID == "" {
		plan, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed")
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "storage_repository_required"})
		return
	}
	plan, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "activating_storage")
	tasks, warning, err := r.dispatchProtectionPlanStorageTasks(plan, true)
	if err != nil {
		r.logger.Error("failed to reconfigure protection plan storage", "id", id, "error", err)
		plan, _, _ = r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed")
		writeJSON(w, http.StatusAccepted, protectionPlanStorageReconfigureResponse(plan, tasks, "storage reconfigure dispatch failed: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusAccepted, protectionPlanStorageReconfigureResponse(plan, tasks, warning))
}

func protectionPlanStorageReconfigureResponse(plan store.ProtectionPlan, tasks []store.Task, warning string) map[string]any {
	response := protectionPlanActivationResponse(plan, nil, warning)
	response["storageTasks"] = nonNilSlice(tasks)
	return response
}

func (r *Router) dispatchProtectionPlanStorageTasks(plan store.ProtectionPlan, reconfigure bool) ([]store.Task, string, error) {
	attemptID := store.NewPublicID()
	targets := []struct {
		clusterID string
		role      string
	}{
		{clusterID: plan.SourceClusterID, role: "source"},
	}
	if plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID {
		targets = append(targets, struct {
			clusterID string
			role      string
		}{clusterID: plan.TargetClusterID, role: "target"})
	}
	tasks := make([]store.Task, 0, len(targets))
	warnings := []string{}
	for _, target := range targets {
		if !reconfigure {
			action, warning, err := r.storageBindingActivationAction(target.clusterID, plan.StorageRepoID, plan.SourceClusterID, target.role)
			if err != nil {
				return tasks, strings.Join(warnings, "; "), err
			}
			if warning != "" {
				warnings = append(warnings, target.role+" cluster: "+warning)
			}
			if action == "skip" {
				r.logger.Info("storage repository binding reused for protection plan activation", "plan_id", plan.ID, "cluster_id", target.clusterID, "repository_id", plan.StorageRepoID, "role", target.role)
				continue
			}
			if action == "wait" {
				r.logger.Info("storage repository binding already configuring for protection plan activation", "plan_id", plan.ID, "cluster_id", target.clusterID, "repository_id", plan.StorageRepoID, "role", target.role)
				continue
			}
		}
		task, warning, err := r.dispatchStorageSyncTaskForPlanActivationAttempt(target.clusterID, plan.StorageRepoID, plan.ID, plan.SourceClusterID, attemptID, target.role, reconfigure, 1)
		if err != nil {
			return tasks, strings.Join(warnings, "; "), err
		}
		tasks = append(tasks, task)
		if warning != "" {
			warnings = append(warnings, target.role+" cluster: "+warning)
		}
	}
	return tasks, strings.Join(warnings, "; "), nil
}

func (r *Router) storageBindingActivationAction(clusterID string, storageRepoID string, sourceClusterID string, role string) (string, string, error) {
	repo, ok, err := r.store.GetStorageRepository(storageRepoID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", errors.New("storage repository not found")
	}
	binding, ok, err := r.store.GetClusterStorageBinding(clusterID, storageRepoID, sourceClusterID)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "dispatch", "", nil
	}
	expectedBSLName := storageDomainBSLName(repo, sourceClusterID)
	expectedPrefix := storageDomainPrefix(sourceClusterID)
	if binding.BSLName != expectedBSLName || binding.ObjectPrefix != expectedPrefix {
		return "dispatch", "", nil
	}
	if strings.EqualFold(binding.Status, "ready") && binding.LastSuccessAt.After(repo.UpdatedAt.Add(-time.Second)) {
		return "skip", "", nil
	}
	if strings.EqualFold(binding.Status, "configuring") {
		return "wait", "storage binding is already being configured", nil
	}
	if strings.EqualFold(binding.Status, "failed") {
		message := binding.LastErrorMessage
		if message == "" {
			message = binding.LastErrorCode
		}
		if message == "" {
			message = "storage binding failed"
		}
		if role == "target" {
			return "wait", message, nil
		}
		return "", "", errors.New(message)
	}
	return "dispatch", "", nil
}

func protectionPlanActivationResponse(item store.ProtectionPlan, activationTask *store.Task, warning string) map[string]any {
	response := map[string]any{
		"id":                   item.ID,
		"tenantId":             item.TenantID,
		"sourceClusterId":      item.SourceClusterID,
		"appId":                item.AppID,
		"appIds":               nonNilSlice(item.AppIDs),
		"scopeType":            item.ScopeType,
		"includedResources":    nonNilSlice(item.IncludedResources),
		"labelSelector":        item.LabelSelector,
		"includeClusterScoped": item.IncludeClusterScoped,
		"storageRepoId":        item.StorageRepoID,
		"policyId":             item.PolicyID,
		"targetClusterId":      item.TargetClusterID,
		"excludedResources":    nonNilSlice(item.ExcludedResources),
		"preHooks":             nonNilSlice(item.PreHooks),
		"postHooks":            nonNilSlice(item.PostHooks),
		"status":               item.Status,
		"createdAt":            item.CreatedAt,
		"updatedAt":            item.UpdatedAt,
	}
	if activationTask != nil {
		response["activationTask"] = activationTask
	}
	if warning != "" {
		response["warning"] = warning
	}
	return response
}

func (r *Router) deleteProtectionPlan(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_id"})
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(id)
	if err != nil {
		r.logger.Error("failed to load protection plan before cleanup", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "load_protection_plan_failed", "message": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
		return
	}
	plan, _, err = r.store.UpdateProtectionPlanStatus(id, "cleanup_running")
	if err != nil {
		r.logger.Error("failed to mark protection plan cleanup running", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "mark_cleanup_running_failed", "message": err.Error()})
		return
	}
	cleanupTask, cleanupWarning, err := r.createProtectionCleanupTask(plan)
	if err != nil {
		r.logger.Error("failed to create protection cleanup task", "error", err, "id", id)
		_, _, _ = r.store.UpdateProtectionPlanStatus(id, "cleanup_failed")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_protection_cleanup_failed", "message": err.Error()})
		return
	}
	response := protectionPlanActivationResponse(plan, nil, cleanupWarning)
	if cleanupTask.ID != "" {
		response["cleanupTask"] = cleanupTask
	}
	writeJSON(w, http.StatusOK, response)
}

func (r *Router) dispatchScheduleSyncTask(plan store.ProtectionPlan) (store.Task, string, error) {
	if plan.PolicyID == "" {
		return store.Task{}, "", nil
	}
	if existing, ok, err := r.existingScheduleSyncTask(plan.ID); err != nil {
		return store.Task{}, "", err
	} else if ok {
		return existing, "", nil
	}
	policy, ok, err := r.findPolicy(plan.PolicyID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok || policy.Status != "active" || policy.ScheduleType == "manual" {
		return store.Task{}, "", nil
	}
	cron, err := policyCron(policy)
	if err != nil {
		return store.Task{}, "", err
	}
	sourceNamespaces := []string{}
	appIDs := plan.AppIDs
	if len(appIDs) == 0 && plan.AppID != "" {
		appIDs = []string{plan.AppID}
	}
	for _, appID := range appIDs {
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return store.Task{}, "", err
		}
		if ok && app.Namespace != "" && !slices.Contains(sourceNamespaces, app.Namespace) {
			sourceNamespaces = append(sourceNamespaces, app.Namespace)
		}
	}
	if len(sourceNamespaces) == 0 {
		return store.Task{}, "", errors.New("protection plan has no application namespaces")
	}
	repo, ok, err := r.store.GetStorageRepository(plan.StorageRepoID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok {
		return store.Task{}, "", errors.New("storage repository not found")
	}
	storageName := storageDomainBSLName(repo, plan.SourceClusterID)
	commandID := store.NewPublicID()
	payload := map[string]any{
		"planId":                  plan.ID,
		"scheduleName":            scheduleNameForPlan(plan.ID),
		"cron":                    cron,
		"sourceNamespaces":        sourceNamespaces,
		"scope":                   plan.ScopeType,
		"includedResources":       plan.IncludedResources,
		"labelSelector":           plan.LabelSelector,
		"storageRepoId":           repo.ID,
		"storageRepo":             storageName,
		"storageRepoDisplayName":  repo.Name,
		"sourceClusterId":         plan.SourceClusterID,
		"objectPrefix":            storageDomainPrefix(plan.SourceClusterID),
		"includeClusterResources": plan.IncludeClusterScoped,
		"excludedResources":       plan.ExcludedResources,
		"retentionCount":          policy.RetentionCount,
	}
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: plan.ID,
		Type:             "schedule-sync",
		Status:           "queued",
		CommandID:        commandID,
		Payload:          payload,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	conn, ok := r.hub.get(plan.SourceClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; schedule sync will be dispatched after reconnect",
		})
		return task, "agent is offline; schedule sync remains queued", nil
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		return task, "schedule sync task created but dispatch failed", nil
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "velero schedule sync dispatched to agent",
	})
	return task, "", nil
}

func (r *Router) existingScheduleSyncTask(planID string) (store.Task, bool, error) {
	if planID == "" {
		return store.Task{}, false, nil
	}
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return store.Task{}, false, err
	}
	var latest store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "schedule-sync" {
			continue
		}
		if !isCompletedTaskStatus(task.Status) && !isActiveTaskStatus(task.Status) {
			continue
		}
		if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	if latest.ID == "" {
		return store.Task{}, false, nil
	}
	return latest, true, nil
}

func (r *Router) findPolicy(policyID string) (store.Policy, bool, error) {
	policies, err := r.store.ListPolicies()
	if err != nil {
		return store.Policy{}, false, err
	}
	for _, policy := range policies {
		if policy.ID == policyID {
			return policy, true, nil
		}
	}
	return store.Policy{}, false, nil
}

func (r *Router) protectionPlanSchedulePolicy(plan store.ProtectionPlan) (store.Policy, bool, error) {
	if plan.PolicyID == "" {
		return store.Policy{}, false, nil
	}
	policy, ok, err := r.findPolicy(plan.PolicyID)
	if err != nil {
		return store.Policy{}, false, err
	}
	if !ok {
		return store.Policy{}, false, errors.New("policy not found")
	}
	if policy.Status != "active" || policy.ScheduleType == "manual" {
		return policy, false, nil
	}
	if _, err := policyCron(policy); err != nil {
		return policy, false, err
	}
	return policy, true, nil
}

func policyCron(policy store.Policy) (string, error) {
	switch policy.ScheduleType {
	case "interval":
		value := policy.IntervalValue
		if value <= 0 {
			return "", errors.New("interval policy value must be greater than zero")
		}
		switch strings.ToLower(policy.IntervalUnit) {
		case "minute", "minutes":
			if value == 1 {
				return "* * * * *", nil
			}
			if value > 59 {
				return "", errors.New("minute interval must be between 1 and 59")
			}
			return fmt.Sprintf("*/%d * * * *", value), nil
		case "hour", "hours", "":
			if value == 1 {
				return "0 * * * *", nil
			}
			if value > 23 {
				return "", errors.New("hour interval must be between 1 and 23")
			}
			return fmt.Sprintf("0 */%d * * *", value), nil
		default:
			return "", errors.New("unsupported interval unit: " + policy.IntervalUnit)
		}
	case "daily":
		return fmt.Sprintf("%d %d * * *", clampMinute(policy.Minute), clampHour(policy.Hour)), nil
	case "weekly":
		return fmt.Sprintf("%d %d * * %d", clampMinute(policy.Minute), clampHour(policy.Hour), clampWeekday(policy.WeekDay)), nil
	case "monthly":
		return fmt.Sprintf("%d %d %d * *", clampMinute(policy.Minute), clampHour(policy.Hour), clampMonthDay(policy.MonthDay)), nil
	default:
		return "", errors.New("unsupported schedule type: " + policy.ScheduleType)
	}
}

func scheduleNameForPlan(planID string) string {
	return "hcdr-" + planUUIDNoDash(planID)
}

func manualBackupNameForPlan(planID string, runID string) string {
	return backupNameForPlan(planID, runID, "manual")
}

func backupNameForPlan(planID string, runID string, trigger string) string {
	base := scheduleNameForPlan(planID)
	if trigger == "manual" || trigger == "" {
		base += "-m"
	}
	base += "-" + time.Now().UTC().Format("20060102150405")
	suffix := strings.ReplaceAll(runID, "-", "")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix != "" {
		base += "-" + suffix
	}
	if len(base) > 63 {
		base = strings.Trim(base[:63], "-")
	}
	return base
}

func planUUIDNoDash(planID string) string {
	id := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(planID), "-", ""))
	if id == "" {
		return "unknown"
	}
	return id
}

func clampMinute(value int) int {
	if value < 0 || value > 59 {
		return 0
	}
	return value
}

func clampHour(value int) int {
	if value < 0 || value > 23 {
		return 0
	}
	return value
}

func clampWeekday(value int) int {
	if value < 0 || value > 6 {
		return 0
	}
	return value
}

func clampMonthDay(value int) int {
	if value < 1 || value > 31 {
		return 1
	}
	return value
}

func (r *Router) listRestorePoints(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListRestorePoints(store.RestorePointFilter{
		ClusterID:        req.URL.Query().Get("clusterId"),
		AppID:            req.URL.Query().Get("appId"),
		ProtectionPlanID: req.URL.Query().Get("protectionPlanId"),
	})
	if err != nil {
		r.logger.Error("failed to list restore points", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_restore_points_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	items = enrichRestorePointStorageIncrements(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) deleteRestorePoints(w http.ResponseWriter, req *http.Request) {
	var body struct {
		RestorePointIDs []string `json:"restorePointIds"`
		RestorePointID  string   `json:"restorePointId"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	ids := append([]string{}, body.RestorePointIDs...)
	if body.RestorePointID != "" {
		ids = append(ids, body.RestorePointID)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "restore_point_id_required"})
		return
	}

	pointsByCluster := map[string][]store.RestorePoint{}
	for _, id := range ids {
		point, ok, err := r.store.GetRestorePoint(id)
		if err != nil {
			r.logger.Error("failed to get restore point for delete", "restore_point_id", id, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_restore_point_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found", "restorePointId": id})
			return
		}
		if !tenantVisible(req, point.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found", "restorePointId": id})
			return
		}
		if point.Status == "deleted" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_already_deleted", "restorePointId": id})
			return
		}
		if point.VeleroBackupName == "" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_backup_name_required", "restorePointId": id})
			return
		}
		if state, _ := point.Metadata["retentionState"].(string); state == "deleting" || state == "pending_delete" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "restore_point_delete_in_progress", "restorePointId": id})
			return
		}
		pointsByCluster[point.SourceClusterID] = append(pointsByCluster[point.SourceClusterID], point)
	}

	tasks := make([]store.Task, 0, len(pointsByCluster))
	warnings := []string{}
	for clusterID, points := range pointsByCluster {
		task, warning, err := r.createRestorePointDeleteTask(clusterID, points)
		if err != nil {
			r.logger.Error("failed to create restore point delete task", "cluster_id", clusterID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_delete_task_failed"})
			return
		}
		tasks = append(tasks, task)
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}

	statusCode := http.StatusAccepted
	response := map[string]any{"tasks": tasks}
	if len(tasks) == 1 {
		response["task"] = tasks[0]
	}
	if len(warnings) > 0 {
		response["warning"] = strings.Join(warnings, "; ")
	}
	writeJSON(w, statusCode, response)
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (r *Router) listTasks(w http.ResponseWriter, req *http.Request) {
	items, err := r.store.ListTasks(req.URL.Query().Get("clusterId"))
	if err != nil {
		r.logger.Error("failed to list tasks", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_tasks_failed"})
		return
	}
	visible := items[:0]
	for _, item := range items {
		if tenantVisible(req, item.TenantID) {
			visible = append(visible, item)
		}
	}
	items = visible
	items = r.enrichCleanupTaskRestorePointTimes(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

// enrichCleanupTaskRestorePointTimes keeps restore-point labels derivable for
// historical cleanup tasks after their restore-point records stop appearing in
// the normal active restore-point list. The UI converts this UTC instant using
// its currently selected timezone; no persisted display label is involved.
func (r *Router) enrichCleanupTaskRestorePointTimes(items []store.Task) []store.Task {
	for index := range items {
		if items[index].Type != "retention-cleanup" && items[index].Type != "protection-cleanup" {
			continue
		}
		rawPoints, ok := items[index].Payload["restorePoints"].([]any)
		if !ok || len(rawPoints) == 0 {
			continue
		}
		payload := make(map[string]any, len(items[index].Payload))
		for key, value := range items[index].Payload {
			payload[key] = value
		}
		points := append([]any(nil), rawPoints...)
		changed := false
		for pointIndex, rawPoint := range points {
			pointPayload, ok := rawPoint.(map[string]any)
			if !ok {
				continue
			}
			if taskCreatedAt, exists := pointPayload["taskCreatedAt"]; exists && taskCreatedAt != nil && strings.TrimSpace(fmt.Sprint(taskCreatedAt)) != "" {
				continue
			}
			id := strings.TrimSpace(fmt.Sprint(pointPayload["id"]))
			if id == "" {
				continue
			}
			point, found, err := r.store.GetRestorePoint(id)
			if err != nil || !found || point.TaskCreatedAt.IsZero() {
				continue
			}
			enriched := make(map[string]any, len(pointPayload)+1)
			for key, value := range pointPayload {
				enriched[key] = value
			}
			enriched["taskCreatedAt"] = point.TaskCreatedAt
			points[pointIndex] = enriched
			changed = true
		}
		if changed {
			payload["restorePoints"] = points
			items[index].Payload = payload
		}
	}
	return items
}

func (r *Router) listTaskEvents(w http.ResponseWriter, req *http.Request) {
	taskID := req.PathValue("id")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_id_required"})
		return
	}

	items, err := r.store.ListTaskEvents(taskID)
	if err != nil {
		r.logger.Error("failed to list task events", "task_id", taskID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "list_task_events_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilSlice(items)})
}

func (r *Router) cancelTask(w http.ResponseWriter, req *http.Request) {
	taskID := req.PathValue("id")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "task_id_required"})
		return
	}
	task, ok, err := r.findTaskByID("", taskID)
	if err != nil {
		r.logger.Error("failed to load task for cancel", "task_id", taskID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_task_failed"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "task_not_found"})
		return
	}
	if task.Type != "backup" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "task_cancel_unsupported", "message": "Only running sync tasks can be force stopped."})
		return
	}
	if !isActiveTaskStatus(task.Status) || !task.CompletedAt.IsZero() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "task_not_active", "message": "This sync task is no longer active."})
		return
	}
	if task.Status == "canceling" {
		writeJSON(w, http.StatusOK, map[string]any{"task": task, "warning": "Force stop is already in progress.", "reused": true})
		return
	}
	cancelCommandID := store.NewPublicID()
	cancelTask, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        task.ClusterID,
		AppID:            task.AppID,
		ProtectionPlanID: task.ProtectionPlanID,
		Type:             "backup-cancel",
		Status:           "queued",
		CommandID:        cancelCommandID,
		Payload: map[string]any{
			"requestedBy":      requestActor(req),
			"targetTaskId":     task.ID,
			"planId":           task.ProtectionPlanID,
			"veleroBackupName": stringPayload(task.Payload, "veleroBackupName"),
			"reason":           "user_requested",
		},
	})
	if err != nil {
		r.logger.Error("failed to create backup cancel task", "task_id", task.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_cancel_task_failed"})
		return
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       task.ID,
		Status:       "canceling",
		Progress:     task.Progress,
		ErrorCode:    "SYNC_CANCEL_REQUESTED",
		ErrorMessage: "Force stop requested by user.",
		Payload: map[string]any{
			"cancelTaskId": cancelTask.ID,
			"cancelReason": "user_requested",
		},
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "warning",
		Reason:  "cancel_requested",
		Message: "Force stop requested by user.",
		Payload: map[string]any{"cancelTaskId": cancelTask.ID},
	})
	conn, online := r.hub.get(task.ClusterID)
	if !online {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       cancelTask.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; force stop will be dispatched after reconnect",
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "cancelTask": cancelTask, "warning": "Agent is offline; force stop is queued."})
		return
	}
	if err := r.dispatchStoredTask(conn, cancelTask); err != nil {
		r.logger.Error("failed to dispatch backup cancel task", "task_id", task.ID, "cancel_task_id", cancelTask.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       cancelTask.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "cancelTask": cancelTask, "warning": "Force stop is queued."})
		return
	}
	cancelTask, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   cancelTask.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  cancelTask.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "Force stop dispatched to source cluster agent.",
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "cancelTask": cancelTask})
}

func (r *Router) createBackupTask(w http.ResponseWriter, req *http.Request) {
	var body backupTaskRequest
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if body.ClusterID == "" || (body.SourceNamespace == "" && body.AppID == "" && body.ProtectionPlanID == "") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_and_target_required"})
		return
	}
	if body.Scope == "" {
		body.Scope = "all"
	}
	if body.StorageRepo == "" {
		body.StorageRepo = "default"
	}
	if body.Trigger == "" {
		body.Trigger = "manual"
	}
	body.RequestedBy = requestActor(req)

	// When a protection plan is provided, expand to one task per app on the plan.
	type planApp struct {
		appID                   string
		ns                      string
		storage                 string
		storageRepoID           string
		scope                   string
		includedResources       []string
		labelSelector           store.LabelSelector
		excludedResources       []string
		includeClusterResources bool
	}
	targets := []planApp{}
	seen := map[string]struct{}{}
	if body.ProtectionPlanID != "" {
		plan, ok, err := r.store.GetProtectionPlan(body.ProtectionPlanID)
		if err != nil {
			r.logger.Error("failed to load protection plan", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
			return
		}
		if !tenantVisible(req, plan.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
			return
		}
		if !protectionPlanAllowsBackup(plan.Status) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "protection_plan_not_active", "status": plan.Status})
			return
		}
		body.ClusterID = plan.SourceClusterID
		repo, hasRepo, _ := r.store.GetStorageRepository(plan.StorageRepoID)
		storageName := body.StorageRepo
		if hasRepo {
			storageName = storageDomainBSLName(repo, plan.SourceClusterID)
		}
		if body.AppID == "" && body.SourceNamespace == "" {
			sourceNamespaces := []string{}
			appIDs := plan.AppIDs
			if len(appIDs) == 0 && plan.AppID != "" {
				appIDs = []string{plan.AppID}
			}
			for _, appID := range appIDs {
				if appID == "" {
					continue
				}
				if _, dup := seen[appID]; dup {
					continue
				}
				app, ok, _ := r.store.GetApplication(appID)
				if !ok || app.Namespace == "" {
					continue
				}
				seen[appID] = struct{}{}
				if !slices.Contains(sourceNamespaces, app.Namespace) {
					sourceNamespaces = append(sourceNamespaces, app.Namespace)
				}
			}
			if len(sourceNamespaces) == 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no_resolvable_applications"})
				return
			}
			body.ProtectionPlanID = plan.ID
			body.SourceNamespace = sourceNamespaces[0]
			body.SourceNamespaces = sourceNamespaces
			body.StorageRepo = storageName
			body.Scope = plan.ScopeType
			body.IncludedResources = plan.IncludedResources
			body.LabelSelector = plan.LabelSelector
			body.ExcludedResources = plan.ExcludedResources
			body.IncludeClusterResources = plan.IncludeClusterScoped
			if existing, ok, err := r.findActiveBackupTask(body.ClusterID, body.ProtectionPlanID, "", ""); err != nil {
				r.logger.Error("failed to check active plan backup task", "cluster_id", body.ClusterID, "protection_plan_id", body.ProtectionPlanID, "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "check_active_backup_failed"})
				return
			} else if ok {
				writeJSON(w, http.StatusOK, map[string]any{"task": existing, "warning": "Sync is already running.", "reused": true})
				return
			}
			task, err := r.createPendingBackupTask(body, "")
			if err != nil {
				r.logger.Error("failed to create plan backup task", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
				return
			}
			go r.dispatchBackupTaskAfterStorageSync(task, storageName, plan.StorageRepoID, plan.SourceClusterID)
			writeJSON(w, http.StatusCreated, map[string]any{"task": task})
			return
		}
		for _, appID := range plan.AppIDs {
			if appID == "" {
				continue
			}
			if _, dup := seen[appID]; dup {
				continue
			}
			app, ok, _ := r.store.GetApplication(appID)
			if !ok {
				continue
			}
			seen[appID] = struct{}{}
			targets = append(targets, planApp{
				appID:                   appID,
				ns:                      app.Namespace,
				storage:                 storageName,
				storageRepoID:           plan.StorageRepoID,
				scope:                   plan.ScopeType,
				includedResources:       plan.IncludedResources,
				labelSelector:           plan.LabelSelector,
				excludedResources:       plan.ExcludedResources,
				includeClusterResources: plan.IncludeClusterScoped,
			})
		}
	} else {
		plan, ok, err := r.findActiveProtectionPlanForBackupTarget(body.ClusterID, body.AppID, body.SourceNamespace)
		if err != nil {
			r.logger.Error("failed to resolve protection plan for backup target", "cluster_id", body.ClusterID, "app_id", body.AppID, "namespace", body.SourceNamespace, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "resolve_protection_plan_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "protection_plan_required"})
			return
		}
		if !tenantVisible(req, plan.TenantID) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "protection_plan_required"})
			return
		}
		body.ProtectionPlanID = plan.ID
		body.ClusterID = plan.SourceClusterID
		repo, hasRepo, _ := r.store.GetStorageRepository(plan.StorageRepoID)
		storageName := body.StorageRepo
		if hasRepo {
			storageName = storageDomainBSLName(repo, plan.SourceClusterID)
		}
		appIDs := plan.AppIDs
		if len(appIDs) == 0 && plan.AppID != "" {
			appIDs = []string{plan.AppID}
		}
		for _, appID := range appIDs {
			if appID == "" {
				continue
			}
			app, ok, _ := r.store.GetApplication(appID)
			if !ok {
				continue
			}
			if body.AppID != "" && app.ID != body.AppID {
				continue
			}
			if body.SourceNamespace != "" && app.Namespace != body.SourceNamespace {
				continue
			}
			if _, dup := seen[app.ID]; dup {
				continue
			}
			seen[app.ID] = struct{}{}
			targets = append(targets, planApp{
				appID:                   app.ID,
				ns:                      app.Namespace,
				storage:                 storageName,
				storageRepoID:           plan.StorageRepoID,
				scope:                   plan.ScopeType,
				includedResources:       plan.IncludedResources,
				labelSelector:           plan.LabelSelector,
				excludedResources:       plan.ExcludedResources,
				includeClusterResources: plan.IncludeClusterScoped,
			})
		}
	}
	if len(targets) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no_resolvable_applications"})
		return
	}

	tasks := make([]store.Task, 0, len(targets))
	reused := 0
	for _, tgt := range targets {
		body.SourceNamespace = tgt.ns
		body.StorageRepo = tgt.storage
		body.Scope = tgt.scope
		body.IncludedResources = tgt.includedResources
		body.LabelSelector = tgt.labelSelector
		body.ExcludedResources = tgt.excludedResources
		body.IncludeClusterResources = tgt.includeClusterResources
		if existing, ok, err := r.findActiveBackupTask(body.ClusterID, body.ProtectionPlanID, tgt.appID, tgt.ns); err != nil {
			r.logger.Error("failed to check active backup tasks", "cluster_id", body.ClusterID, "protection_plan_id", body.ProtectionPlanID, "namespace", tgt.ns, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "check_active_backup_failed"})
			return
		} else if ok {
			reused++
			tasks = append(tasks, existing)
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  existing.ID,
				Level:   "info",
				Reason:  "sync_already_running",
				Message: "Sync is already running.",
			})
			continue
		}
		task, err := r.createPendingBackupTask(body, tgt.appID)
		if err != nil {
			r.logger.Error("failed to create backup task", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
			return
		}
		tasks = append(tasks, task)
		go r.dispatchBackupTaskAfterStorageSync(task, tgt.storage, tgt.storageRepoID, body.ClusterID)
	}
	statusCode := http.StatusCreated
	warning := ""
	if reused > 0 {
		warning = "Sync is already running."
		if reused == len(tasks) {
			statusCode = http.StatusOK
		}
	}
	if len(tasks) == 1 {
		response := map[string]any{"task": tasks[0]}
		if warning != "" {
			response["warning"] = warning
			response["reused"] = true
		}
		writeJSON(w, statusCode, response)
		return
	}
	response := map[string]any{"tasks": tasks}
	if warning != "" {
		response["warning"] = warning
		response["reused"] = reused
	}
	writeJSON(w, statusCode, response)
}

func (r *Router) findActiveProtectionPlanForBackupTarget(clusterID string, appID string, namespace string) (store.ProtectionPlan, bool, error) {
	if clusterID == "" {
		return store.ProtectionPlan{}, false, nil
	}
	if appID != "" {
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return store.ProtectionPlan{}, false, err
		}
		if ok && app.ClusterID == clusterID && namespace == "" {
			namespace = app.Namespace
		}
	}
	plans, err := r.store.ListProtectionPlans(clusterID)
	if err != nil {
		return store.ProtectionPlan{}, false, err
	}
	for _, plan := range plans {
		if !protectionPlanAllowsBackup(plan.Status) {
			continue
		}
		appIDs := plan.AppIDs
		if len(appIDs) == 0 && plan.AppID != "" {
			appIDs = []string{plan.AppID}
		}
		if appID != "" && slices.Contains(appIDs, appID) {
			return plan, true, nil
		}
		if namespace == "" {
			continue
		}
		for _, planAppID := range appIDs {
			app, ok, err := r.store.GetApplication(planAppID)
			if err != nil {
				return store.ProtectionPlan{}, false, err
			}
			if ok && app.ClusterID == clusterID && app.Namespace == namespace {
				return plan, true, nil
			}
		}
	}
	return store.ProtectionPlan{}, false, nil
}

func (r *Router) planSourceNamespaces(plan store.ProtectionPlan) ([]string, []string, error) {
	sourceNamespaces := []string{}
	appIDs := plan.AppIDs
	if len(appIDs) == 0 && plan.AppID != "" {
		appIDs = []string{plan.AppID}
	}
	resolvedAppIDs := []string{}
	for _, appID := range appIDs {
		if appID == "" || slices.Contains(resolvedAppIDs, appID) {
			continue
		}
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return nil, nil, err
		}
		if !ok || app.Namespace == "" {
			continue
		}
		resolvedAppIDs = append(resolvedAppIDs, appID)
		if !slices.Contains(sourceNamespaces, app.Namespace) {
			sourceNamespaces = append(sourceNamespaces, app.Namespace)
		}
	}
	if len(sourceNamespaces) == 0 {
		return nil, nil, errors.New("protection plan has no application namespaces")
	}
	return sourceNamespaces, resolvedAppIDs, nil
}

func (r *Router) findActiveBackupTask(clusterID string, protectionPlanID string, appID string, namespace string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.Type != "backup" || !isActiveTaskStatus(task.Status) {
			continue
		}
		if !task.CompletedAt.IsZero() {
			continue
		}
		if protectionPlanID != "" && task.ProtectionPlanID != protectionPlanID {
			continue
		}
		if appID != "" && task.AppID != "" && task.AppID != appID {
			continue
		}
		taskNamespace := taskPayloadString(task.Payload, "sourceNamespace")
		if namespace != "" && taskNamespace != "" && taskNamespace != namespace {
			continue
		}
		if protectionPlanID != "" || appID != "" || namespace != "" {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) createPendingBackupTask(body backupTaskRequest, appID string) (store.Task, error) {
	commandID := store.NewPublicID()
	veleroBackupName := ""
	if body.ProtectionPlanID != "" {
		veleroBackupName = backupNameForPlan(body.ProtectionPlanID, commandID, body.Trigger)
	}
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        body.ClusterID,
		AppID:            appID,
		ProtectionPlanID: body.ProtectionPlanID,
		Type:             "backup",
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"sourceNamespace":         body.SourceNamespace,
			"sourceNamespaces":        body.SourceNamespaces,
			"scope":                   body.Scope,
			"includedResources":       body.IncludedResources,
			"labelSelector":           body.LabelSelector,
			"storageRepo":             body.StorageRepo,
			"excludedResources":       body.ExcludedResources,
			"includeClusterResources": body.IncludeClusterResources,
			"veleroBackupName":        veleroBackupName,
			"trigger":                 body.Trigger,
			"scheduled":               body.Trigger == "scheduled",
			"requestedBy":             firstNonEmptyString(body.RequestedBy, "System"),
		},
	})
	if err != nil {
		return store.Task{}, err
	}
	return task, nil
}

func (r *Router) dispatchBackupTaskAfterStorageSync(task store.Task, storageName string, storageRepoID string, sourceClusterID string) {
	if storageRepoID == "" {
		r.dispatchBackupTask(task)
		return
	}
	if r.isStorageAlreadySynced(task.ClusterID, storageName, storageRepoID, sourceClusterID) {
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "storage_preflight_skipped",
			Message: "Storage location already configured on source cluster.",
		})
		r.dispatchBackupTask(task)
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "storage_preflight_started",
		Message: "Configuring storage...",
	})
	storageTask, err := r.ensureStorageSynced(context.Background(), task.ClusterID, storageName, storageRepoID, sourceClusterID)
	if err != nil {
		r.logger.Error("storage sync preflight failed before backup dispatch", "cluster_id", task.ClusterID, "task_id", task.ID, "storage_repo", storageName, "storage_task_id", storageTask.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "failed",
			Progress:     0,
			ErrorCode:    "STORAGE_SYNC_FAILED",
			ErrorMessage: err.Error(),
			MarkDone:     true,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "error",
			Reason:  "storage_preflight_failed",
			Message: err.Error(),
			Payload: map[string]any{"storageTaskId": storageTask.ID},
		})
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "storage_preflight_succeeded",
		Message: "Dispatching sync task...",
		Payload: map[string]any{"storageTaskId": storageTask.ID},
	})
	r.dispatchBackupTask(task)
}

func (r *Router) dispatchBackupTask(task store.Task) {
	conn, ok := r.hub.get(task.ClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; backup will be dispatched after reconnect",
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "warning",
			Reason:  "dispatch_waiting_agent",
			Message: "Dispatching sync task...",
		})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch backup task after storage preflight", "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "warning",
			Reason:  "dispatch_failed",
			Message: "Dispatching sync task...",
		})
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "Dispatching sync task...",
	})
}

func (r *Router) createRestoreTask(w http.ResponseWriter, req *http.Request) {
	r.createRecoveryTask(w, req, "restore")
}

func (r *Router) createDrillTask(w http.ResponseWriter, req *http.Request) {
	r.createRecoveryTask(w, req, "drill")
}

func (r *Router) createTakeoverTask(w http.ResponseWriter, req *http.Request) {
	r.createRecoveryTask(w, req, "takeover")
}

func (r *Router) createRecoveryTask(w http.ResponseWriter, req *http.Request, taskType string) {
	var body struct {
		ClusterID                  string            `json:"clusterId"`
		ProtectionPlanID           string            `json:"protectionPlanId"`
		RestorePointID             string            `json:"restorePointId"`
		VeleroBackupName           string            `json:"veleroBackupName"`
		StorageRepo                string            `json:"storageRepo"`
		SourceNamespace            string            `json:"sourceNamespace"`
		SourceNamespaces           []string          `json:"sourceNamespaces"`
		TargetNamespace            string            `json:"targetNamespace"`
		TargetNamespaces           map[string]string `json:"targetNamespaces"`
		TargetMode                 string            `json:"targetMode"`
		RestoreMode                string            `json:"restoreMode"`
		ArtifactMode               string            `json:"artifactMode"`
		ConflictPolicy             string            `json:"conflictPolicy"`
		OriginalNamespaceConfirmed bool              `json:"originalNamespaceConfirmed"`
		IncludeClusterScoped       bool              `json:"includeClusterScoped"`
		UseTransforms              bool              `json:"useTransforms"`
		TransformPreset            string            `json:"transformPreset"`
		StorageProfileMode         string            `json:"storageProfileMode"`
		AlternateProfileID         string            `json:"alternateProfileId"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	if body.ClusterID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_id_required"})
		return
	}
	if _, authenticated := requestUser(req); authenticated {
		if !r.clusterVisible(req, body.ClusterID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "cluster_not_found"})
			return
		}
	}
	storageRepoID := ""
	storageSourceClusterID := ""
	protectionPlanID := body.ProtectionPlanID
	if protectionPlanID != "" {
		plan, found, err := r.store.GetProtectionPlan(protectionPlanID)
		if err != nil {
			r.logger.Error("failed to get protection plan for recovery", "protection_plan_id", protectionPlanID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_protection_plan_failed"})
			return
		}
		if !found || !tenantVisible(req, plan.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "protection_plan_not_found"})
			return
		}
	}
	if body.RestorePointID != "" {
		point, ok, err := r.store.GetRestorePoint(body.RestorePointID)
		if err != nil {
			r.logger.Error("failed to get restore point", "restore_point_id", body.RestorePointID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "get_restore_point_failed"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found"})
			return
		}
		if !tenantVisible(req, point.TenantID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "restore_point_not_found"})
			return
		}
		if !strings.EqualFold(strings.TrimSpace(point.Status), "available") {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "restore_point_not_available",
				"message": "The selected restore point is no longer available. Refresh restore points and select an available recovery point.",
				"status":  point.Status,
			})
			return
		}
		protectionPlanID = point.ProtectionPlanID
		if body.VeleroBackupName == "" {
			body.VeleroBackupName = point.VeleroBackupName
		}
		if body.StorageRepo == "" {
			body.StorageRepo = point.BackupStorageName
		}
		storageRepoID = point.StorageRepoID
		storageSourceClusterID = point.SourceClusterID
		if storageRepoID != "" {
			if repo, ok, err := r.store.GetStorageRepository(storageRepoID); err == nil && ok {
				body.StorageRepo = storageDomainBSLName(repo, point.SourceClusterID)
			} else if err != nil {
				r.logger.Warn("failed to load restore point storage repository", "restore_point_id", point.ID, "repository_id", storageRepoID, "error", err)
			}
		}
		if body.SourceNamespace == "" {
			body.SourceNamespace = point.SourceNamespace
		}
		if len(body.SourceNamespaces) == 0 {
			body.SourceNamespaces = stringArrayFromAny(point.Metadata["includedNamespaces"])
		}
		if body.TargetNamespace == "" {
			body.TargetNamespace = point.SourceNamespace
		}
	}
	if len(body.SourceNamespaces) == 0 && body.SourceNamespace != "" {
		body.SourceNamespaces = []string{body.SourceNamespace}
	}
	if body.VeleroBackupName == "" || body.SourceNamespace == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "velero_backup_name_and_source_namespace_required"})
		return
	}
	if body.TargetNamespace == "" {
		body.TargetNamespace = body.SourceNamespace
	}
	if body.RestoreMode == "" {
		body.RestoreMode = taskType
	}
	if body.RestoreMode != "full" && body.RestoreMode != "dataOnly" && body.RestoreMode != "restore" && body.RestoreMode != "drill" && body.RestoreMode != "takeover" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_restore_mode"})
		return
	}
	if body.ConflictPolicy == "" {
		body.ConflictPolicy = "none"
	}
	if body.RestoreMode == "dataOnly" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "data_only_restore_not_enabled"})
		return
	}
	if body.TargetNamespace == body.SourceNamespace && !body.OriginalNamespaceConfirmed {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "original_namespace_confirmation_required"})
		return
	}
	if body.TargetMode == "" {
		body.TargetMode = "same-namespace"
	}
	if taskType == "drill" {
		activeTask, found, err := r.findActiveRecoveryTask("drill", body.ClusterID, body.SourceNamespace)
		if err != nil {
			r.logger.Error("failed to check active drill tasks", "cluster_id", body.ClusterID, "source_namespace", body.SourceNamespace, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "active_drill_check_failed"})
			return
		}
		if found {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "active_drill_task_exists",
				"message": "A drill task is already running for this application. Wait for it to finish before starting another drill.",
				"taskId":  activeTask.ID,
				"status":  activeTask.Status,
			})
			return
		}
	}

	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        body.ClusterID,
		ProtectionPlanID: protectionPlanID,
		RestorePointID:   body.RestorePointID,
		Type:             taskType,
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"requestedBy":                requestActor(req),
			"protectionPlanId":           protectionPlanID,
			"restorePointId":             body.RestorePointID,
			"veleroBackupName":           body.VeleroBackupName,
			"storageRepo":                body.StorageRepo,
			"sourceNamespace":            body.SourceNamespace,
			"sourceNamespaces":           body.SourceNamespaces,
			"targetNamespace":            body.TargetNamespace,
			"targetNamespaces":           body.TargetNamespaces,
			"targetMode":                 body.TargetMode,
			"restoreMode":                body.RestoreMode,
			"artifactMode":               body.ArtifactMode,
			"conflictPolicy":             body.ConflictPolicy,
			"originalNamespaceConfirmed": body.OriginalNamespaceConfirmed,
			"includeClusterScoped":       body.IncludeClusterScoped,
			"useTransforms":              body.UseTransforms,
			"transformPreset":            body.TransformPreset,
			"storageProfileMode":         body.StorageProfileMode,
			"alternateProfileId":         body.AlternateProfileID,
		},
	})
	if err != nil {
		r.logger.Error("failed to create recovery task", "task_type", taskType, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "create_task_failed"})
		return
	}
	go r.dispatchRecoveryTaskAfterStorageSync(task, body.StorageRepo, storageRepoID, storageSourceClusterID)
	writeJSON(w, http.StatusCreated, task)
}

func (r *Router) findActiveRecoveryTask(taskType string, clusterID string, sourceNamespace string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.Type != taskType || !isActiveTaskStatus(task.Status) {
			continue
		}
		taskSourceNamespace := stringPayload(task.Payload, "sourceNamespace")
		if taskSourceNamespace == sourceNamespace {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func isActiveTaskStatus(status string) bool {
	switch strings.ToLower(status) {
	case "queued", "dispatched", "accepted", "running", "syncing", "finalizing", "canceling":
		return true
	default:
		return false
	}
}

func isCompletedTaskStatus(status string) bool {
	switch strings.ToLower(status) {
	case "succeeded", "completed", "success":
		return true
	default:
		return false
	}
}

func protectionPlanAllowsBackup(status string) bool {
	switch strings.ToLower(status) {
	case "active", "active_with_warning":
		return true
	default:
		return false
	}
}

func (r *Router) dispatchRecoveryTaskAfterStorageSync(task store.Task, storageName string, storageRepoID string, sourceClusterID string) {
	if storageRepoID == "" {
		r.dispatchRecoveryTask(task)
		return
	}
	if r.isStorageAlreadySynced(task.ClusterID, storageName, storageRepoID, sourceClusterID) {
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "storage_preflight_skipped",
			Message: "Storage location already configured on target cluster.",
		})
		r.dispatchRecoveryTask(task)
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "storage_preflight_started",
		Message: "Configuring storage...",
	})
	storageTask, err := r.ensureStorageSynced(context.Background(), task.ClusterID, storageName, storageRepoID, sourceClusterID)
	if err != nil {
		r.logger.Error("storage sync preflight failed before recovery dispatch", "cluster_id", task.ClusterID, "task_type", task.Type, "task_id", task.ID, "storage_repo", storageName, "storage_task_id", storageTask.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "failed",
			Progress:     0,
			ErrorCode:    "STORAGE_SYNC_FAILED",
			ErrorMessage: err.Error(),
			MarkDone:     true,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "error",
			Reason:  "storage_preflight_failed",
			Message: err.Error(),
			Payload: map[string]any{"storageTaskId": storageTask.ID},
		})
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "storage_preflight_succeeded",
		Message: recoveryDispatchMessage(task.Type),
		Payload: map[string]any{"storageTaskId": storageTask.ID},
	})
	r.dispatchRecoveryTask(task)
}

func (r *Router) dispatchRecoveryTask(task store.Task) {
	conn, ok := r.hub.get(task.ClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; recovery will be dispatched after reconnect",
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "warning",
			Reason:  "dispatch_waiting_agent",
			Message: recoveryDispatchMessage(task.Type),
		})
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		r.logger.Error("failed to dispatch recovery task after storage preflight", "task_type", task.Type, "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "warning",
			Reason:  "dispatch_failed",
			Message: recoveryDispatchMessage(task.Type),
		})
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: recoveryDispatchMessage(task.Type),
	})
}

func (r *Router) isStorageAlreadySynced(clusterID string, storageName string, repositoryID string, sourceClusterID string) bool {
	if repositoryID != "" {
		ready, _, err := r.clusterStorageBindingReady(clusterID, repositoryID, sourceClusterID)
		if err != nil {
			r.logger.Warn("failed to check cluster storage binding", "cluster_id", clusterID, "repository_id", repositoryID, "error", err)
			return false
		}
		return ready
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		r.logger.Warn("failed to check storage sync history", "cluster_id", clusterID, "storage_repo", storageName, "repository_id", repositoryID, "error", err)
		return false
	}
	for _, task := range tasks {
		if task.Type != "storage-sync" {
			continue
		}
		if !isCompletedTaskStatus(task.Status) {
			continue
		}
		matchesRepo := repositoryID != "" && taskPayloadString(task.Payload, "repositoryId") == repositoryID
		matchesName := storageName != "" && taskPayloadString(task.Payload, "name") == storageName
		if matchesRepo || matchesName {
			return true
		}
	}
	return false
}

func recoveryDispatchMessage(taskType string) string {
	switch taskType {
	case "drill":
		return "Dispatching drill task..."
	case "takeover":
		return "Dispatching takeover task..."
	default:
		return "Dispatching restore task..."
	}
}

func (r *Router) agentWebSocket(w http.ResponseWriter, req *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(req *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		r.logger.Warn("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	var register protocol.Message[protocol.RegisterPayload]
	if err := conn.ReadJSON(&register); err != nil {
		r.logger.Warn("failed to read register message", "error", err)
		return
	}

	if register.Type != protocol.MessageAgentRegister {
		_ = conn.WriteJSON(newRejectedMessage("EXPECTED_REGISTER", "first message must be agent.register"))
		return
	}

	var cluster store.Cluster
	var credential string
	isNewRegistration := false
	if register.Payload.AgentCredential != "" {
		authenticated, ok, err := r.store.AuthenticateAgentCredential(store.AgentCredentialInput{
			ClusterID:  register.ClusterID,
			Credential: register.Payload.AgentCredential,
		})
		if err != nil {
			r.logger.Error("failed to authenticate agent credential", "cluster_id", register.ClusterID, "error", err)
			_ = conn.WriteJSON(newRejectedMessage("CREDENTIAL_AUTH_FAILED", "failed to authenticate agent credential"))
			return
		}
		if !ok {
			_ = conn.WriteJSON(newRejectedMessage("CREDENTIAL_INVALID", "agent credential is invalid"))
			return
		}
		cluster = authenticated
		credential = register.Payload.AgentCredential
	} else {
		registered, issuedCredential, err := r.store.RegisterCluster(store.RegisterClusterInput{
			Token:         register.Payload.InstallToken,
			ClusterName:   register.Payload.Cluster.Name,
			KubeVersion:   register.Payload.Cluster.KubeVersion,
			AgentVersion:  register.Payload.Agent.Version,
			VeleroVersion: register.Payload.Velero.Version,
			VeleroStatus:  register.Payload.Velero.Status,
		})
		if err != nil {
			reason := "TOKEN_INVALID"
			if errors.Is(err, store.ErrTokenExpired) {
				reason = "TOKEN_EXPIRED"
			}
			if errors.Is(err, store.ErrTokenUsed) {
				reason = "TOKEN_USED"
			}
			_ = conn.WriteJSON(newRejectedMessage(reason, err.Error()))
			return
		}
		cluster = registered
		credential = issuedCredential
		isNewRegistration = true
	}

	accepted := protocol.Message[protocol.RegisterAcceptedPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformRegisterAccepted,
		TenantID:    cluster.TenantID,
		ClusterID:   cluster.ID,
		AgentID:     register.AgentID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.RegisterAcceptedPayload{
			AckMessageID:                    register.MessageID,
			AckType:                         protocol.MessageAgentRegister,
			RequestID:                       register.Payload.RequestID,
			TenantID:                        cluster.TenantID,
			ClusterID:                       cluster.ID,
			ClusterName:                     cluster.Name,
			AgentCredential:                 credential,
			HeartbeatIntervalSeconds:        30,
			InventoryResyncIntervalSeconds:  300,
			InventoryChangeDebounceSeconds:  8,
			InventoryMinPushIntervalSeconds: 15,
			ProtocolVersion:                 protocol.Version,
			Features: map[string]bool{
				"taskDispatch":    true,
				"inventoryReport": true,
				"veleroEvent":     true,
				"sizeReport":      true,
			},
		},
	}
	if err := conn.WriteJSON(accepted); err != nil {
		r.logger.Warn("failed to write register accepted", "cluster_id", cluster.ID, "error", err)
		return
	}
	if isNewRegistration {
		task, err := r.store.CreateTask(store.TaskInput{
			ClusterID: cluster.ID,
			Type:      "register",
			Status:    "succeeded",
			Payload: map[string]any{
				"agentId":       register.AgentID,
				"clusterId":     cluster.ID,
				"clusterName":   cluster.Name,
				"kubeVersion":   register.Payload.Cluster.KubeVersion,
				"agentVersion":  register.Payload.Agent.Version,
				"veleroVersion": register.Payload.Velero.Version,
				"veleroStatus":  register.Payload.Velero.Status,
			},
		})
		if err != nil {
			r.logger.Warn("failed to record register task", "cluster_id", cluster.ID, "error", err)
		} else {
			_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:   task.ID,
				Status:   "succeeded",
				Progress: 100,
				MarkDone: true,
			})
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  task.ID,
				Level:   "info",
				Reason:  "register.accepted",
				Message: "platform accepted agent registration and issued cluster identity",
				Payload: map[string]any{
					"clusterId": cluster.ID,
					"agentId":   register.AgentID,
				},
			})
		}
	}
	r.logger.Info("agent registered", "cluster_id", cluster.ID, "cluster", cluster.Name)

	r.hub.set(cluster.ID, conn)
	r.configureAgentConnection(conn, cluster.ID)
	pingDone := make(chan struct{})
	go r.pingAgentConnection(conn, cluster.ID, pingDone)
	defer func() {
		close(pingDone)
		r.hub.remove(cluster.ID, conn)
		if _, _, err := r.store.SetClusterConnectionStatus(cluster.ID, "offline"); err != nil {
			r.logger.Warn("failed to mark agent offline", "cluster_id", cluster.ID, "error", err)
		}
	}()
	r.redispatchPendingTasks(cluster.ID, conn)
	r.readAgentMessages(conn, cluster.ID)
}

func (r *Router) configureAgentConnection(conn *websocket.Conn, clusterID string) {
	_ = conn.SetReadDeadline(time.Now().Add(agentPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(agentPongWait))
	})
	conn.SetCloseHandler(func(code int, text string) error {
		r.logger.Info("agent websocket close received", "cluster_id", clusterID, "code", code, "text", text)
		return nil
	})
}

func (r *Router) pingAgentConnection(conn *websocket.Conn, clusterID string, done <-chan struct{}) {
	ticker := time.NewTicker(agentPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			deadline := time.Now().Add(5 * time.Second)
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), deadline); err != nil {
				r.logger.Info("agent websocket ping failed", "cluster_id", clusterID, "error", err)
				_ = conn.Close()
				return
			}
		}
	}
}

func (r *Router) readAgentMessages(conn *websocket.Conn, clusterID string) {
	for {
		var meta struct {
			Type      string `json:"type"`
			ClusterID string `json:"clusterId"`
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			r.logger.Info("agent websocket closed", "cluster_id", clusterID, "error", err)
			return
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			r.logger.Warn("failed to decode agent message metadata", "cluster_id", clusterID, "error", err)
			return
		}

		switch meta.Type {
		case protocol.MessageAgentHeartbeat:
			var heartbeat protocol.Message[protocol.HeartbeatPayload]
			if err := json.Unmarshal(data, &heartbeat); err != nil {
				r.logger.Warn("failed to decode heartbeat", "cluster_id", clusterID, "error", err)
				return
			}
			updated, ok, err := r.store.UpdateHeartbeat(store.HeartbeatInput{
				ClusterID:                  clusterID,
				Status:                     heartbeat.Payload.Status,
				AgentVersion:               heartbeat.Payload.AgentVersion,
				AgentImage:                 heartbeat.Payload.AgentImage,
				AgentImageID:               heartbeat.Payload.AgentImageID,
				AgentImageDigest:           heartbeat.Payload.AgentImageDigest,
				VeleroStatus:               heartbeat.Payload.VeleroStatus,
				VeleroVersion:              heartbeat.Payload.VeleroVersion,
				VeleroImage:                heartbeat.Payload.VeleroImage,
				VeleroImageDigest:          heartbeat.Payload.VeleroImageDigest,
				VeleroServerReady:          heartbeat.Payload.VeleroServerReady,
				VeleroNodeAgentDesired:     heartbeat.Payload.VeleroNodeAgentDesired,
				VeleroNodeAgentReady:       heartbeat.Payload.VeleroNodeAgentReady,
				VeleroNodeAgentImageDigest: heartbeat.Payload.VeleroNodeAgentImageDigest,
				ActiveTasks:                heartbeat.Payload.ActiveTasks,
			})
			if err != nil {
				r.logger.Error("failed to update heartbeat", "cluster_id", clusterID, "error", err)
				return
			}
			if !ok {
				r.logger.Warn("heartbeat for unknown cluster", "cluster_id", clusterID)
				return
			}
			r.completeAgentUpgradeAfterHeartbeat(updated)
			r.completeVeleroUpgradeAfterHeartbeat(updated)
			r.logger.Info("agent heartbeat",
				"cluster_id", updated.ID,
				"status", updated.Status,
				"last_inventory_at", heartbeat.Payload.LastInventoryAt,
			)
		case protocol.MessageAgentInventoryReport:
			var inventory protocol.Message[protocol.InventoryReportPayload]
			if err := json.Unmarshal(data, &inventory); err != nil {
				r.logger.Warn("failed to decode inventory report", "cluster_id", clusterID, "error", err)
				return
			}
			apps := make([]store.Application, 0, len(inventory.Payload.Apps))
			for _, app := range inventory.Payload.Apps {
				workloadCount := app.Resources.Deployments + app.Resources.StatefulSets + app.Resources.DaemonSets + app.Resources.Jobs + app.Resources.CronJobs
				resourceSummary := map[string]any{
					"deployments":     app.Resources.Deployments,
					"statefulsets":    app.Resources.StatefulSets,
					"daemonsets":      app.Resources.DaemonSets,
					"jobs":            app.Resources.Jobs,
					"cronjobs":        app.Resources.CronJobs,
					"services":        app.Resources.Services,
					"ingresses":       app.Resources.Ingresses,
					"networkPolicies": app.Resources.NetworkPolicies,
					"configmaps":      app.Resources.ConfigMaps,
					"secrets":         app.Resources.Secrets,
					"serviceAccounts": app.Resources.ServiceAccounts,
					"pvcs":            app.Resources.PVCs,
					"pvCapacityBytes": app.Resources.PVCapacityBytes,
					"ageSeconds":      app.AgeSeconds,
				}
				if len(app.Resources.Categories) > 0 {
					resourceSummary["categories"] = app.Resources.Categories
				}
				if app.Resources.DRSupport != nil {
					resourceSummary["drSupport"] = app.Resources.DRSupport
				}
				apps = append(apps, store.Application{
					Namespace:       app.Namespace,
					Name:            app.Namespace,
					Status:          app.Status,
					Labels:          app.Labels,
					WorkloadCount:   workloadCount,
					ServiceCount:    app.Resources.Services,
					IngressCount:    app.Resources.Ingresses,
					ConfigMapCount:  app.Resources.ConfigMaps,
					SecretCount:     app.Resources.Secrets,
					PVCCount:        app.Resources.PVCs,
					PVCapacityBytes: app.Resources.PVCapacityBytes,
					ResourceSummary: resourceSummary,
					LastCollectedAt: inventory.Payload.CollectedAt,
				})
			}
			updated, ok, err := r.store.ApplyInventory(store.InventoryInput{
				ClusterID:      clusterID,
				KubeVersion:    inventory.Payload.Cluster.KubeVersion,
				VeleroStatus:   inventory.Payload.Velero.Status,
				NodeCount:      inventory.Payload.Cluster.NodeCount,
				NamespaceCount: inventory.Payload.Cluster.NamespaceCount,
				Nodes:          mapInventoryNodes(inventory.Payload.Nodes),
				StorageClasses: mapInventoryStorageClasses(inventory.Payload.StorageClasses),
				Apps:           apps,
				CollectedAt:    inventory.Payload.CollectedAt,
				Hash:           inventory.Payload.InventoryHash,
			})
			if err != nil {
				r.logger.Error("failed to apply inventory", "cluster_id", clusterID, "error", err)
				return
			}
			if !ok {
				r.logger.Warn("inventory for unknown cluster", "cluster_id", clusterID)
				return
			}
			r.logger.Info("agent inventory applied",
				"cluster_id", updated.ID,
				"node_count", updated.NodeCount,
				"application_count", updated.ApplicationCount,
			)
			r.ingestVeleroBackupsFromInventory(clusterID, inventory.Payload.Velero.RecentBackups)
			r.completeInventoryRequest(clusterID, inventory.Payload)
		case protocol.MessageAgentMessageError:
			var messageError protocol.Message[protocol.MessageErrorPayload]
			if err := json.Unmarshal(data, &messageError); err != nil {
				r.logger.Warn("failed to decode agent message error", "cluster_id", clusterID, "error", err)
				return
			}
			r.logger.Warn("agent rejected platform message",
				"cluster_id", clusterID,
				"ack_message_id", messageError.Payload.AckMessageID,
				"ack_type", messageError.Payload.AckType,
				"request_id", messageError.Payload.RequestID,
				"task_id", messageError.Payload.TaskID,
				"error_code", messageError.Payload.ErrorCode,
				"message", messageError.Payload.Message,
				"retryable", messageError.Payload.Retryable,
			)
			if messageError.Payload.AckType == protocol.MessagePlatformInventoryRequest {
				r.failInventoryRequest(clusterID, messageError.Payload)
			}
		case protocol.MessageAgentLogReport:
			var report protocol.Message[protocol.LogReportPayload]
			if err := json.Unmarshal(data, &report); err != nil {
				r.logger.Warn("failed to decode agent log report", "cluster_id", clusterID, "error", err)
				continue
			}
			r.logRequestMu.Lock()
			waiter := r.logRequests[report.Payload.RequestID]
			r.logRequestMu.Unlock()
			if waiter != nil {
				select {
				case waiter <- report.Payload:
				default:
				}
			}
			clusters, _ := r.store.ListClusters()
			tenantID := ""
			for _, cluster := range clusters {
				if cluster.ID == clusterID {
					tenantID = cluster.TenantID
					break
				}
			}
			if tenantID == "" {
				r.logger.Warn("log report for unknown cluster", "cluster_id", clusterID)
				continue
			}
			if report.Payload.ErrorCode != "" {
				_, _ = r.store.CreateDiagnosticLog(store.DiagnosticLogInput{TenantID: tenantID, Scope: "tenant", Level: "error", Component: report.Payload.Component, Operation: "collect_logs", Message: report.Payload.Message, ClusterID: clusterID, RequestID: report.Payload.RequestID, ErrorCode: report.Payload.ErrorCode})
				continue
			}
			for _, entry := range report.Payload.Entries {
				_, err := r.store.CreateDiagnosticLog(store.DiagnosticLogInput{TenantID: tenantID, Scope: "tenant", Level: entry.Level, Component: entry.Component, Operation: "container_log", Message: sanitizeDiagnosticMessage(entry.Message), ClusterID: clusterID, RequestID: report.Payload.RequestID, Details: map[string]any{"pod": entry.Pod, "node": entry.Node, "sourceTimestamp": entry.Timestamp}})
				if err != nil {
					r.logger.Error("persist agent log entry failed", "cluster_id", clusterID, "request_id", report.Payload.RequestID, "error", err)
					break
				}
			}
		case protocol.MessageAgentTaskAccepted:
			var accepted protocol.Message[protocol.TaskAcceptedPayload]
			if err := json.Unmarshal(data, &accepted); err != nil {
				r.logger.Warn("failed to decode task accepted", "cluster_id", clusterID, "error", err)
				return
			}
			_, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       accepted.Payload.TaskID,
				Status:       "accepted",
				Progress:     0,
				MarkAccepted: true,
			})
			if err != nil {
				r.logger.Error("failed to update accepted task", "task_id", accepted.Payload.TaskID, "error", err)
				return
			}
			_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: accepted.Payload.TaskID, Level: "info", Reason: "accepted", Message: "agent accepted task"})
		case protocol.MessageAgentTaskProgress:
			var progress protocol.Message[protocol.TaskProgressPayload]
			if err := json.Unmarshal(data, &progress); err != nil {
				r.logger.Warn("failed to decode task progress", "cluster_id", clusterID, "error", err)
				return
			}
			_, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:      progress.Payload.TaskID,
				Status:      progress.Payload.Status,
				Progress:    progress.Payload.Progress,
				Payload:     taskProgressPayloadPatch(progress.Payload),
				MarkStarted: true,
			})
			if err != nil {
				r.logger.Error("failed to update task progress", "task_id", progress.Payload.TaskID, "error", err)
				return
			}
			_ = r.addTaskEventIfChanged(store.TaskEventInput{
				TaskID:  progress.Payload.TaskID,
				Level:   "info",
				Reason:  "progress",
				Message: progress.Payload.Message,
				Payload: map[string]any{"velero": progress.Payload.Velero},
			})
		case protocol.MessageAgentVeleroEvent:
			var event protocol.Message[protocol.VeleroEventPayload]
			if err := json.Unmarshal(data, &event); err != nil {
				r.logger.Warn("failed to decode velero event", "cluster_id", clusterID, "error", err)
				return
			}
			task, err := r.handleVeleroBackupEvent(clusterID, event.Payload)
			if err != nil {
				if event.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, event.AgentID, event.MessageID, event.Type, event.Payload.TaskID, event.Payload.CommandID, "VELERO_EVENT_HANDLE_FAILED", err.Error(), true)
				}
				continue
			}
			if event.Payload.AckRequired {
				taskID := event.Payload.TaskID
				commandID := event.Payload.CommandID
				if taskID == "" && task.ID != "" {
					taskID = task.ID
				}
				if commandID == "" && task.CommandID != "" {
					commandID = task.CommandID
				}
				_ = r.writeEventAck(conn, clusterID, event.AgentID, event.MessageID, event.Type, taskID, commandID)
			}
		case protocol.MessageAgentTaskCompleted:
			var completed protocol.Message[protocol.TaskCompletedPayload]
			if err := json.Unmarshal(data, &completed); err != nil {
				r.logger.Warn("failed to decode task completed", "cluster_id", clusterID, "error", err)
				return
			}
			existingTask, ok, err := r.findTaskByID(clusterID, completed.Payload.TaskID)
			if err != nil {
				r.logger.Error("failed to load task before completion", "task_id", completed.Payload.TaskID, "error", err)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_COMPLETE_FAILED", err.Error(), true)
				}
				return
			}
			if !ok {
				r.logger.Error("task not found before completion", "task_id", completed.Payload.TaskID)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_NOT_FOUND", "task not found", true)
				}
				return
			}
			if existingTask.Type == "backup" && isForceStoppedBackupTask(existingTask) {
				_ = r.store.AddTaskEvent(store.TaskEventInput{
					TaskID:  existingTask.ID,
					Level:   "warning",
					Reason:  "completion_after_cancel",
					Message: "Ignored backup completion because force stop already finalized this sync task.",
					Payload: map[string]any{"velero": completed.Payload.Velero},
				})
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				continue
			}
			if existingTask.Type == "backup-cancel" {
				if err := r.finishBackupCancelTask(clusterID, existingTask, completed.Payload); err != nil {
					r.logger.Error("failed to finish backup cancel task", "task_id", completed.Payload.TaskID, "error", err)
					if completed.Payload.AckRequired {
						_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "BACKUP_CANCEL_FINISH_FAILED", err.Error(), true)
					}
					return
				}
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				continue
			}
			if existingTask.Type == "agent-upgrade" || existingTask.Type == "velero-upgrade" {
				progress := 70
				reason := "waiting_for_reconnect"
				message := "agent deployment updated; waiting for the new agent to reconnect"
				if existingTask.Type == "velero-upgrade" {
					progress = 90
					reason = "waiting_for_verification"
					message = "velero rollout completed; waiting for server and node-agent digest verification"
				}
				_, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
					TaskID:      existingTask.ID,
					Status:      "running",
					Progress:    progress,
					Payload:     taskCompletedPayloadPatch(completed.Payload),
					MarkStarted: true,
				})
				if err != nil {
					r.logger.Error("failed to mark agent upgrade waiting for reconnect", "task_id", existingTask.ID, "error", err)
					return
				}
				_ = r.addTaskEventIfChanged(store.TaskEventInput{
					TaskID: existingTask.ID, Level: "info", Reason: reason, Message: message,
				})
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				continue
			}
			patch := taskCompletedPayloadPatch(completed.Payload)
			existingTask, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:      completed.Payload.TaskID,
				Status:      "finalizing",
				Progress:    100,
				Payload:     patch,
				MarkStarted: true,
			})
			if err != nil {
				r.logger.Error("failed to mark task finalizing", "task_id", completed.Payload.TaskID, "error", err)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_FINALIZE_FAILED", err.Error(), true)
				}
				return
			}
			_ = r.addTaskEventIfChanged(store.TaskEventInput{
				TaskID:  completed.Payload.TaskID,
				Level:   "info",
				Reason:  "finalizing",
				Message: "finalizing restore point",
				Payload: map[string]any{"velero": completed.Payload.Velero},
			})
			if existingTask.Type == "backup" {
				taskForPoint := existingTask
				if taskForPoint.Payload == nil {
					taskForPoint.Payload = map[string]any{}
				}
				for key, value := range patch {
					taskForPoint.Payload[key] = value
				}
				if point, err := r.createRestorePointFromBackup(taskForPoint, completed.Payload.Velero); err != nil {
					r.logger.Error("failed to create restore point", "task_id", existingTask.ID, "error", err)
					_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
						TaskID:       completed.Payload.TaskID,
						Status:       "failed",
						Progress:     100,
						ErrorCode:    "RESTORE_POINT_CREATE_FAILED",
						ErrorMessage: err.Error(),
						MarkDone:     true,
					})
					if completed.Payload.AckRequired {
						_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "RESTORE_POINT_CREATE_FAILED", err.Error(), true)
					}
					continue
				} else if point.ID != "" {
					r.reconcileRetention(point.ProtectionPlanID, point.BackupTaskID)
				}
			}
			if existingTask.Type == "unregister" {
				if err := r.finishUnregisterTask(clusterID, existingTask); err != nil {
					_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: existingTask.ID, Status: "failed", Progress: 95, ErrorCode: "PLATFORM_CLEANUP_FAILED", ErrorMessage: err.Error(), MarkDone: true})
					_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: existingTask.ID, Level: "error", Reason: "PLATFORM_CLEANUP_FAILED", Message: err.Error()})
					if completed.Payload.AckRequired {
						_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "PLATFORM_CLEANUP_FAILED", err.Error(), true)
					}
					return
				}
				_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: existingTask.ID, Status: "succeeded", Progress: 100, Payload: patch, MarkDone: true})
				_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: existingTask.ID, Level: "info", Reason: "completed", Message: "cluster unregister completed"})
				if completed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
				}
				return
			}
			task, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:   completed.Payload.TaskID,
				Status:   "succeeded",
				Progress: 100,
				Payload:  patch,
				MarkDone: true,
			})
			if err != nil {
				r.logger.Error("failed to complete task", "task_id", completed.Payload.TaskID, "error", err)
				if completed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID, "TASK_COMPLETE_FAILED", err.Error(), true)
				}
				return
			}
			_ = r.addTaskEventIfChanged(store.TaskEventInput{
				TaskID:  completed.Payload.TaskID,
				Level:   "info",
				Reason:  "completed",
				Message: completed.Payload.Message,
				Payload: map[string]any{"velero": completed.Payload.Velero},
			})
			if task.Type == "storage-sync" && taskPayloadBool(task.Payload, "reconfigureStorage") {
				r.markClusterStorageBindingReady(task)
				r.finishProtectionPlanStorageReconfigure(task)
			} else if task.Type == "storage-sync" {
				r.markClusterStorageBindingReady(task)
				r.continueProtectionPlanActivationAfterStorage(task)
			}
			if task.Type == "schedule-sync" && task.ProtectionPlanID != "" {
				status := "active"
				if r.hasTargetStorageWarning(task.ProtectionPlanID) {
					status = "active_with_warning"
				}
				if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, status); err != nil {
					r.logger.Error("failed to mark protection plan active", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
				}
			}
			if task.Type == "retention-cleanup" {
				r.finishRetentionCleanupTask(task, completed.Payload.Velero)
			}
			if task.Type == "protection-cleanup" {
				r.finishProtectionCleanupTask(task, completed.Payload.Velero)
			}
			if completed.Payload.AckRequired {
				_ = r.writeEventAck(conn, clusterID, completed.AgentID, completed.MessageID, completed.Type, completed.Payload.TaskID, completed.Payload.CommandID)
			}
		case protocol.MessageAgentTaskFailed:
			var failed protocol.Message[protocol.TaskFailedPayload]
			if err := json.Unmarshal(data, &failed); err != nil {
				r.logger.Warn("failed to decode task failed", "cluster_id", clusterID, "error", err)
				return
			}
			errorMessage := detailedTaskFailureMessage(failed.Payload.Message, failed.Payload.Details)
			payloadPatch := taskFailurePayloadPatch(failed.Payload.Details)
			existingTask, _, lookupErr := r.findTaskByID(clusterID, failed.Payload.TaskID)
			if lookupErr == nil && existingTask.Type == "backup" && isForceStoppedBackupTask(existingTask) {
				_ = r.store.AddTaskEvent(store.TaskEventInput{
					TaskID:  existingTask.ID,
					Level:   "warning",
					Reason:  failed.Payload.ErrorCode,
					Message: "Ignored backup failure because force stop is in progress or already completed.",
					Payload: failed.Payload.Details,
				})
				if failed.Payload.AckRequired {
					_ = r.writeEventAck(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID)
				}
				continue
			}
			task, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       failed.Payload.TaskID,
				Status:       "failed",
				Progress:     0,
				ErrorCode:    failed.Payload.ErrorCode,
				ErrorMessage: errorMessage,
				Payload:      payloadPatch,
				MarkDone:     true,
			})
			if err != nil {
				r.logger.Error("failed to mark task failed", "task_id", failed.Payload.TaskID, "error", err)
				if failed.Payload.AckRequired {
					_ = r.writeEventError(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID, "TASK_FAIL_UPDATE_FAILED", err.Error(), true)
				}
				return
			}
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  failed.Payload.TaskID,
				Level:   "error",
				Reason:  failed.Payload.ErrorCode,
				Message: errorMessage,
				Payload: failed.Payload.Details,
			})
			if task.Type == "retention-cleanup" {
				r.markRetentionCleanupFailed(task, failed.Payload.Message)
			}
			if task.Type == "protection-cleanup" {
				r.markProtectionCleanupFailed(task, failed.Payload.Message)
			}
			if task.Type == "backup-cancel" {
				r.markBackupCancelFailed(task, failed.Payload.Message)
			}
			if task.Type == "storage-sync" && task.ProtectionPlanID != "" {
				r.markClusterStorageBindingFailed(task, failed.Payload.ErrorCode, failed.Payload.Message)
				if r.retryStorageSyncTask(task, failed.Payload.Message) {
					if failed.Payload.AckRequired {
						_ = r.writeEventAck(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID)
					}
					continue
				}
				if taskPayloadString(task.Payload, "activationRole") == "target" {
					r.finishTargetStorageSyncFailed(task)
				} else {
					if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "storage_failed"); err != nil {
						r.logger.Error("failed to mark protection plan storage failed", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
					}
				}
			} else if task.Type == "storage-sync" {
				r.markClusterStorageBindingFailed(task, failed.Payload.ErrorCode, failed.Payload.Message)
			}
			if task.Type == "schedule-sync" && task.ProtectionPlanID != "" {
				if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "schedule_failed"); err != nil {
					r.logger.Error("failed to mark protection plan schedule failed", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
				}
			}
			if failed.Payload.AckRequired {
				_ = r.writeEventAck(conn, clusterID, failed.AgentID, failed.MessageID, failed.Type, failed.Payload.TaskID, failed.Payload.CommandID)
			}
		default:
			r.logger.Warn("unsupported agent message", "cluster_id", clusterID, "type", meta.Type)
		}
	}
}

func (r *Router) writeEventAck(conn *websocket.Conn, clusterID string, agentID string, ackMessageID string, ackType string, taskID string, commandID string) error {
	message := protocol.Message[protocol.EventAckPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformEventAck,
		ClusterID:   clusterID,
		AgentID:     agentID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.EventAckPayload{
			AckMessageID: ackMessageID,
			AckType:      ackType,
			TaskID:       taskID,
			CommandID:    commandID,
			Persisted:    true,
		},
	}
	return conn.WriteJSON(message)
}

func (r *Router) writeEventError(conn *websocket.Conn, clusterID string, agentID string, ackMessageID string, ackType string, taskID string, commandID string, code string, messageText string, retryable bool) error {
	message := protocol.Message[protocol.EventErrorPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformEventError,
		ClusterID:   clusterID,
		AgentID:     agentID,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.EventErrorPayload{
			AckMessageID: ackMessageID,
			AckType:      ackType,
			TaskID:       taskID,
			CommandID:    commandID,
			ErrorCode:    code,
			Message:      messageText,
			Retryable:    retryable,
		},
	}
	return conn.WriteJSON(message)
}

func (r *Router) continueProtectionPlanActivationAfterStorage(task store.Task) {
	if task.ProtectionPlanID == "" {
		return
	}
	if taskPayloadString(task.Payload, "activationRole") == "target" {
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(task.ProtectionPlanID)
	if err != nil {
		r.logger.Error("failed to load protection plan after storage sync", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		return
	}
	if !ok {
		return
	}
	if plan.Status != "activating_storage" {
		return
	}
	ready, warning, err := r.protectionPlanStorageBindingsReady(plan)
	if err != nil {
		r.logger.Error("failed to evaluate protection plan storage bindings", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan storage failed", "plan_id", plan.ID, "error", updateErr)
		}
		return
	}
	if !ready {
		if warning != "" {
			r.logger.Warn("protection plan storage binding is not ready", "plan_id", plan.ID, "task_id", task.ID, "warning", warning)
		}
		return
	}
	if attemptID := taskPayloadString(task.Payload, "activationAttempt"); attemptID != "" {
		ready, err := r.protectionPlanActivationStorageReady(plan.ID, attemptID)
		if err != nil {
			r.logger.Error("failed to evaluate protection plan storage activation tasks", "plan_id", plan.ID, "task_id", task.ID, "error", err)
			if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed"); updateErr != nil {
				r.logger.Error("failed to mark protection plan storage failed", "plan_id", plan.ID, "error", updateErr)
			}
			return
		}
		if !ready {
			return
		}
	}
	policy, shouldSchedule, err := r.protectionPlanSchedulePolicy(plan)
	if err != nil {
		r.logger.Error("failed to evaluate protection plan schedule policy", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan schedule failed", "plan_id", plan.ID, "error", updateErr)
		}
		return
	}
	if !shouldSchedule {
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "active"); err != nil {
			r.logger.Error("failed to mark protection plan active", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		}
		return
	}
	if err := r.enableProtectionPlanSchedule(plan, policy); err != nil {
		r.logger.Error("failed to enable platform schedule for protection plan", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan schedule failed", "plan_id", plan.ID, "error", updateErr)
		}
		return
	}
	if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "active"); err != nil {
		r.logger.Error("failed to mark protection plan active after enabling platform schedule", "plan_id", plan.ID, "task_id", task.ID, "error", err)
	}
}

func (r *Router) protectionPlanStorageBindingsReady(plan store.ProtectionPlan) (bool, string, error) {
	if plan.StorageRepoID == "" {
		return false, "storage repository is not set", nil
	}
	sourceReady, sourceMessage, err := r.clusterStorageBindingReady(plan.SourceClusterID, plan.StorageRepoID, plan.SourceClusterID)
	if err != nil {
		return false, "", err
	}
	if !sourceReady {
		if sourceMessage == "" {
			sourceMessage = "source cluster storage binding is not ready"
		}
		return false, sourceMessage, nil
	}
	if plan.TargetClusterID == "" || plan.TargetClusterID == plan.SourceClusterID {
		return true, "", nil
	}
	targetReady, targetMessage, err := r.clusterStorageBindingReady(plan.TargetClusterID, plan.StorageRepoID, plan.SourceClusterID)
	if err != nil {
		return false, "", err
	}
	if !targetReady {
		if targetMessage == "" {
			targetMessage = "target cluster storage binding is not ready"
		}
		return true, targetMessage, nil
	}
	return true, "", nil
}

func (r *Router) clusterStorageBindingReady(clusterID string, storageRepoID string, sourceClusterID string) (bool, string, error) {
	repo, ok, err := r.store.GetStorageRepository(storageRepoID)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "storage repository not found", nil
	}
	binding, ok, err := r.store.GetClusterStorageBinding(clusterID, storageRepoID, sourceClusterID)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "storage binding has not been configured", nil
	}
	expectedBSLName := storageDomainBSLName(repo, sourceClusterID)
	expectedPrefix := storageDomainPrefix(sourceClusterID)
	if binding.BSLName != expectedBSLName || binding.ObjectPrefix != expectedPrefix {
		return false, "storage binding uses an outdated backup storage location", nil
	}
	if strings.EqualFold(binding.Status, "ready") && binding.LastSuccessAt.After(repo.UpdatedAt.Add(-time.Second)) {
		return true, "", nil
	}
	if strings.EqualFold(binding.Status, "failed") {
		if binding.LastErrorMessage != "" {
			return false, binding.LastErrorMessage, nil
		}
		if binding.LastErrorCode != "" {
			return false, binding.LastErrorCode, nil
		}
		return false, "storage binding failed", nil
	}
	if strings.EqualFold(binding.Status, "configuring") {
		return false, "storage binding is being configured", nil
	}
	return false, "storage binding is not ready", nil
}

func (r *Router) markClusterStorageBindingReady(task store.Task) {
	repositoryID := taskPayloadString(task.Payload, "repositoryId")
	if task.ClusterID == "" || repositoryID == "" {
		return
	}
	sourceClusterID := taskPayloadString(task.Payload, "sourceClusterId")
	if sourceClusterID == "" {
		sourceClusterID = task.ClusterID
	}
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil || !ok {
		if err != nil {
			r.logger.Warn("failed to load storage repository while marking binding ready", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
		}
		return
	}
	now := time.Now().UTC()
	if !task.CompletedAt.IsZero() {
		now = task.CompletedAt
	}
	if _, ok, err := r.store.UpdateClusterStorageBindingStatus(store.ClusterStorageBindingStatusInput{
		ClusterID:       task.ClusterID,
		StorageRepoID:   repositoryID,
		SourceClusterID: sourceClusterID,
		Status:          "ready",
		RetryCount:      taskPayloadInt(task.Payload, "retryAttempt"),
		LastSyncedAt:    now,
		LastSuccessAt:   now,
		RepoUpdatedAt:   repo.UpdatedAt,
	}); err != nil {
		r.logger.Warn("failed to mark cluster storage binding ready", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
	} else if !ok {
		_, _ = r.store.UpsertClusterStorageBinding(store.ClusterStorageBindingInput{
			ClusterID:       task.ClusterID,
			StorageRepoID:   repositoryID,
			SourceClusterID: sourceClusterID,
			BSLName:         taskPayloadString(task.Payload, "name"),
			ObjectPrefix:    taskPayloadString(task.Payload, "objectPrefix"),
			Status:          "ready",
			RetryCount:      taskPayloadInt(task.Payload, "retryAttempt"),
			RepoUpdatedAt:   repo.UpdatedAt,
		})
		_, _, _ = r.store.UpdateClusterStorageBindingStatus(store.ClusterStorageBindingStatusInput{
			ClusterID:       task.ClusterID,
			StorageRepoID:   repositoryID,
			SourceClusterID: sourceClusterID,
			Status:          "ready",
			RetryCount:      taskPayloadInt(task.Payload, "retryAttempt"),
			LastSyncedAt:    now,
			LastSuccessAt:   now,
			RepoUpdatedAt:   repo.UpdatedAt,
		})
	}
}

func (r *Router) markClusterStorageBindingFailed(task store.Task, code string, message string) {
	repositoryID := taskPayloadString(task.Payload, "repositoryId")
	if task.ClusterID == "" || repositoryID == "" {
		return
	}
	sourceClusterID := taskPayloadString(task.Payload, "sourceClusterId")
	if sourceClusterID == "" {
		sourceClusterID = task.ClusterID
	}
	repo, ok, err := r.store.GetStorageRepository(repositoryID)
	if err != nil {
		r.logger.Warn("failed to load storage repository while marking binding failed", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
		return
	}
	repoUpdatedAt := time.Time{}
	if ok {
		repoUpdatedAt = repo.UpdatedAt
	}
	if _, _, err := r.store.UpdateClusterStorageBindingStatus(store.ClusterStorageBindingStatusInput{
		ClusterID:        task.ClusterID,
		StorageRepoID:    repositoryID,
		SourceClusterID:  sourceClusterID,
		Status:           "failed",
		RetryCount:       taskPayloadInt(task.Payload, "retryAttempt"),
		LastSyncedAt:     time.Now().UTC(),
		LastErrorCode:    code,
		LastErrorMessage: message,
		RepoUpdatedAt:    repoUpdatedAt,
	}); err != nil {
		r.logger.Warn("failed to mark cluster storage binding failed", "cluster_id", task.ClusterID, "repository_id", repositoryID, "task_id", task.ID, "error", err)
	}
}

func (r *Router) reconcileProtectionPlanActivationStates(clusterID string) {
	plans, err := r.store.ListProtectionPlans(clusterID)
	if err != nil {
		r.logger.Warn("failed to list protection plans for activation reconcile", "cluster_id", clusterID, "error", err)
		return
	}
	for _, plan := range plans {
		switch strings.ToLower(plan.Status) {
		case "activating_storage":
			r.reconcileProtectionPlanStorageActivation(plan)
		case "activating_schedule":
			r.reconcileProtectionPlanScheduleActivation(plan)
		}
	}
}

func (r *Router) reconcileProtectionPlanStorageActivation(plan store.ProtectionPlan) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to list tasks for storage activation reconcile", "plan_id", plan.ID, "error", err)
		return
	}
	latestAttempt := ""
	for _, task := range tasks {
		if task.ProtectionPlanID != plan.ID || task.Type != "storage-sync" {
			continue
		}
		attemptID := taskPayloadString(task.Payload, "activationAttempt")
		if attemptID == "" {
			continue
		}
		if latestAttempt == "" || task.CreatedAt.After(latestStorageAttemptCreatedAt(tasks, plan.ID, latestAttempt)) {
			latestAttempt = attemptID
		}
	}
	if latestAttempt == "" {
		return
	}
	for _, task := range tasks {
		if task.ProtectionPlanID != plan.ID || task.Type != "storage-sync" {
			continue
		}
		if taskPayloadString(task.Payload, "activationAttempt") != latestAttempt {
			continue
		}
		if isActiveTaskStatus(task.Status) && task.CreatedAt.Add(protectionPlanActivationTaskTimeout).Before(time.Now().UTC()) {
			message := "storage configuration timed out while waiting for agent response"
			failedTask, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
				TaskID:       task.ID,
				Status:       "failed",
				Progress:     task.Progress,
				ErrorCode:    "STORAGE_SYNC_TIMEOUT",
				ErrorMessage: message,
				MarkDone:     true,
			})
			if err != nil {
				r.logger.Warn("failed to mark storage activation task timed out", "plan_id", plan.ID, "task_id", task.ID, "error", err)
				continue
			}
			r.markClusterStorageBindingFailed(failedTask, "STORAGE_SYNC_TIMEOUT", message)
			_ = r.store.AddTaskEvent(store.TaskEventInput{
				TaskID:  task.ID,
				Level:   "error",
				Reason:  "storage_sync_timeout",
				Message: message,
				Payload: map[string]any{"timeoutSeconds": int(protectionPlanActivationTaskTimeout.Seconds())},
			})
			if r.retryStorageSyncTask(failedTask, message) {
				continue
			}
			if taskPayloadString(failedTask.Payload, "activationRole") == "target" {
				r.finishTargetStorageSyncFailed(failedTask)
			} else if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "storage_failed"); err != nil {
				r.logger.Warn("failed to mark protection plan storage failed after timeout", "plan_id", plan.ID, "error", err)
			}
		}
	}
	latestTask := latestStorageTaskForAttempt(tasks, plan.ID, latestAttempt)
	if latestTask.ID != "" {
		if taskPayloadBool(latestTask.Payload, "reconfigureStorage") {
			r.finishProtectionPlanStorageReconfigure(latestTask)
		} else {
			r.continueProtectionPlanActivationAfterStorage(latestTask)
		}
	}
}

func latestStorageAttemptCreatedAt(tasks []store.Task, planID string, attemptID string) time.Time {
	var latest time.Time
	for _, task := range tasks {
		if task.ProtectionPlanID == planID && task.Type == "storage-sync" && taskPayloadString(task.Payload, "activationAttempt") == attemptID && task.CreatedAt.After(latest) {
			latest = task.CreatedAt
		}
	}
	return latest
}

func latestStorageTaskForAttempt(tasks []store.Task, planID string, attemptID string) store.Task {
	var latest store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "storage-sync" || taskPayloadString(task.Payload, "activationAttempt") != attemptID {
			continue
		}
		if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	return latest
}

func (r *Router) reconcileProtectionPlanScheduleActivation(plan store.ProtectionPlan) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to list tasks for schedule activation reconcile", "plan_id", plan.ID, "error", err)
		return
	}
	var latest store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != plan.ID || task.Type != "schedule-sync" {
			continue
		}
		if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	if latest.ID == "" {
		return
	}
	if isCompletedTaskStatus(latest.Status) {
		status := "active"
		if r.hasTargetStorageWarning(plan.ID) {
			status = "active_with_warning"
		}
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, status); err != nil {
			r.logger.Warn("failed to mark protection plan active during schedule reconcile", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
		}
		return
	}
	if strings.EqualFold(latest.Status, "failed") {
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); err != nil {
			r.logger.Warn("failed to mark protection plan schedule failed during reconcile", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
		}
		return
	}
	if isActiveTaskStatus(latest.Status) && latest.CreatedAt.Add(protectionPlanActivationTaskTimeout).Before(time.Now().UTC()) {
		message := "schedule configuration timed out while waiting for agent response"
		if _, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       latest.ID,
			Status:       "failed",
			Progress:     latest.Progress,
			ErrorCode:    "SCHEDULE_SYNC_TIMEOUT",
			ErrorMessage: message,
			MarkDone:     true,
		}); err != nil {
			r.logger.Warn("failed to mark schedule activation task timed out", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
			return
		}
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  latest.ID,
			Level:   "error",
			Reason:  "schedule_sync_timeout",
			Message: message,
			Payload: map[string]any{"timeoutSeconds": int(protectionPlanActivationTaskTimeout.Seconds())},
		})
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "schedule_failed"); err != nil {
			r.logger.Warn("failed to mark protection plan schedule failed after timeout", "plan_id", plan.ID, "task_id", latest.ID, "error", err)
		}
	}
}

func (r *Router) finishProtectionPlanStorageReconfigure(task store.Task) {
	if task.ProtectionPlanID == "" {
		return
	}
	attemptID := taskPayloadString(task.Payload, "activationAttempt")
	if attemptID == "" {
		if _, _, err := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "active"); err != nil {
			r.logger.Error("failed to mark protection plan active after storage reconfigure", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		}
		return
	}
	ready, err := r.protectionPlanActivationStorageReady(task.ProtectionPlanID, attemptID)
	if err != nil {
		r.logger.Error("failed to evaluate protection plan storage reconfigure tasks", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		if _, _, updateErr := r.store.UpdateProtectionPlanStatus(task.ProtectionPlanID, "storage_failed"); updateErr != nil {
			r.logger.Error("failed to mark protection plan storage failed", "plan_id", task.ProtectionPlanID, "error", updateErr)
		}
		return
	}
	if !ready {
		return
	}
	r.continueProtectionPlanActivationAfterStorage(task)
}

func (r *Router) protectionPlanActivationStorageReady(planID string, attemptID string) (bool, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return false, err
	}
	var sourceTask *store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "storage-sync" {
			continue
		}
		if taskPayloadString(task.Payload, "activationAttempt") != attemptID {
			continue
		}
		role := taskPayloadString(task.Payload, "activationRole")
		if role != "" && role != "source" {
			continue
		}
		if sourceTask == nil || task.CreatedAt.After(sourceTask.CreatedAt) {
			taskCopy := task
			sourceTask = &taskCopy
		}
	}
	if sourceTask == nil {
		return false, nil
	}
	switch strings.ToLower(sourceTask.Status) {
	case "succeeded", "completed", "success":
		return true, nil
	case "failed":
		message := sourceTask.ErrorMessage
		if message == "" {
			message = sourceTask.ErrorCode
		}
		if message == "" {
			message = "source storage sync task failed"
		}
		return false, errors.New(message)
	default:
		return false, nil
	}
}

func (r *Router) retryStorageSyncTask(task store.Task, failureMessage string) bool {
	attemptID := taskPayloadString(task.Payload, "activationAttempt")
	if attemptID == "" {
		return false
	}
	attempt := taskPayloadInt(task.Payload, "retryAttempt")
	if attempt <= 0 {
		attempt = 1
	}
	maxAttempts := taskPayloadInt(task.Payload, "maxAttempts")
	if maxAttempts <= 0 {
		maxAttempts = storageSyncMaxAttempts
	}
	if attempt >= maxAttempts {
		return false
	}
	repositoryID := taskPayloadString(task.Payload, "repositoryId")
	if repositoryID == "" {
		return false
	}
	role := taskPayloadString(task.Payload, "activationRole")
	reconfigure := taskPayloadBool(task.Payload, "reconfigureStorage")
	sourceClusterID := taskPayloadString(task.Payload, "sourceClusterId")
	if sourceClusterID == "" {
		sourceClusterID = task.ClusterID
	}
	nextTask, warning, err := r.dispatchStorageSyncTaskForPlanActivationAttempt(task.ClusterID, repositoryID, task.ProtectionPlanID, sourceClusterID, attemptID, role, reconfigure, attempt+1)
	if err != nil {
		r.logger.Error("failed to retry storage sync task", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "role", role, "attempt", attempt, "error", err)
		return false
	}
	message := fmt.Sprintf("Storage sync failed; retrying attempt %d/%d.", attempt+1, maxAttempts)
	if failureMessage != "" {
		message += " Last error: " + failureMessage
	}
	if warning != "" {
		message += " " + warning
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "warning",
		Reason:  "storage_sync_retry_scheduled",
		Message: message,
		Payload: map[string]any{
			"nextTaskId":     nextTask.ID,
			"retryAttempt":   attempt + 1,
			"maxAttempts":    maxAttempts,
			"activationRole": role,
		},
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  nextTask.ID,
		Level:   "info",
		Reason:  "storage_sync_retry",
		Message: fmt.Sprintf("Storage sync retry attempt %d/%d.", attempt+1, maxAttempts),
		Payload: map[string]any{
			"previousTaskId": task.ID,
			"retryAttempt":   attempt + 1,
			"maxAttempts":    maxAttempts,
			"activationRole": role,
		},
	})
	return true
}

func (r *Router) finishTargetStorageSyncFailed(task store.Task) {
	if task.ProtectionPlanID == "" {
		return
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "warning",
		Reason:  "target_storage_warning",
		Message: "Target cluster storage configuration failed after automatic retries. Backup schedule is not blocked, but restore, drill, and takeover may be unavailable until storage is reconfigured.",
		Payload: map[string]any{
			"impact": []string{"restore_unavailable", "drill_unavailable", "takeover_unavailable"},
		},
	})
	plan, ok, err := r.store.GetProtectionPlan(task.ProtectionPlanID)
	if err != nil {
		r.logger.Error("failed to load protection plan after target storage failure", "plan_id", task.ProtectionPlanID, "task_id", task.ID, "error", err)
		return
	}
	if !ok {
		return
	}
	switch strings.ToLower(plan.Status) {
	case "active", "active_with_warning":
		if _, _, err := r.store.UpdateProtectionPlanStatus(plan.ID, "active_with_warning"); err != nil {
			r.logger.Error("failed to mark protection plan active with warning", "plan_id", plan.ID, "task_id", task.ID, "error", err)
		}
	case "activating_storage":
		r.continueProtectionPlanActivationAfterStorage(task)
	}
}

func (r *Router) hasTargetStorageWarning(planID string) bool {
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err == nil && ok && plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID && plan.StorageRepoID != "" {
		ready, _, readyErr := r.clusterStorageBindingReady(plan.TargetClusterID, plan.StorageRepoID, plan.SourceClusterID)
		if readyErr != nil {
			r.logger.Warn("failed to inspect target storage binding warning", "plan_id", planID, "error", readyErr)
		} else if !ready {
			return true
		}
	} else if err != nil {
		r.logger.Warn("failed to load protection plan for target storage warning", "plan_id", planID, "error", err)
	}
	tasks, err := r.store.ListTasks("")
	if err != nil {
		r.logger.Warn("failed to inspect target storage warning", "plan_id", planID, "error", err)
		return false
	}
	var latest *store.Task
	for _, task := range tasks {
		if task.ProtectionPlanID != planID || task.Type != "storage-sync" {
			continue
		}
		if taskPayloadString(task.Payload, "activationRole") != "target" {
			continue
		}
		if latest == nil || task.CreatedAt.After(latest.CreatedAt) {
			taskCopy := task
			latest = &taskCopy
		}
	}
	if latest == nil {
		return false
	}
	return strings.EqualFold(latest.Status, "failed")
}

func taskPayloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(value)
		return n
	default:
		return 0
	}
}

func taskPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch value := payload[key].(type) {
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}

func taskPayloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	switch value := payload[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}

func mapInventoryNodes(nodes []protocol.NodeInventory) []store.ClusterNode {
	items := make([]store.ClusterNode, 0, len(nodes))
	for _, node := range nodes {
		role := node.Role
		if role == "" {
			role = "<none>"
		}
		status := node.Status
		if status == "" {
			status = "unknown"
		}
		items = append(items, store.ClusterNode{
			Name:           node.Name,
			Status:         status,
			Roles:          role,
			AgeSeconds:     node.AgeSeconds,
			KubeletVersion: node.KubeletVersion,
			Capacity:       node.Capacity,
		})
	}
	return items
}

func mapInventoryStorageClasses(storageClasses []protocol.StorageClassInventory) []store.ClusterStorageClass {
	items := make([]store.ClusterStorageClass, 0, len(storageClasses))
	for _, storageClass := range storageClasses {
		items = append(items, store.ClusterStorageClass{
			Name:                 storageClass.Name,
			Provisioner:          storageClass.Provisioner,
			ReclaimPolicy:        storageClass.ReclaimPolicy,
			VolumeBindingMode:    storageClass.VolumeBindingMode,
			AllowVolumeExpansion: storageClass.AllowVolumeExpansion,
			Default:              storageClass.Default,
			AgeSeconds:           storageClass.AgeSeconds,
		})
	}
	return items
}

func (r *Router) finishUnregisterTask(clusterID string, task store.Task) error {
	r.hub.close(clusterID)
	ok, err := r.store.DeleteCluster(clusterID)
	if err != nil {
		r.logger.Error("failed to clean cluster after unregister", "cluster_id", clusterID, "task_id", task.ID, "error", err)
		return err
	}
	if !ok {
		r.logger.Warn("unregister completed for unknown cluster", "cluster_id", clusterID, "task_id", task.ID)
		return nil
	}
	r.logger.Info("cluster cleaned after agent unregister", "cluster_id", clusterID, "task_id", task.ID)
	return nil
}

func (r *Router) ingestVeleroBackupsFromInventory(clusterID string, backups []map[string]any) {
	if len(backups) == 0 {
		return
	}
	existingPoints, err := r.store.ListRestorePoints(store.RestorePointFilter{ClusterID: clusterID, IncludeDeleted: true})
	if err != nil {
		r.logger.Error("failed to list restore points before velero ingest", "cluster_id", clusterID, "error", err)
		return
	}
	seen := map[string]struct{}{}
	for _, point := range existingPoints {
		seen[point.VeleroBackupName] = struct{}{}
	}
	for _, backup := range backups {
		name := stringFromMap(backup, "name")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		phase := stringFromMap(backup, "phase")
		if phase != "Completed" {
			continue
		}
		labels := mapPayload(backup, "labels")
		planID := stringFromMap(labels, "hypercdr.io/plan-id")
		if planID == "" {
			continue
		}
		plan, ok, err := r.store.GetProtectionPlan(planID)
		if err != nil {
			r.logger.Error("failed to load protection plan for velero backup ingest", "plan_id", planID, "error", err)
			continue
		}
		if !ok {
			continue
		}
		if plan.SourceClusterID != clusterID {
			continue
		}
		sourceNamespace := stringFromMap(labels, "hypercdr.io/source-namespace")
		if sourceNamespace == "" {
			sourceNamespace = firstStringFromAny(backup["includedNamespaces"])
		}
		storageName := stringFromMap(backup, "storageLocation")
		completedAt := parseTimeFromAny(backup["completedAt"])
		if completedAt.IsZero() {
			completedAt = parseTimeFromAny(backup["createdAt"])
		}
		task, ok, err := r.findVeleroBackupTask(clusterID, name)
		if err != nil {
			r.logger.Error("failed to find scheduled backup task from inventory", "backup", name, "error", err)
			continue
		}
		if !ok {
			commandID := store.NewPublicID()
			task, err = r.store.CreateTask(store.TaskInput{
				ClusterID:        clusterID,
				AppID:            plan.AppID,
				ProtectionPlanID: plan.ID,
				Type:             "backup",
				Status:           "queued",
				CommandID:        commandID,
				Payload: map[string]any{
					"scheduled":        true,
					"sourceNamespace":  sourceNamespace,
					"storageRepo":      storageName,
					"veleroBackupName": name,
					"phase":            phase,
				},
			})
			if err != nil {
				r.logger.Error("failed to create scheduled backup task from inventory", "backup", name, "error", err)
				continue
			}
		}
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:      task.ID,
			Status:      "finalizing",
			Progress:    100,
			Payload:     backupTaskPayloadPatch(backup),
			MarkStarted: true,
		})
		point, err := r.store.CreateRestorePoint(store.RestorePointInput{
			ProtectionPlanID:  plan.ID,
			SourceClusterID:   clusterID,
			AppID:             plan.AppID,
			StorageRepoID:     plan.StorageRepoID,
			TaskCreatedAt:     task.CreatedAt,
			VeleroBackupName:  name,
			PointType:         "backup",
			Status:            "available",
			SizeBytes:         veleroBackupSizeBytes(backup),
			CompletedAt:       completedAt,
			SourceNamespace:   sourceNamespace,
			BackupTaskID:      task.ID,
			BackupStorageName: storageName,
			Metadata: map[string]any{
				"scheduled":        true,
				"phase":            phase,
				"velero":           backup,
				"size":             firstNonEmptyMap(mapFromAny(backup["restorePointSize"]), mapFromAny(backup["size"])),
				"sizeStatus":       backup["sizeStatus"],
				"sizeWarnings":     sliceFromAny(backup["sizeWarnings"]),
				"restorePointSize": firstNonEmptyMap(mapFromAny(backup["restorePointSize"]), mapFromAny(backup["size"])),
				"planStorageSize":  mapFromAny(backup["planStorageSize"]),
			},
		})
		if err != nil {
			r.logger.Error("failed to create restore point from scheduled backup", "backup", name, "error", err)
			continue
		}
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:   task.ID,
			Status:   "succeeded",
			Progress: 100,
			Payload:  backupTaskPayloadPatch(backup),
			MarkDone: true,
		})
		_ = r.store.AddTaskEvent(store.TaskEventInput{
			TaskID:  task.ID,
			Level:   "info",
			Reason:  "velero-schedule",
			Message: "scheduled velero backup completed",
			Payload: map[string]any{"velero": backup},
		})
		r.updateProtectionPlanStorageSizeFromVelero(plan.ID, backup)
		seen[name] = struct{}{}
		r.reconcileRetention(point.ProtectionPlanID, point.BackupTaskID)
	}
}

func (r *Router) handleVeleroBackupEvent(clusterID string, event protocol.VeleroEventPayload) (store.Task, error) {
	if event.BackupName == "" {
		return store.Task{}, nil
	}
	planID := event.PlanID
	if planID == "" && event.Labels != nil {
		planID = event.Labels["hypercdr.io/plan-id"]
	}
	if planID == "" {
		return store.Task{}, nil
	}
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err != nil {
		r.logger.Error("failed to load protection plan for velero event", "plan_id", planID, "backup", event.BackupName, "error", err)
		return store.Task{}, err
	}
	if !ok {
		return store.Task{}, nil
	}
	if plan.SourceClusterID != clusterID {
		return store.Task{}, nil
	}
	if strings.EqualFold(event.Phase, "Deleting") || strings.EqualFold(event.EventType, "backup_deleting") {
		return store.Task{}, nil
	}
	task, err := r.findOrCreateVeleroBackupTask(clusterID, plan, event)
	if err != nil {
		r.logger.Error("failed to upsert velero backup task", "backup", event.BackupName, "error", err)
		return store.Task{}, err
	}
	if !isTerminalVeleroPhase(event.Phase) && !task.CompletedAt.IsZero() {
		return task, nil
	}
	status := "running"
	markDone := false
	errorCode := ""
	errorMessage := ""
	switch event.EventType {
	case "backup_completed":
		status = "succeeded"
		markDone = true
	case "backup_failed":
		status = "failed"
		markDone = true
		errorCode = "VELERO_BACKUP_FAILED"
		errorMessage = detailedTaskFailureMessage(event.Message, event.Velero)
	default:
		status = "running"
	}
	if event.Phase == "Failed" || event.Phase == "FailedValidation" || event.Phase == "PartiallyFailed" || event.Phase == "Canceled" {
		status = "failed"
		markDone = true
		errorCode = "VELERO_BACKUP_FAILED"
		errorMessage = detailedTaskFailureMessage(event.Message, event.Velero)
	}
	if event.Phase == "Completed" {
		status = "succeeded"
		markDone = true
	}
	progress := event.Progress
	if markDone && status == "succeeded" {
		progress = 100
	}
	var completedPoint store.RestorePoint
	if status == "succeeded" {
		task, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:      task.ID,
			Status:      "finalizing",
			Progress:    100,
			Payload:     backupTaskPayloadPatch(event.Velero),
			MarkStarted: true,
		})
		if err != nil {
			r.logger.Error("failed to mark velero backup task finalizing", "task_id", task.ID, "backup", event.BackupName, "error", err)
			return task, err
		}
		completedPoint, err = r.createRestorePointFromVeleroEvent(clusterID, plan, task, event)
		if err != nil {
			r.logger.Error("failed to create restore point from velero event", "backup", event.BackupName, "error", err)
			return task, err
		}
	}
	task, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       task.ID,
		Status:       status,
		Progress:     progress,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
		Payload:      backupTaskPayloadPatch(event.Velero),
		MarkStarted:  true,
		MarkDone:     markDone,
	})
	if err != nil {
		r.logger.Error("failed to update velero backup task", "task_id", task.ID, "backup", event.BackupName, "error", err)
		return task, err
	}
	level := "info"
	if status == "failed" {
		level = "error"
	}
	_ = r.addTaskEventIfChanged(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   level,
		Reason:  event.EventType,
		Message: detailedTaskFailureMessage(event.Message, event.Velero),
		Payload: map[string]any{"velero": event.Velero},
	})
	if status == "succeeded" {
		if completedPoint.ID != "" {
			r.updateProtectionPlanStorageSizeFromVelero(plan.ID, event.Velero)
			r.reconcileRetention(completedPoint.ProtectionPlanID, completedPoint.BackupTaskID)
		}
	}
	return task, nil
}

func (r *Router) addTaskEventIfChanged(input store.TaskEventInput) error {
	incomingVelero := mapFromAny(input.Payload["velero"])
	if len(incomingVelero) == 0 {
		return r.store.AddTaskEvent(input)
	}
	events, err := r.store.ListTaskEvents(input.TaskID)
	if err != nil {
		return r.store.AddTaskEvent(input)
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Reason != input.Reason {
			continue
		}
		if sameVeleroEventPayload(mapFromAny(event.Payload["velero"]), incomingVelero) {
			return nil
		}
	}
	return r.store.AddTaskEvent(input)
}

func mapFromAny(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	default:
		return map[string]any{}
	}
}

func firstNonEmptyMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return map[string]any{}
}

func sliceFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func detailedTaskFailureMessage(fallback string, details map[string]any) string {
	messages := taskFailureMessagesFromDetails(details)
	if len(messages) == 0 {
		if fallback != "" {
			return fallback
		}
		return "Task failed"
	}
	prefix := fallback
	if strings.EqualFold(prefix, "velero backup failed") ||
		strings.HasPrefix(prefix, "Velero backup PartiallyFailed:") ||
		strings.HasPrefix(prefix, "Velero backup Failed:") ||
		strings.HasPrefix(prefix, "Velero backup FailedValidation:") ||
		strings.HasPrefix(prefix, "Velero backup Canceled:") {
		prefix = ""
	}
	if prefix == "" {
		return strings.Join(messages, "\n")
	}
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message), strings.TrimSpace(prefix)) {
			return strings.Join(messages, "\n")
		}
	}
	return prefix + "\n" + strings.Join(messages, "\n")
}

func taskFailurePayloadPatch(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	patch := map[string]any{}
	if velero := mapFromAny(details["velero"]); len(velero) > 0 {
		if volumeProgress := mapFromAny(velero["volumeProgress"]); len(volumeProgress) > 0 {
			patch["volumeProgress"] = volumeProgress
		}
		if size := mapFromAny(velero["size"]); len(size) > 0 {
			patch["size"] = size
		}
		if restorePointSize := mapFromAny(velero["restorePointSize"]); len(restorePointSize) > 0 {
			patch["restorePointSize"] = restorePointSize
		}
		if planStorageSize := mapFromAny(velero["planStorageSize"]); len(planStorageSize) > 0 {
			patch["planStorageSize"] = planStorageSize
		}
		if sizeStatus := stringFromMap(velero, "sizeStatus"); sizeStatus != "" {
			patch["sizeStatus"] = sizeStatus
		}
		if sizeWarnings := sliceFromAny(velero["sizeWarnings"]); len(sizeWarnings) > 0 {
			patch["sizeWarnings"] = sizeWarnings
		}
	}
	if messages := taskFailureMessagesFromDetails(details); len(messages) > 0 {
		patch["failureDetails"] = messages
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func taskFailureMessagesFromDetails(details map[string]any) []string {
	velero := details
	if nested := mapFromAny(details["velero"]); len(nested) > 0 {
		velero = nested
	}
	var messages []string
	volumeProgress := mapFromAny(velero["volumeProgress"])
	for _, raw := range sliceFromAny(volumeProgress["items"]) {
		item := mapFromAny(raw)
		message := strings.TrimSpace(fmt.Sprint(item["message"]))
		if message == "" || message == "<nil>" {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(item["name"]))
		phase := strings.TrimSpace(fmt.Sprint(item["phase"]))
		label := "Volume backup"
		if name != "" && name != "<nil>" {
			label += " " + name
		}
		if phase != "" && phase != "<nil>" {
			label += " " + phase
		}
		messages = append(messages, label+": "+humanizeBackupFailureMessage(message))
	}
	status := mapFromAny(velero["status"])
	if statusMessage := strings.TrimSpace(fmt.Sprint(status["message"])); statusMessage != "" && statusMessage != "<nil>" {
		messages = append(messages, humanizeBackupFailureMessage(statusMessage))
	}
	return dedupeStrings(messages)
}

func humanizeBackupFailureMessage(message string) string {
	if strings.Contains(message, "repository not initialized in the provided storage") {
		return message + "。Kopia 文件系统备份仓库不存在或未初始化，请重新配置/重试该集群的 BackupStorageLocation 后再执行同步；如果刚手动删除过对象存储 kopia 目录，需要先让系统重新初始化仓库。"
	}
	return message
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func backupTaskPayloadPatch(velero map[string]any) map[string]any {
	if len(velero) == 0 {
		return nil
	}
	patch := map[string]any{}
	if name := stringFromMap(velero, "name"); name != "" {
		patch["veleroBackupName"] = name
	}
	if storage := stringFromMap(velero, "storageLocation"); storage != "" {
		patch["backupStorageName"] = storage
		patch["storageLocation"] = storage
	}
	if namespaces := stringArrayFromAny(velero["includedNamespaces"]); len(namespaces) > 0 {
		patch["includedNamespaces"] = namespaces
		if _, ok := patch["sourceNamespace"]; !ok && len(namespaces) == 1 {
			patch["sourceNamespace"] = namespaces[0]
		}
	}
	if phase := stringFromMap(velero, "phase"); phase != "" {
		patch["phase"] = phase
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func taskProgressPayloadPatch(payload protocol.TaskProgressPayload) map[string]any {
	patch := backupTaskPayloadPatch(payload.Velero)
	if patch == nil {
		patch = map[string]any{}
	}
	progress := map[string]any{}
	if payload.TotalBytes > 0 {
		progress["totalBytes"] = payload.TotalBytes
		patch["totalBytes"] = payload.TotalBytes
	}
	if payload.SyncedBytes > 0 {
		progress["syncedBytes"] = payload.SyncedBytes
		patch["syncedBytes"] = payload.SyncedBytes
	}
	if payload.SpeedBytesPerSecond > 0 {
		progress["speedBytesPerSecond"] = payload.SpeedBytesPerSecond
		patch["speedBytesPerSecond"] = payload.SpeedBytesPerSecond
	}
	if payload.Percent > 0 {
		progress["percent"] = payload.Percent
		patch["percent"] = payload.Percent
	}
	if payload.EtaSeconds > 0 {
		progress["etaSeconds"] = payload.EtaSeconds
		patch["etaSeconds"] = payload.EtaSeconds
	}
	if len(progress) > 0 {
		patch["progressMetrics"] = progress
	}
	volumeProgress := mapFromAny(payload.Velero["volumeProgress"])
	if len(volumeProgress) > 0 {
		patch["volumeProgress"] = volumeProgress
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func taskCompletedPayloadPatch(payload protocol.TaskCompletedPayload) map[string]any {
	patch := backupTaskPayloadPatch(payload.Velero)
	if patch == nil {
		patch = map[string]any{}
	}
	restorePointSize := mapFromAny(payload.Velero["restorePointSize"])
	if len(restorePointSize) == 0 {
		restorePointSize = payload.Size
	}
	if len(restorePointSize) == 0 {
		restorePointSize = mapFromAny(payload.Velero["size"])
	}
	if len(restorePointSize) > 0 {
		patch["restorePointSize"] = restorePointSize
		patch["size"] = restorePointSize
		if totalBytes := int64FromAny(restorePointSize["totalBytes"]); totalBytes > 0 {
			patch["sizeBytes"] = totalBytes
			patch["totalBytes"] = totalBytes
		}
	}
	if planStorageSize := mapFromAny(payload.Velero["planStorageSize"]); len(planStorageSize) > 0 {
		patch["planStorageSize"] = planStorageSize
	}
	if sizeStatus := stringFromMap(payload.Velero, "sizeStatus"); sizeStatus != "" {
		patch["sizeStatus"] = sizeStatus
	}
	if sizeWarnings := sliceFromAny(payload.Velero["sizeWarnings"]); len(sizeWarnings) > 0 {
		patch["sizeWarnings"] = sizeWarnings
	}
	volumeProgress := mapFromAny(payload.Velero["volumeProgress"])
	if len(volumeProgress) > 0 {
		patch["volumeProgress"] = volumeProgress
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func sameVeleroEventPayload(existing map[string]any, incoming map[string]any) bool {
	if len(existing) == 0 || len(incoming) == 0 {
		return false
	}
	existingName := fmt.Sprint(existing["name"])
	incomingName := fmt.Sprint(incoming["name"])
	if existingName == "" || existingName != incomingName {
		return false
	}
	if existingKind := fmt.Sprint(existing["kind"]); existingKind != "" && fmt.Sprint(incoming["kind"]) != "" && existingKind != fmt.Sprint(incoming["kind"]) {
		return false
	}
	existingResourceVersion := fmt.Sprint(existing["resourceVersion"])
	incomingResourceVersion := fmt.Sprint(incoming["resourceVersion"])
	if existingResourceVersion != "" && incomingResourceVersion != "" {
		return existingResourceVersion == incomingResourceVersion
	}
	existingPhase := fmt.Sprint(existing["phase"])
	incomingPhase := fmt.Sprint(incoming["phase"])
	if existingPhase != incomingPhase {
		return false
	}
	if isTerminalVeleroPhase(incomingPhase) {
		return true
	}
	existingVolume := mapFromAny(existing["volumeProgress"])
	incomingVolume := mapFromAny(incoming["volumeProgress"])
	if len(existingVolume) == 0 || len(incomingVolume) == 0 {
		return false
	}
	return fmt.Sprint(existingVolume["bytesDone"]) == fmt.Sprint(incomingVolume["bytesDone"]) &&
		fmt.Sprint(existingVolume["totalBytes"]) == fmt.Sprint(incomingVolume["totalBytes"]) &&
		fmt.Sprint(existingVolume["knownTotal"]) == fmt.Sprint(incomingVolume["knownTotal"])
}

func isTerminalVeleroPhase(phase string) bool {
	switch phase {
	case "Completed", "PartiallyFailed", "Failed", "FailedValidation", "Canceled":
		return true
	default:
		return false
	}
}

func (r *Router) findOrCreateVeleroBackupTask(clusterID string, plan store.ProtectionPlan, event protocol.VeleroEventPayload) (store.Task, error) {
	if event.TaskID != "" {
		tasks, err := r.store.ListTasks(clusterID)
		if err != nil {
			return store.Task{}, err
		}
		for _, task := range tasks {
			if task.ID == event.TaskID && task.Type == "backup" {
				return task, nil
			}
		}
	}
	if task, ok, err := r.findVeleroBackupTask(clusterID, event.BackupName); err != nil || ok {
		return task, err
	}
	sourceNamespace := firstNonEmptyString(event.Labels["hypercdr.io/source-namespace"], firstStringFromStrings(event.IncludedNamespaces))
	commandID := store.NewPublicID()
	return r.store.CreateTask(store.TaskInput{
		ClusterID:        clusterID,
		AppID:            plan.AppID,
		ProtectionPlanID: plan.ID,
		Type:             "backup",
		Status:           "running",
		CommandID:        commandID,
		Payload: map[string]any{
			"scheduled":          true,
			"sourceNamespace":    sourceNamespace,
			"includedNamespaces": event.IncludedNamespaces,
			"storageRepo":        event.StorageLocation,
			"veleroBackupName":   event.BackupName,
			"phase":              event.Phase,
		},
	})
}

func (r *Router) findVeleroBackupTask(clusterID string, backupName string) (store.Task, bool, error) {
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.Type != "backup" {
			continue
		}
		if taskPayloadString(task.Payload, "veleroBackupName") == backupName {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) findTaskByID(clusterID string, taskID string) (store.Task, bool, error) {
	if taskID == "" {
		return store.Task{}, false, nil
	}
	tasks, err := r.store.ListTasks(clusterID)
	if err != nil {
		return store.Task{}, false, err
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return task, true, nil
		}
	}
	return store.Task{}, false, nil
}

func (r *Router) finishBackupCancelTask(clusterID string, cancelTask store.Task, completed protocol.TaskCompletedPayload) error {
	patch := taskCompletedPayloadPatch(completed)
	cancelTask, _, err := r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:      cancelTask.ID,
		Status:      "succeeded",
		Progress:    100,
		Payload:     patch,
		MarkStarted: true,
		MarkDone:    true,
	})
	if err != nil {
		return err
	}
	_ = r.addTaskEventIfChanged(store.TaskEventInput{
		TaskID:  cancelTask.ID,
		Level:   "info",
		Reason:  "completed",
		Message: completed.Message,
		Payload: map[string]any{"velero": completed.Velero},
	})
	targetTaskID := firstNonEmptyString(stringPayload(cancelTask.Payload, "targetTaskId"), stringPayload(completed.Velero, "targetTaskId"))
	if targetTaskID == "" {
		return errors.New("backup cancel target task id is missing")
	}
	target, ok, err := r.findTaskByID(clusterID, targetTaskID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("backup cancel target task not found")
	}
	if target.Type != "backup" {
		return fmt.Errorf("backup cancel target has unsupported task type %q", target.Type)
	}
	if !target.CompletedAt.IsZero() {
		return nil
	}
	targetPatch := map[string]any{
		"cancelTaskId":     cancelTask.ID,
		"cancelReason":     firstNonEmptyString(stringPayload(cancelTask.Payload, "reason"), "user_requested"),
		"canceledByUser":   true,
		"veleroBackupName": firstNonEmptyString(stringPayload(cancelTask.Payload, "veleroBackupName"), stringPayload(completed.Velero, "backupName")),
	}
	if deleted, ok := completed.Velero["deleted"].(bool); ok {
		targetPatch["veleroBackupDeleted"] = deleted
	}
	_, _, err = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       target.ID,
		Status:       "canceled",
		Progress:     target.Progress,
		ErrorCode:    "SYNC_FORCE_STOPPED",
		ErrorMessage: "Sync was force stopped by user.",
		Payload:      targetPatch,
		MarkDone:     true,
	})
	if err != nil {
		return err
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  target.ID,
		Level:   "warning",
		Reason:  "canceled",
		Message: "Sync was force stopped by user.",
		Payload: map[string]any{"cancelTaskId": cancelTask.ID, "velero": completed.Velero},
	})
	return nil
}

func (r *Router) markBackupCancelFailed(cancelTask store.Task, message string) {
	targetTaskID := stringPayload(cancelTask.Payload, "targetTaskId")
	if targetTaskID == "" {
		return
	}
	target, ok, err := r.findTaskByID(cancelTask.ClusterID, targetTaskID)
	if err != nil || !ok || target.Type != "backup" || !target.CompletedAt.IsZero() {
		return
	}
	if target.Status != "canceling" {
		return
	}
	if message == "" {
		message = "Force stop failed."
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       target.ID,
		Status:       "running",
		Progress:     target.Progress,
		ErrorCode:    "SYNC_FORCE_STOP_FAILED",
		ErrorMessage: message,
		Payload: map[string]any{
			"cancelTaskId": cancelTask.ID,
			"cancelFailed": true,
		},
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  target.ID,
		Level:   "error",
		Reason:  "cancel_failed",
		Message: message,
		Payload: map[string]any{"cancelTaskId": cancelTask.ID},
	})
}

func isForceStoppedBackupTask(task store.Task) bool {
	status := strings.ToLower(task.Status)
	return status == "canceling" || status == "canceled" || strings.EqualFold(task.ErrorCode, "SYNC_FORCE_STOPPED")
}

func (r *Router) createRestorePointFromVeleroEvent(clusterID string, plan store.ProtectionPlan, task store.Task, event protocol.VeleroEventPayload) (store.RestorePoint, error) {
	completedAt := event.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	sourceNamespace := firstNonEmptyString(event.Labels["hypercdr.io/source-namespace"], firstStringFromStrings(event.IncludedNamespaces), taskPayloadString(task.Payload, "sourceNamespace"))
	scheduled := taskPayloadBool(task.Payload, "scheduled")
	return r.store.CreateRestorePoint(store.RestorePointInput{
		ProtectionPlanID:  plan.ID,
		SourceClusterID:   clusterID,
		AppID:             plan.AppID,
		StorageRepoID:     plan.StorageRepoID,
		TaskCreatedAt:     task.CreatedAt,
		VeleroBackupName:  event.BackupName,
		PointType:         "backup",
		Status:            "available",
		SizeBytes:         veleroBackupSizeBytes(event.Velero),
		CompletedAt:       completedAt,
		SourceNamespace:   sourceNamespace,
		BackupTaskID:      task.ID,
		BackupStorageName: event.StorageLocation,
		Metadata: map[string]any{
			"scheduled":          scheduled,
			"phase":              event.Phase,
			"velero":             event.Velero,
			"size":               firstNonEmptyMap(mapFromAny(event.Velero["restorePointSize"]), mapFromAny(event.Velero["size"])),
			"sizeStatus":         event.Velero["sizeStatus"],
			"sizeWarnings":       sliceFromAny(event.Velero["sizeWarnings"]),
			"restorePointSize":   firstNonEmptyMap(mapFromAny(event.Velero["restorePointSize"]), mapFromAny(event.Velero["size"])),
			"planStorageSize":    mapFromAny(event.Velero["planStorageSize"]),
			"includedNamespaces": event.IncludedNamespaces,
			"sourceNamespaces":   event.IncludedNamespaces,
		},
	})
}

func (r *Router) createRestorePointFromBackup(task store.Task, veleroPayload map[string]any) (store.RestorePoint, error) {
	kind, _ := veleroPayload["kind"].(string)
	if kind != "Backup" {
		return store.RestorePoint{}, nil
	}
	backupName, _ := veleroPayload["name"].(string)
	if backupName == "" {
		return store.RestorePoint{}, nil
	}
	storageRepoID := ""
	if task.ProtectionPlanID != "" {
		plan, ok, err := r.store.GetProtectionPlan(task.ProtectionPlanID)
		if err != nil {
			return store.RestorePoint{}, err
		}
		if ok && plan.SourceClusterID != task.ClusterID {
			return store.RestorePoint{}, nil
		}
		if ok {
			storageRepoID = plan.StorageRepoID
		}
	}

	sourceNamespace, _ := task.Payload["sourceNamespace"].(string)
	labelSelector, _ := task.Payload["labelSelector"].(string)
	storageName, _ := task.Payload["storageRepo"].(string)
	if manifest, ok := veleroPayload["manifest"].(map[string]any); ok {
		if spec, ok := manifest["spec"].(map[string]any); ok {
			if value, ok := spec["storageLocation"].(string); ok && value != "" {
				storageName = value
			}
		}
	}
	completedAt := task.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}

	point, err := r.store.CreateRestorePoint(store.RestorePointInput{
		ProtectionPlanID:  task.ProtectionPlanID,
		SourceClusterID:   task.ClusterID,
		AppID:             task.AppID,
		StorageRepoID:     storageRepoID,
		TaskCreatedAt:     task.CreatedAt,
		VeleroBackupName:  backupName,
		PointType:         "backup",
		Status:            "available",
		SizeBytes:         veleroBackupSizeBytes(veleroPayload),
		CompletedAt:       completedAt,
		SourceNamespace:   sourceNamespace,
		LabelSelector:     labelSelector,
		BackupTaskID:      task.ID,
		BackupStorageName: storageName,
		Metadata: map[string]any{
			"velero":           veleroPayload,
			"size":             firstNonEmptyMap(mapFromAny(veleroPayload["restorePointSize"]), mapFromAny(veleroPayload["size"])),
			"sizeStatus":       veleroPayload["sizeStatus"],
			"sizeWarnings":     sliceFromAny(veleroPayload["sizeWarnings"]),
			"restorePointSize": firstNonEmptyMap(mapFromAny(veleroPayload["restorePointSize"]), mapFromAny(veleroPayload["size"])),
			"planStorageSize":  mapFromAny(veleroPayload["planStorageSize"]),
		},
	})
	if err != nil {
		return store.RestorePoint{}, err
	}
	if task.ID != "" && point.ID != "" {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:         task.ID,
			RestorePointID: point.ID,
			Payload: map[string]any{
				"restorePointId": point.ID,
			},
		})
	}
	r.updateProtectionPlanStorageSizeFromVelero(task.ProtectionPlanID, veleroPayload)
	return point, nil
}

func (r *Router) updateProtectionPlanStorageSizeFromVelero(planID string, veleroPayload map[string]any) {
	if planID == "" {
		return
	}
	planStorageSize := mapFromAny(veleroPayload["planStorageSize"])
	if len(planStorageSize) == 0 {
		return
	}
	if _, _, err := r.store.UpdateProtectionPlanStorageSize(planID, planStorageSize); err != nil {
		r.logger.Error("failed to update protection plan storage size", "plan_id", planID, "error", err)
	}
}

func (r *Router) reconcileRetention(planID string, triggerTaskID string) {
	if planID == "" {
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err != nil {
		r.logger.Error("failed to load protection plan for retention", "plan_id", planID, "error", err)
		return
	}
	if !ok || plan.PolicyID == "" {
		return
	}
	policy, ok, err := r.findPolicy(plan.PolicyID)
	if err != nil {
		r.logger.Error("failed to load policy for retention", "plan_id", planID, "policy_id", plan.PolicyID, "error", err)
		return
	}
	if !ok || policy.RetentionCount <= 0 {
		return
	}
	points, err := r.store.ListRestorePoints(store.RestorePointFilter{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: planID,
	})
	if err != nil {
		r.logger.Error("failed to list restore points for retention", "plan_id", planID, "error", err)
		return
	}
	clusterTasks, err := r.store.ListTasks(plan.SourceClusterID)
	if err != nil {
		r.logger.Error("failed to list tasks for retention", "plan_id", planID, "error", err)
		return
	}
	activeRetentionTasks := map[string]struct{}{}
	staleTaskCutoff := time.Now().UTC().Add(-35 * time.Minute)
	for _, task := range clusterTasks {
		if task.Type != "retention-cleanup" || !isActiveTaskStatus(task.Status) {
			continue
		}
		lastActivity := task.StartedAt
		if lastActivity.IsZero() {
			lastActivity = task.AcceptedAt
		}
		if lastActivity.IsZero() {
			lastActivity = task.DispatchedAt
		}
		if lastActivity.IsZero() {
			lastActivity = task.CreatedAt
		}
		if lastActivity.After(staleTaskCutoff) {
			activeRetentionTasks[task.ID] = struct{}{}
		}
	}
	available := make([]store.RestorePoint, 0, len(points))
	for _, point := range points {
		if point.Status != "available" || point.VeleroBackupName == "" {
			continue
		}
		state, _ := point.Metadata["retentionState"].(string)
		if state == "deleting" {
			taskID, _ := point.Metadata["retentionCleanupTask"].(string)
			if _, ok := activeRetentionTasks[taskID]; ok {
				continue
			}
		}
		available = append(available, point)
	}
	if len(available) <= policy.RetentionCount {
		return
	}
	sort.SliceStable(available, func(i, j int) bool {
		left := available[i].CompletedAt
		if left.IsZero() {
			left = available[i].CreatedAt
		}
		right := available[j].CompletedAt
		if right.IsZero() {
			right = available[j].CreatedAt
		}
		return left.After(right)
	})
	candidates := available[policy.RetentionCount:]
	if len(candidates) == 0 {
		return
	}
	restorePoints := make([]map[string]any, 0, len(candidates))
	for _, point := range candidates {
		restorePoints = append(restorePoints, map[string]any{
			"id":               point.ID,
			"taskCreatedAt":    point.TaskCreatedAt,
			"veleroBackupName": point.VeleroBackupName,
			"namespace":        r.agentNamespace(),
		})
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"retentionState": "pending_delete",
			},
		})
	}
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: plan.ID,
		Type:             "retention-cleanup",
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"planId":        plan.ID,
			"triggerTaskId": triggerTaskID,
			"restorePoints": restorePoints,
		},
	})
	if err != nil {
		r.logger.Error("failed to create retention cleanup task", "plan_id", plan.ID, "error", err)
		return
	}
	for _, point := range candidates {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"retentionState":       "deleting",
				"retentionCleanupTask": task.ID,
			},
		})
	}
	conn, ok := r.hub.get(plan.SourceClusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; retention cleanup will be dispatched after reconnect",
		})
		r.markBackupTaskRetentionWarning(triggerTaskID, "RETENTION_CLEANUP_PENDING", "Expired restore points were selected for deletion, but the source cluster agent is offline. Cleanup will retry after reconnect.")
		return
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "failed",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
			MarkDone:     true,
		})
		r.markRetentionCleanupFailed(task, err.Error())
		r.markBackupTaskRetentionWarning(triggerTaskID, "RETENTION_CLEANUP_FAILED", err.Error())
		r.logger.Error("failed to dispatch retention cleanup", "task_id", task.ID, "plan_id", plan.ID, "error", err)
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
}

func (r *Router) createRestorePointDeleteTask(clusterID string, points []store.RestorePoint) (store.Task, string, error) {
	if clusterID == "" || len(points) == 0 {
		return store.Task{}, "", errors.New("restore point delete target required")
	}
	restorePoints := make([]map[string]any, 0, len(points))
	protectionPlanID := ""
	for _, point := range points {
		if protectionPlanID == "" {
			protectionPlanID = point.ProtectionPlanID
		}
		restorePoints = append(restorePoints, map[string]any{
			"id":               point.ID,
			"taskCreatedAt":    point.TaskCreatedAt,
			"veleroBackupName": point.VeleroBackupName,
			"namespace":        r.agentNamespace(),
		})
	}
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        clusterID,
		ProtectionPlanID: protectionPlanID,
		Type:             "retention-cleanup",
		Status:           "queued",
		CommandID:        commandID,
		Payload: map[string]any{
			"planId":        protectionPlanID,
			"manualDelete":  true,
			"restorePoints": restorePoints,
		},
	})
	if err != nil {
		return store.Task{}, "", err
	}
	for _, point := range points {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"retentionState":       "deleting",
				"retentionCleanupTask": task.ID,
				"deleteRequestedAt":    time.Now().UTC().Format(time.RFC3339),
				"deleteRequestedBy":    "manual",
			},
		})
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "manual_restore_point_delete",
		Message: "restore point delete requested",
		Payload: map[string]any{"restorePoints": restorePoints},
	})
	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; restore point delete will be dispatched after reconnect",
		})
		return task, "agent is offline; restore point delete task remains queued", nil
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "failed",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
			MarkDone:     true,
		})
		r.markRetentionCleanupFailed(task, err.Error())
		return task, "restore point delete task created but dispatch failed", nil
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: "restore point delete task dispatched to agent",
	})
	return task, "", nil
}

func (r *Router) createProtectionCleanupTask(plan store.ProtectionPlan) (store.Task, string, error) {
	if plan.ID == "" || plan.SourceClusterID == "" {
		return store.Task{}, "", errors.New("protection cleanup target required")
	}
	sourceNamespaces, err := r.protectionPlanNamespaces(plan)
	if err != nil {
		return store.Task{}, "", err
	}
	repo, ok, err := r.store.GetStorageRepository(plan.StorageRepoID)
	if err != nil {
		return store.Task{}, "", err
	}
	if !ok {
		return store.Task{}, "", errors.New("storage repository not found")
	}
	storageName := storageDomainBSLName(repo, plan.SourceClusterID)
	points, err := r.store.ListRestorePoints(store.RestorePointFilter{
		ClusterID:        plan.SourceClusterID,
		ProtectionPlanID: plan.ID,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	restorePoints := make([]map[string]any, 0, len(points))
	for _, point := range points {
		if point.Status == "deleted" || point.VeleroBackupName == "" {
			continue
		}
		restorePoints = append(restorePoints, map[string]any{
			"id":               point.ID,
			"taskCreatedAt":    point.TaskCreatedAt,
			"veleroBackupName": point.VeleroBackupName,
			"namespace":        r.agentNamespace(),
		})
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: point.Status,
			Metadata: map[string]any{
				"protectionCleanupState": "pending_delete",
				"cleanupRequestedAt":     time.Now().UTC().Format(time.RFC3339),
			},
		})
	}
	restoreNames, err := r.protectionPlanRestoreNames(plan.ID)
	if err != nil {
		return store.Task{}, "", err
	}
	sourcePayload := map[string]any{
		"planId":                 plan.ID,
		"cleanupMode":            "source",
		"scheduleName":           scheduleNameForPlan(plan.ID),
		"backupNamePrefix":       scheduleNameForPlan(plan.ID),
		"namespace":              r.agentNamespace(),
		"sourceNamespaces":       sourceNamespaces,
		"storageRepo":            storageName,
		"storageRepoDisplayName": repo.Name,
		"sourceClusterId":        plan.SourceClusterID,
		"objectPrefix":           storageDomainPrefix(plan.SourceClusterID),
		"cleanupObjectStorage":   true,
		"restorePoints":          restorePoints,
		"restoreNames":           restoreNames,
	}
	task, warning, err := r.createAndDispatchProtectionCleanupTask(plan, plan.SourceClusterID, sourcePayload, "source protection cleanup task dispatched to agent")
	if err != nil {
		return store.Task{}, "", err
	}
	warnings := []string{}
	if warning != "" {
		warnings = append(warnings, warning)
	}
	if plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID {
		targetPayload := map[string]any{
			"planId":                 plan.ID,
			"cleanupMode":            "target",
			"backupNamePrefix":       scheduleNameForPlan(plan.ID),
			"namespace":              r.agentNamespace(),
			"sourceNamespaces":       sourceNamespaces,
			"storageRepo":            storageName,
			"storageRepoDisplayName": repo.Name,
			"sourceClusterId":        plan.SourceClusterID,
			"objectPrefix":           storageDomainPrefix(plan.SourceClusterID),
			"cleanupObjectStorage":   false,
			"restorePoints":          restorePoints,
			"restoreNames":           restoreNames,
		}
		_, targetWarning, err := r.createAndDispatchProtectionCleanupTask(plan, plan.TargetClusterID, targetPayload, "target protection cleanup task dispatched to agent")
		if err != nil {
			return store.Task{}, "", err
		}
		if targetWarning != "" {
			warnings = append(warnings, targetWarning)
		}
	}
	return task, strings.Join(warnings, "; "), nil
}

func (r *Router) protectionPlanRestoreNames(planID string) ([]string, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, task := range tasks {
		if task.ProtectionPlanID != planID && stringPayload(task.Payload, "protectionPlanId") != planID && stringPayload(task.Payload, "planId") != planID {
			continue
		}
		switch task.Type {
		case "restore", "drill", "takeover", "failback":
		default:
			continue
		}
		name := strings.TrimSpace(stringPayload(task.Payload, "veleroBackupName"))
		if name != "" {
			names = append(names, name)
		}
	}
	return uniqueNonEmptyStrings(names), nil
}

func (r *Router) createAndDispatchProtectionCleanupTask(plan store.ProtectionPlan, clusterID string, payload map[string]any, dispatchedMessage string) (store.Task, string, error) {
	commandID := store.NewPublicID()
	task, err := r.store.CreateTask(store.TaskInput{
		ClusterID:        clusterID,
		ProtectionPlanID: plan.ID,
		Type:             "protection-cleanup",
		Status:           "queued",
		CommandID:        commandID,
		Payload:          payload,
	})
	if err != nil {
		return store.Task{}, "", err
	}
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "protection_cleanup_requested",
		Message: "protection resource cleanup requested",
		Payload: map[string]any{
			"planId":            plan.ID,
			"cleanupMode":       stringPayload(payload, "cleanupMode"),
			"clusterId":         clusterID,
			"scheduleName":      stringPayload(payload, "scheduleName"),
			"restorePointCount": len(retentionRestorePointsFromAny(payload["restorePoints"])),
		},
	})
	conn, ok := r.hub.get(clusterID)
	if !ok {
		_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "AGENT_OFFLINE",
			ErrorMessage: "agent is not connected; protection cleanup will be dispatched after reconnect",
		})
		return task, "agent is offline; protection cleanup task remains queued", nil
	}
	if err := r.dispatchStoredTask(conn, task); err != nil {
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
			TaskID:       task.ID,
			Status:       "queued",
			Progress:     0,
			ErrorCode:    "DISPATCH_FAILED",
			ErrorMessage: err.Error(),
		})
		return task, "protection cleanup task created but dispatch failed", nil
	}
	task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:   task.ID,
		Status:   "dispatched",
		Progress: 0,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  task.ID,
		Level:   "info",
		Reason:  "dispatched",
		Message: dispatchedMessage,
	})
	return task, "", nil
}

func (r *Router) protectionPlanNamespaces(plan store.ProtectionPlan) ([]string, error) {
	sourceNamespaces := []string{}
	appIDs := plan.AppIDs
	if len(appIDs) == 0 && plan.AppID != "" {
		appIDs = []string{plan.AppID}
	}
	for _, appID := range appIDs {
		app, ok, err := r.store.GetApplication(appID)
		if err != nil {
			return nil, err
		}
		if ok && app.Namespace != "" && !slices.Contains(sourceNamespaces, app.Namespace) {
			sourceNamespaces = append(sourceNamespaces, app.Namespace)
		}
	}
	if len(sourceNamespaces) == 0 {
		return nil, errors.New("protection plan has no application namespaces")
	}
	return sourceNamespaces, nil
}

func (r *Router) finishRetentionCleanupTask(task store.Task, veleroPayload map[string]any) {
	deletedRaw, _ := veleroPayload["deleted"].([]any)
	if len(deletedRaw) == 0 {
		if deletedStrings, ok := veleroPayload["deleted"].([]string); ok {
			for _, id := range deletedStrings {
				r.markRestorePointDeleted(id)
			}
			r.clearBackupTaskRetentionWarning(stringPayload(task.Payload, "triggerTaskId"))
			return
		}
	}
	for _, item := range deletedRaw {
		if id, ok := item.(string); ok {
			r.markRestorePointDeleted(id)
		}
	}
	r.clearBackupTaskRetentionWarning(stringPayload(task.Payload, "triggerTaskId"))
}

func (r *Router) finishProtectionCleanupTask(task store.Task, veleroPayload map[string]any) {
	planID := firstNonEmptyString(task.ProtectionPlanID, stringPayload(task.Payload, "planId"))
	if planID == "" {
		r.logger.Warn("protection cleanup completed without plan id", "task_id", task.ID)
		return
	}
	plan, ok, err := r.store.GetProtectionPlan(planID)
	if err != nil {
		r.logger.Error("failed to load protection plan before final cleanup", "plan_id", planID, "task_id", task.ID, "error", err)
		return
	}
	if !ok {
		r.logger.Warn("protection cleanup completed for missing plan", "plan_id", planID, "task_id", task.ID)
		return
	}
	complete, err := r.protectionCleanupTasksComplete(plan)
	if err != nil {
		r.logger.Error("failed to inspect protection cleanup task completion", "plan_id", planID, "task_id", task.ID, "error", err)
		return
	}
	if !complete {
		r.logger.Info("protection cleanup task completed; waiting for remaining cleanup tasks", "plan_id", planID, "task_id", task.ID, "mode", stringPayload(task.Payload, "cleanupMode"))
		return
	}
	if _, ok, err := r.store.CleanupProtectionPlanRecords(planID); err != nil {
		r.logger.Error("failed to physically cleanup protection plan records", "plan_id", planID, "task_id", task.ID, "error", err)
		_, _, _ = r.store.UpdateProtectionPlanStatus(planID, "cleanup_failed")
	} else if !ok {
		r.logger.Warn("protection cleanup completed for missing plan", "plan_id", planID, "task_id", task.ID)
	}
}

func (r *Router) protectionCleanupTasksComplete(plan store.ProtectionPlan) (bool, error) {
	tasks, err := r.store.ListTasks("")
	if err != nil {
		return false, err
	}
	expectTarget := plan.TargetClusterID != "" && plan.TargetClusterID != plan.SourceClusterID
	sourceDone := false
	targetDone := !expectTarget
	for _, item := range tasks {
		if item.ProtectionPlanID != plan.ID || item.Type != "protection-cleanup" {
			continue
		}
		mode := stringPayload(item.Payload, "cleanupMode")
		if mode == "" {
			mode = "source"
		}
		if item.Status != "succeeded" {
			return false, nil
		}
		if mode == "target" {
			targetDone = true
		} else {
			sourceDone = true
		}
	}
	return sourceDone && targetDone, nil
}

func (r *Router) markRestorePointDeleted(id string) {
	if id == "" {
		return
	}
	_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
		ID:     id,
		Status: "deleted",
		Metadata: map[string]any{
			"retentionState": "deleted",
			"deletedAt":      time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (r *Router) markRetentionCleanupFailed(task store.Task, message string) {
	command := retentionCleanupCommandFromPayload(task.Payload)
	for _, point := range command.RestorePoints {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: "available",
			Metadata: map[string]any{
				"retentionState": "delete_failed",
				"deleteError":    message,
			},
		})
	}
	r.markBackupTaskRetentionWarning(stringPayload(task.Payload, "triggerTaskId"), "RETENTION_CLEANUP_FAILED", message)
}

func (r *Router) markProtectionCleanupFailed(task store.Task, message string) {
	planID := firstNonEmptyString(task.ProtectionPlanID, stringPayload(task.Payload, "planId"))
	if planID != "" {
		if _, _, err := r.store.UpdateProtectionPlanStatus(planID, "cleanup_failed"); err != nil {
			r.logger.Error("failed to mark protection plan cleanup failed", "plan_id", planID, "task_id", task.ID, "error", err)
		}
	}
	command := protectionCleanupCommandFromPayload(task.Payload)
	for _, point := range command.RestorePoints {
		_, _, _ = r.store.UpdateRestorePointState(store.RestorePointStateInput{
			ID:     point.ID,
			Status: "available",
			Metadata: map[string]any{
				"protectionCleanupState": "delete_failed",
				"cleanupError":           message,
			},
		})
	}
}

func (r *Router) markBackupTaskRetentionWarning(taskID string, code string, message string) {
	if taskID == "" {
		return
	}
	if message == "" {
		message = "Expired restore point cleanup did not complete."
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{
		TaskID:       taskID,
		ErrorCode:    code,
		ErrorMessage: message,
	})
	_ = r.store.AddTaskEvent(store.TaskEventInput{
		TaskID:  taskID,
		Level:   "warning",
		Reason:  code,
		Message: message,
	})
}

func (r *Router) clearBackupTaskRetentionWarning(taskID string) {
	if taskID == "" {
		return
	}
	_, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: taskID})
}

func newRejectedMessage(reason string, message string) protocol.Message[protocol.RegisterRejectedPayload] {
	return protocol.Message[protocol.RegisterRejectedPayload]{
		Version:     protocol.Version,
		MessageID:   store.NewPublicID(),
		MessageKind: protocol.MessageKindResponse,
		Type:        protocol.MessagePlatformRegisterRejected,
		Timestamp:   time.Now().UTC(),
		Payload: protocol.RegisterRejectedPayload{
			Reason:    reason,
			ErrorCode: reason,
			Message:   message,
		},
	}
}

func (r *Router) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started := time.Now()
		requestID := store.NewPublicID()
		w.Header().Set("X-Request-ID", requestID)
		if !strings.HasPrefix(req.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), requestIDContextKey{}, requestID)))
			r.logger.Info("http request", "method", req.Method, "path", req.URL.Path, "request_id", requestID, "duration_ms", time.Since(started).Milliseconds())
			return
		}
		recorder := &diagnosticResponseWriter{ResponseWriter: w}
		req = req.WithContext(context.WithValue(req.Context(), requestIDContextKey{}, requestID))
		next.ServeHTTP(recorder, req)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started).Milliseconds()
		r.logger.Info("http request",
			"method", req.Method,
			"path", req.URL.Path,
			"status", status,
			"request_id", requestID,
			"duration_ms", duration,
		)
		if strings.HasPrefix(req.URL.Path, "/api/v1/") && !strings.HasPrefix(req.URL.Path, "/api/v1/diagnostic-logs") && (status >= 400 || req.Method != http.MethodGet) {
			input := store.DiagnosticLogInput{Scope: "system", Level: "info", Component: "platform-api", Operation: req.Method + " " + req.URL.Path, Message: "API request completed", RequestID: requestID, Status: strconv.Itoa(status), DurationMS: duration, Details: map[string]any{"method": req.Method, "path": req.URL.Path, "httpStatus": status}}
			if status >= 400 {
				input.Level = "error"
				input.Message = "API request failed"
				input.ErrorCode = http.StatusText(status)
			}
			if user, ok := requestUser(req); ok && user.TenantID != "" {
				input.Scope = "tenant"
				input.TenantID = user.TenantID
				input.Details["userId"] = user.ID
			}
			if _, err := r.store.CreateDiagnosticLog(input); err != nil {
				r.logger.Error("write diagnostic access log failed", "request_id", requestID, "error", err)
			}
		}
	})
}

type diagnosticResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *diagnosticResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *diagnosticResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (r *Router) diagnosticLogFilter(req *http.Request) (store.DiagnosticLogFilter, error) {
	q := req.URL.Query()
	user, ok := requestUser(req)
	filter := store.DiagnosticLogFilter{TenantID: q.Get("tenantId"), Scope: q.Get("scope"), Level: q.Get("level"), Component: q.Get("component"), ClusterID: q.Get("clusterId"), TaskID: q.Get("taskId"), Query: strings.TrimSpace(q.Get("q"))}
	filter.Limit, _ = strconv.Atoi(q.Get("limit"))
	filter.Offset, _ = strconv.Atoi(q.Get("offset"))
	if filter.Limit <= 0 || filter.Limit > 5000 {
		filter.Limit = 200
	}
	if value := q.Get("from"); value != "" {
		filter.From, _ = time.Parse(time.RFC3339, value)
	}
	if value := q.Get("to"); value != "" {
		filter.To, _ = time.Parse(time.RFC3339, value)
	}
	if ok && !user.SystemAdmin {
		filter.TenantID = user.TenantID
		filter.Scope = "tenant"
	}
	if filter.Scope == "system" && (!ok || !user.SystemAdmin) {
		return filter, errors.New("system administrator permission is required")
	}
	if filter.ClusterID != "" {
		clusters, err := r.store.ListClusters()
		if err != nil {
			return filter, err
		}
		found := false
		for _, cluster := range clusters {
			if cluster.ID == filter.ClusterID && (user.SystemAdmin || cluster.TenantID == user.TenantID) && (filter.TenantID == "" || cluster.TenantID == filter.TenantID) {
				found = true
				break
			}
		}
		if !found {
			return filter, errors.New("cluster not found")
		}
	}
	return filter, nil
}

func (r *Router) listDiagnosticLogs(w http.ResponseWriter, req *http.Request) {
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "diagnostic_log_scope_forbidden", "message": err.Error()})
		return
	}
	items, err := r.store.ListDiagnosticLogs(filter)
	if err != nil {
		r.logger.Error("list diagnostic logs failed", "error", err)
		writeJSON(w, 500, map[string]any{"error": "list_diagnostic_logs_failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"items": nonNilSlice(items), "limit": filter.Limit, "offset": filter.Offset})
}

func (r *Router) exportDiagnosticLogs(w http.ResponseWriter, req *http.Request) {
	filter, err := r.diagnosticLogFilter(req)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "diagnostic_log_scope_forbidden", "message": err.Error()})
		return
	}
	filter.Limit = 5000
	filter.Offset = 0
	items, err := r.store.ListDiagnosticLogs(filter)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "export_diagnostic_logs_failed"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="hypercdr-diagnostic-logs.jsonl"`)
	encoder := json.NewEncoder(w)
	for _, item := range items {
		_ = encoder.Encode(item)
	}
}

func (r *Router) diagnosticLogSources(w http.ResponseWriter, req *http.Request) {
	user, ok := requestUser(req)
	if !ok {
		writeJSON(w, 401, map[string]any{"error": "authentication_required"})
		return
	}
	clusters, err := r.store.ListClusters()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "list_log_sources_failed"})
		return
	}
	items := []map[string]any{}
	for _, cluster := range clusters {
		if user.SystemAdmin || cluster.TenantID == user.TenantID {
			items = append(items, map[string]any{"id": cluster.ID, "tenantId": cluster.TenantID, "name": cluster.Name, "connectionStatus": cluster.ConnectionStatus})
		}
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (r *Router) collectClusterLogs(w http.ResponseWriter, req *http.Request) {
	clusterID := req.PathValue("id")
	var body struct {
		Component string    `json:"component"`
		Since     time.Time `json:"since"`
		TailLines int64     `json:"tailLines"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid_json"})
		return
	}
	allowed := map[string]bool{"comm-agent": true, "velero": true, "node-agent": true}
	if !allowed[body.Component] {
		writeJSON(w, 400, map[string]any{"error": "unsupported_log_component", "message": "Component must be comm-agent, velero, or node-agent."})
		return
	}
	if body.Since.IsZero() {
		body.Since = time.Now().UTC().Add(-30 * time.Minute)
	}
	if body.Since.Before(time.Now().UTC().Add(-24 * time.Hour)) {
		writeJSON(w, 400, map[string]any{"error": "log_range_too_large", "message": "Cluster log collection is limited to the last 24 hours."})
		return
	}
	if body.TailLines <= 0 || body.TailLines > 2000 {
		body.TailLines = 1000
	}
	conn, ok := r.hub.get(clusterID)
	if !ok {
		writeJSON(w, 409, map[string]any{"error": "agent_offline", "message": "Cluster is offline. Logs cannot be collected in real time."})
		return
	}
	requestID := store.NewPublicID()
	waiter := make(chan protocol.LogReportPayload, 1)
	r.logRequestMu.Lock()
	r.logRequests[requestID] = waiter
	r.logRequestMu.Unlock()
	defer func() { r.logRequestMu.Lock(); delete(r.logRequests, requestID); r.logRequestMu.Unlock() }()
	message := protocol.Message[protocol.LogRequestPayload]{Version: protocol.Version, MessageID: store.NewPublicID(), MessageKind: protocol.MessageKindRequest, Type: protocol.MessagePlatformLogRequest, ClusterID: clusterID, Timestamp: time.Now().UTC(), Payload: protocol.LogRequestPayload{RequestID: requestID, Component: body.Component, Since: body.Since, TailLines: body.TailLines}}
	if err := conn.WriteJSON(message); err != nil {
		writeJSON(w, 502, map[string]any{"error": "log_request_send_failed", "message": err.Error()})
		return
	}
	select {
	case report := <-waiter:
		if report.ErrorCode != "" {
			writeJSON(w, 502, map[string]any{"error": strings.ToLower(report.ErrorCode), "message": report.Message, "requestId": requestID})
			return
		}
		writeJSON(w, 200, map[string]any{"requestId": requestID, "status": "completed", "count": len(report.Entries), "truncated": report.Truncated, "message": "Cluster logs collected."})
	case <-time.After(25 * time.Second):
		writeJSON(w, 504, map[string]any{"error": "log_collection_timeout", "message": "The cluster agent did not respond. Upgrade comm-agent before using remote log collection.", "requestId": requestID})
	}
}

func sanitizeDiagnosticMessage(message string) string {
	message = strings.TrimSpace(message)
	var object map[string]any
	if json.Unmarshal([]byte(message), &object) == nil {
		for key := range object {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			if strings.Contains(normalized, "password") || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "credential") || strings.Contains(normalized, "accesskey") {
				object[key] = "[REDACTED]"
			}
		}
		if value, err := json.Marshal(object); err == nil {
			return string(value)
		}
	}
	if index := strings.Index(strings.ToLower(message), "bearer "); index >= 0 {
		return message[:index] + "Bearer [REDACTED]"
	}
	return message
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func nonNilSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func decodeJSON(req *http.Request, target any) error {
	defer req.Body.Close()
	return json.NewDecoder(req.Body).Decode(target)
}

func (r *Router) publicBaseURL(req *http.Request) string {
	if r.cfg.PublicBaseURL != "" {
		return r.cfg.PublicBaseURL
	}
	proto := req.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
	}
	host := req.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = req.Host
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func (r *Router) agentWSEndpoint(req *http.Request) string {
	if r.cfg.AgentWSEndpoint != "" {
		return r.cfg.AgentWSEndpoint
	}
	base := r.publicBaseURL(req)
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/ws/agent"
	}
	return "ws://" + strings.TrimPrefix(base, "http://") + "/ws/agent"
}

func (r *Router) registryHost() string {
	registry := strings.TrimSpace(r.cfg.ImageRegistry)
	if registry == "" {
		registry = strings.TrimSpace(r.cfg.AgentImage)
	}
	registry = strings.TrimPrefix(strings.TrimPrefix(registry, "https://"), "http://")
	if registry == "" {
		return ""
	}
	host := strings.Split(registry, "/")[0]
	return host
}

const prepareNodeScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

REGISTRY_HOST="{{REGISTRY_HOST}}"
REGISTRY_CA_URL="{{REGISTRY_CA_URL}}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry-host)
      REGISTRY_HOST="${2:-}"
      shift 2
      ;;
    --registry-ca-url)
      REGISTRY_CA_URL="${2:-}"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$REGISTRY_HOST" ]]; then
  echo "--registry-host is required" >&2
  exit 2
fi
if [[ -z "$REGISTRY_CA_URL" ]]; then
  echo "--registry-ca-url is required" >&2
  exit 2
fi

run_root() {
  if [[ "$(id -u)" == "0" ]]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    echo "root privileges are required; run as root or install sudo" >&2
    exit 1
  fi
}

download_ca() {
  local output="$1"
  if command -v curl >/dev/null 2>&1; then
    if [[ "$REGISTRY_CA_URL" == https://* ]]; then
      curl -k -fsSL "$REGISTRY_CA_URL" -o "$output"
    else
      curl -fsSL "$REGISTRY_CA_URL" -o "$output"
    fi
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    if [[ "$REGISTRY_CA_URL" == https://* ]]; then
      wget --no-check-certificate -qO "$output" "$REGISTRY_CA_URL"
    else
      wget -qO "$output" "$REGISTRY_CA_URL"
    fi
    return
  fi
  echo "curl or wget is required" >&2
  exit 1
}

ensure_containerd_config_path() {
  local config_file="/etc/containerd/config.toml"
  if ! command -v containerd >/dev/null 2>&1 && ! run_root test -d /etc/containerd; then
    return 0
  fi
  run_root mkdir -p /etc/containerd
  if ! run_root test -f "$config_file"; then
    if command -v containerd >/dev/null 2>&1; then
      containerd config default >"${TMP_DIR}/containerd-config.toml"
    else
      cat >"${TMP_DIR}/containerd-config.toml" <<EOF
version = 2
EOF
    fi
    run_root install -m 0644 "${TMP_DIR}/containerd-config.toml" "$config_file"
  fi

  if run_root grep -Eq '^[[:space:]]*config_path[[:space:]]*=' "$config_file"; then
    run_root sed -i -E 's#^([[:space:]]*config_path[[:space:]]*=).*#\1 "/etc/containerd/certs.d"#' "$config_file"
    return 0
  fi

  if grep -q '^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]' "$config_file"; then
    awk '
      /^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]/ && inserted == 0 {
        print
        print "  config_path = \"/etc/containerd/certs.d\""
        inserted = 1
        next
      }
      { print }
    ' "$config_file" >"${TMP_DIR}/containerd-config.toml"
  else
    cp "$config_file" "${TMP_DIR}/containerd-config.toml"
    cat >>"${TMP_DIR}/containerd-config.toml" <<EOF

[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
EOF
  fi
  run_root install -m 0644 "${TMP_DIR}/containerd-config.toml" "$config_file"
}

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
CA_FILE="${TMP_DIR}/hypercdr-registry-ca.crt"

echo "Downloading HyperCDR registry CA from ${REGISTRY_CA_URL}"
download_ca "$CA_FILE"

if command -v openssl >/dev/null 2>&1; then
  openssl x509 -in "$CA_FILE" -noout -subject -dates
else
  echo "warning: openssl is not installed; skipping certificate details"
fi

CONTAINERD_CA_DIR="/etc/containerd/certs.d/${REGISTRY_HOST}"
DOCKER_CA_DIR="/etc/docker/certs.d/${REGISTRY_HOST}"

echo "Installing CA into ${CONTAINERD_CA_DIR} and ${DOCKER_CA_DIR}"
run_root mkdir -p "$CONTAINERD_CA_DIR" "$DOCKER_CA_DIR"
run_root install -m 0644 "$CA_FILE" "${CONTAINERD_CA_DIR}/ca.crt"
run_root install -m 0644 "$CA_FILE" "${DOCKER_CA_DIR}/ca.crt"

HOSTS_FILE="${TMP_DIR}/hosts.toml"
cat >"$HOSTS_FILE" <<EOF
server = "https://${REGISTRY_HOST}"

[host."https://${REGISTRY_HOST}"]
  capabilities = ["pull", "resolve"]
  ca = "ca.crt"
EOF
run_root install -m 0644 "$HOSTS_FILE" "${CONTAINERD_CA_DIR}/hosts.toml"
ensure_containerd_config_path

if command -v systemctl >/dev/null 2>&1; then
  echo "Restarting container runtimes when present"
  run_root systemctl restart containerd || true
  run_root systemctl restart docker || true
else
  echo "systemctl is unavailable; restart containerd/docker manually if image pulls still fail"
fi

echo "HyperCDR registry CA is installed on this node for ${REGISTRY_HOST}"
`

const installScriptTemplate = `#!/usr/bin/env bash
set -euo pipefail

TOKEN=""
ENDPOINT="{{AGENT_WS_ENDPOINT}}"
TOKEN_VALIDATE_URL="{{TOKEN_VALIDATE_URL}}"
NAMESPACE="{{AGENT_NAMESPACE}}"
AGENT_IMAGE="{{AGENT_IMAGE}}"
VELERO_IMAGE="{{VELERO_IMAGE}}"
VELERO_AWS_PLUGIN_IMAGE="{{VELERO_AWS_PLUGIN_IMAGE}}"
VELERO_AZURE_PLUGIN_IMAGE="{{VELERO_AZURE_PLUGIN_IMAGE}}"
VELERO_GCP_PLUGIN_IMAGE="{{VELERO_GCP_PLUGIN_IMAGE}}"
EXECUTOR_MODE="kubernetes"
VELERO_CRDS_URL="{{VELERO_CRDS_URL}}"
REGISTRY_CA_URL="{{REGISTRY_CA_URL}}"
INSTALL_VELERO="true"
ALLOW_EXISTING_VELERO="false"
REGISTRY_SERVER=""
REGISTRY_USERNAME=""
REGISTRY_PASSWORD=""
REGISTRY_EMAIL="hypercdr@example.local"
IMAGE_PULL_SECRET="hypercdr-registry"
IMAGE_PULL_SECRETS_BLOCK=""
RESET_AGENT_CREDENTIAL="true"
SKIP_IMAGE_PREFLIGHT="false"
IMAGE_PULL_PREFLIGHT="true"
WAIT_READY="true"
WAIT_TIMEOUT="180s"
INSTALL_REGISTRY_CA="true"
NODE_SSH_USER=""
NODE_SSH_KEY=""
NODE_SSH_PORT="22"
INTERACTIVE="true"
STORAGE_CLASS=""

log_time() {
  date '+%H:%M:%S'
}

log_section() {
  echo
  echo "==> $1"
}

log_info() {
  echo "[INFO  $(log_time)] $1"
}

log_ok() {
  echo "[OK    $(log_time)] $1"
}

log_warn() {
  echo "[WARN  $(log_time)] $1" >&2
}

log_error() {
  echo "[ERROR $(log_time)] $1" >&2
}

fail() {
  local message="$1"
  local code="${2:-1}"
  log_error "$message"
  exit "$code"
}

print_install_summary() {
  log_section "HyperCDR agent installer"
  log_info "Target namespace: ${NAMESPACE}"
  log_info "Platform endpoint: ${ENDPOINT}"
  log_info "Executor mode: ${EXECUTOR_MODE}"
  log_info "Agent image: ${AGENT_IMAGE}"
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    log_info "Velero image: ${VELERO_IMAGE}"
  else
    log_info "Velero install: skipped"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --token)
      TOKEN="${2:-}"
      shift 2
      ;;
    --endpoint)
      ENDPOINT="${2:-}"
      shift 2
      ;;
    --namespace)
      NAMESPACE="${2:-}"
      shift 2
      ;;
    --agent-image)
      AGENT_IMAGE="${2:-}"
      shift 2
      ;;
    --velero-image)
      VELERO_IMAGE="${2:-}"
      shift 2
      ;;
    --velero-aws-plugin-image)
      VELERO_AWS_PLUGIN_IMAGE="${2:-}"
      shift 2
      ;;
    --velero-azure-plugin-image)
      VELERO_AZURE_PLUGIN_IMAGE="${2:-}"
      shift 2
      ;;
    --velero-gcp-plugin-image)
      VELERO_GCP_PLUGIN_IMAGE="${2:-}"
      shift 2
      ;;
    --executor-mode)
      EXECUTOR_MODE="${2:-}"
      shift 2
      ;;
    --install-velero)
      INSTALL_VELERO="${2:-}"
      shift 2
      ;;
    --allow-existing-velero)
      ALLOW_EXISTING_VELERO="${2:-}"
      shift 2
      ;;
    --velero-crds-url)
      VELERO_CRDS_URL="${2:-}"
      shift 2
      ;;
    --registry-ca-url)
      REGISTRY_CA_URL="${2:-}"
      shift 2
      ;;
    --registry-server)
      REGISTRY_SERVER="${2:-}"
      shift 2
      ;;
    --registry-username)
      REGISTRY_USERNAME="${2:-}"
      shift 2
      ;;
    --registry-password)
      REGISTRY_PASSWORD="${2:-}"
      shift 2
      ;;
    --registry-email)
      REGISTRY_EMAIL="${2:-}"
      shift 2
      ;;
    --image-pull-secret)
      IMAGE_PULL_SECRET="${2:-}"
      shift 2
      ;;
    --reset-agent-credential)
      RESET_AGENT_CREDENTIAL="${2:-}"
      shift 2
      ;;
    --skip-image-preflight)
      SKIP_IMAGE_PREFLIGHT="${2:-}"
      shift 2
      ;;
    --image-pull-preflight)
      IMAGE_PULL_PREFLIGHT="${2:-}"
      shift 2
      ;;
    --wait)
      WAIT_READY="${2:-}"
      shift 2
      ;;
    --wait-timeout)
      WAIT_TIMEOUT="${2:-}"
      shift 2
      ;;
    --install-registry-ca)
      INSTALL_REGISTRY_CA="${2:-}"
      shift 2
      ;;
    --node-ssh-user)
      NODE_SSH_USER="${2:-}"
      shift 2
      ;;
    --node-ssh-key)
      NODE_SSH_KEY="${2:-}"
      shift 2
      ;;
    --node-ssh-port)
      NODE_SSH_PORT="${2:-}"
      shift 2
      ;;
    --interactive)
      INTERACTIVE="${2:-}"
      shift 2
      ;;
    --storage-class)
      STORAGE_CLASS="${2:-}"
      shift 2
      ;;
    *)
      fail "Unknown argument: $1" 2
      ;;
  esac
done

if [[ -z "$TOKEN" ]]; then
  fail "Missing required argument: --token" 2
fi
if [[ -z "$ENDPOINT" ]]; then
  fail "Missing required argument: --endpoint" 2
fi
if ! command -v curl >/dev/null 2>&1; then
  fail "curl is required but was not found in PATH"
fi
if ! command -v kubectl >/dev/null 2>&1; then
  fail "kubectl is required but was not found in PATH"
fi
print_install_summary

log_section "Preflight checks"
log_info "Validating install token before changing the cluster"
token_check_file="$(mktemp)"
token_check_status="$(curl -k -sS -o "$token_check_file" -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  --data "{\"token\":\"${TOKEN}\"}" \
  "$TOKEN_VALIDATE_URL" || true)"
if [[ "$token_check_status" != "200" ]]; then
  token_check_error="$(grep -o '"error"[[:space:]]*:[[:space:]]*"[^"]*"' "$token_check_file" | head -n1 | cut -d'"' -f4 || true)"
  rm -f "$token_check_file"
  case "$token_check_error" in
    TOKEN_EXPIRED) fail "Install token has expired. Generate a new registration command in the platform." ;;
    TOKEN_USED) fail "Install token has already been used. Generate a new registration command in the platform." ;;
    TOKEN_INVALID) fail "Install token is invalid. Generate a new registration command in the platform." ;;
    *) fail "Could not validate the install token with the platform (HTTP ${token_check_status:-000}). Check platform connectivity and try again." ;;
  esac
fi
rm -f "$token_check_file"
log_ok "Install token is valid"
log_info "Checking kubectl access to the current Kubernetes cluster"
if ! kubectl version --client >/dev/null 2>&1; then
  fail "kubectl is installed, but kubectl version --client failed"
fi
if ! kubectl cluster-info >/dev/null 2>&1; then
  log_error "Cannot connect to the Kubernetes API server with the current kubeconfig."
  log_error "Check KUBECONFIG, cluster network connectivity, and Kubernetes API server certificate trust."
  exit 1
fi
log_ok "Kubernetes API is reachable"

select_agent_storage_class() {
  log_section "Storage preflight"
  local existing_storage_class default_storage_class selection attempts index
  local -a storage_class_names

  if kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state >/dev/null 2>&1; then
    existing_storage_class="$(kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state -o jsonpath='{.spec.storageClassName}' 2>/dev/null || true)"
    if [[ -z "$existing_storage_class" ]]; then
      if [[ -n "$STORAGE_CLASS" ]]; then
        fail "Existing PVC hypercdr-agent-state has no StorageClass and cannot be changed to '${STORAGE_CLASS}'. Remove the old installation safely before selecting another StorageClass."
      fi
      log_warn "Existing agent PVC has no StorageClass; keeping its current static volume binding."
      return 0
    fi
    if [[ -n "$STORAGE_CLASS" && "$STORAGE_CLASS" != "$existing_storage_class" ]]; then
      fail "Existing PVC hypercdr-agent-state uses StorageClass '${existing_storage_class}', which cannot be changed to '${STORAGE_CLASS}'. Remove the old installation safely or rerun with --storage-class ${existing_storage_class}."
    fi
    STORAGE_CLASS="$existing_storage_class"
    log_ok "Existing agent PVC uses StorageClass: ${STORAGE_CLASS}"
    return 0
  fi

  mapfile -t storage_class_names < <(kubectl get storageclass -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sed '/^[[:space:]]*$/d' | sort -u)
  if [[ ${#storage_class_names[@]} -eq 0 ]]; then
    log_error "Installation stopped: no StorageClass is available in this cluster."
    log_error "HyperCDR Agent requires a 1 GiB persistent volume for its state."
    log_error "Recommended actions:"
    log_error "1. Install a CSI storage provider."
    log_error "2. Create a StorageClass."
    log_error "3. Set it as the default, or rerun with --storage-class <name>."
    log_error "4. Verify with: kubectl get storageclass"
    exit 1
  fi

  if [[ -n "$STORAGE_CLASS" ]]; then
    if ! kubectl get storageclass "$STORAGE_CLASS" >/dev/null 2>&1; then
      log_error "StorageClass '${STORAGE_CLASS}' does not exist."
      log_error "Available StorageClasses: ${storage_class_names[*]}"
      exit 1
    fi
    log_ok "Selected StorageClass: ${STORAGE_CLASS}"
    return 0
  fi

  default_storage_class="$(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' | head -n1)"
  if [[ -z "$default_storage_class" ]]; then
    default_storage_class="$(kubectl get storageclass -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.beta\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' | head -n1)"
  fi
  if [[ -n "$default_storage_class" ]]; then
    STORAGE_CLASS="$default_storage_class"
    log_ok "Default StorageClass detected: ${STORAGE_CLASS}"
    return 0
  fi

  log_warn "No default StorageClass was found."
  if [[ "$INTERACTIVE" != "true" ]] || ! { exec 3<>/dev/tty; } 2>/dev/null; then
    log_error "A StorageClass must be selected in this non-interactive environment."
    log_error "Available StorageClasses: ${storage_class_names[*]}"
    log_error "Rerun this command with: --storage-class <name>"
    exit 1
  fi
  echo >&3
  echo "Select a StorageClass for the HyperCDR Agent state volume:" >&3
  for index in "${!storage_class_names[@]}"; do
    local name provisioner binding_mode
    name="${storage_class_names[$index]}"
    provisioner="$(kubectl get storageclass "$name" -o jsonpath='{.provisioner}')"
    binding_mode="$(kubectl get storageclass "$name" -o jsonpath='{.volumeBindingMode}')"
    printf '%d) %s\n   Provisioner: %s\n   Binding mode: %s\n' "$((index + 1))" "$name" "${provisioner:-unknown}" "${binding_mode:-Immediate}" >&3
  done
  attempts=0
  while [[ $attempts -lt 3 ]]; do
    printf 'Enter selection [1-%d]: ' "${#storage_class_names[@]}" >&3
    if ! IFS= read -r selection <&3; then
      break
    fi
    if [[ "$selection" =~ ^[0-9]+$ ]] && (( selection >= 1 && selection <= ${#storage_class_names[@]} )); then
      STORAGE_CLASS="${storage_class_names[$((selection - 1))]}"
      exec 3>&-
      log_ok "Selected StorageClass: ${STORAGE_CLASS}"
      return 0
    fi
    attempts=$((attempts + 1))
    log_warn "Invalid selection. Enter a number between 1 and ${#storage_class_names[@]}."
  done
  exec 3>&-
  fail "No valid StorageClass was selected. Rerun with --storage-class <name>."
}

select_agent_storage_class

image_registry_host() {
  local image="$1"
  local first="${image%%/*}"
  if [[ "$first" == *.* || "$first" == *:* || "$first" == "localhost" ]]; then
    echo "${first%%:*}"
  fi
}

is_ipv4_address() {
  local host="$1"
  [[ "$host" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
}

check_registry_host() {
  local image="$1"
  local host
  host="$(image_registry_host "$image")"
  if [[ -z "$host" ]]; then
    return 0
  fi
  if is_ipv4_address "$host"; then
    return 0
  fi
  if command -v getent >/dev/null 2>&1; then
    if getent hosts "$host" >/dev/null 2>&1; then
      return 0
    fi
  elif command -v nslookup >/dev/null 2>&1; then
    if nslookup "$host" >/dev/null 2>&1; then
      return 0
    fi
  else
    log_warn "Cannot verify registry host ${host}; getent/nslookup is unavailable"
    return 0
  fi
  log_error "Image registry host '${host}' from image '${image}' is not resolvable on this machine."
  log_error "Provide reachable images with --agent-image and --velero-image, or start the platform with HCDR_IMAGE_REGISTRY set to a resolvable internal registry."
  log_error "Use --skip-image-preflight true only when cluster nodes already have these images cached or resolve the registry through node-local configuration."
  exit 1
}

download_registry_ca() {
  if [[ -z "$REGISTRY_CA_URL" ]]; then
    return 1
  fi
  local ca_file="$1"
  download_url "$REGISTRY_CA_URL" "$ca_file"
}

download_url() {
  local url="$1"
  local output="$2"
  if command -v curl >/dev/null 2>&1; then
    if [[ "$url" == https://* ]]; then
      curl -k -fsSL "$url" -o "$output"
    else
      curl -fsSL "$url" -o "$output"
    fi
    return $?
  fi
  if command -v wget >/dev/null 2>&1; then
    if [[ "$url" == https://* ]]; then
      wget --no-check-certificate -qO "$output" "$url"
    else
      wget -qO "$output" "$url"
    fi
    return $?
  fi
  echo "curl or wget is required to download ${url}" >&2
  return 1
}

root_exec() {
  if [[ "$(id -u)" == "0" ]]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    return 1
  fi
}

ensure_containerd_registry_config_path() {
  local config_file="/etc/containerd/config.toml"
  local tmp_file next_file
  tmp_file="$(mktemp)"
  next_file="$(mktemp)"
  root_exec mkdir -p /etc/containerd || {
    rm -f "$tmp_file" "$next_file"
    fail "Root or sudo is required to configure containerd registry trust"
  }
  if ! root_exec test -f "$config_file"; then
    if command -v containerd >/dev/null 2>&1; then
      containerd config default >"$tmp_file"
    else
      echo "version = 2" >"$tmp_file"
    fi
    root_exec install -m 0644 "$tmp_file" "$config_file"
  fi

  if root_exec grep -Eq '^[[:space:]]*config_path[[:space:]]*=' "$config_file"; then
    root_exec sed -i -E 's#^([[:space:]]*config_path[[:space:]]*=).*#\1 "/etc/containerd/certs.d"#' "$config_file"
    rm -f "$tmp_file" "$next_file"
    return 0
  fi

  root_exec cat "$config_file" >"$tmp_file"
  if grep -q '^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]' "$tmp_file"; then
    awk '
      /^\[plugins\."io.containerd.grpc.v1.cri"\.registry\]/ && inserted == 0 {
        print
        print "  config_path = \"/etc/containerd/certs.d\""
        inserted = 1
        next
      }
      { print }
    ' "$tmp_file" >"$next_file"
  else
    cp "$tmp_file" "$next_file"
    cat >>"$next_file" <<EOF

[plugins."io.containerd.grpc.v1.cri".registry]
  config_path = "/etc/containerd/certs.d"
EOF
  fi
  root_exec install -m 0644 "$next_file" "$config_file"
  rm -f "$tmp_file" "$next_file"
}

install_registry_ca_local() {
  local registry_host="$1"
  if [[ -z "$registry_host" || -z "$REGISTRY_CA_URL" || "$INSTALL_REGISTRY_CA" != "true" ]]; then
    return 0
  fi
  local ca_file
  ca_file="$(mktemp)"
  download_registry_ca "$ca_file" || {
    rm -f "$ca_file"
    fail "Failed to download registry CA from ${REGISTRY_CA_URL}"
  }

  log_info "Installing registry CA for ${registry_host} on the current node"
  if command -v sudo >/dev/null 2>&1 && [[ "$(id -u)" != "0" ]]; then
    sudo mkdir -p "/etc/containerd/certs.d/${registry_host}" "/etc/docker/certs.d/${registry_host}"
    sudo cp "$ca_file" "/etc/containerd/certs.d/${registry_host}/ca.crt"
    sudo cp "$ca_file" "/etc/docker/certs.d/${registry_host}/ca.crt"
    ensure_containerd_registry_config_path
    sudo systemctl restart containerd >/dev/null 2>&1 || true
    sudo systemctl restart docker >/dev/null 2>&1 || true
  elif [[ "$(id -u)" == "0" ]]; then
    mkdir -p "/etc/containerd/certs.d/${registry_host}" "/etc/docker/certs.d/${registry_host}"
    cp "$ca_file" "/etc/containerd/certs.d/${registry_host}/ca.crt"
    cp "$ca_file" "/etc/docker/certs.d/${registry_host}/ca.crt"
    ensure_containerd_registry_config_path
    systemctl restart containerd >/dev/null 2>&1 || true
    systemctl restart docker >/dev/null 2>&1 || true
  else
    log_error "Root or sudo is required to install registry CA on the current node"
    rm -f "$ca_file"
    exit 1
  fi
  log_ok "Registry CA installed on the current node"
  rm -f "$ca_file"
}

install_registry_ca_remote_nodes() {
  local registry_host="$1"
  if [[ -z "$registry_host" || -z "$NODE_SSH_USER" || -z "$NODE_SSH_KEY" || -z "$REGISTRY_CA_URL" ]]; then
    return 0
  fi
  if ! command -v ssh >/dev/null 2>&1 || ! command -v scp >/dev/null 2>&1; then
    fail "ssh and scp are required for --node-ssh-user/--node-ssh-key registry CA distribution"
  fi
  local ca_file
  ca_file="$(mktemp)"
  download_registry_ca "$ca_file" || {
    rm -f "$ca_file"
    fail "Failed to download registry CA from ${REGISTRY_CA_URL}"
  }
  local node_ips
  node_ips="$(kubectl get nodes -o jsonpath='{range .items[*]}{range .status.addresses[?(@.type=="InternalIP")]}{.address}{"\n"}{end}{end}' | sort -u)"
  for node_ip in $node_ips; do
    log_info "Installing registry CA on node ${node_ip} through ssh"
    scp -P "$NODE_SSH_PORT" -i "$NODE_SSH_KEY" -o StrictHostKeyChecking=accept-new "$ca_file" "${NODE_SSH_USER}@${node_ip}:/tmp/hypercdr-registry-ca.crt"
    ssh -p "$NODE_SSH_PORT" -i "$NODE_SSH_KEY" -o StrictHostKeyChecking=accept-new "${NODE_SSH_USER}@${node_ip}" \
      "sudo mkdir -p /etc/containerd/certs.d/${registry_host} /etc/docker/certs.d/${registry_host} && sudo cp /tmp/hypercdr-registry-ca.crt /etc/containerd/certs.d/${registry_host}/ca.crt && sudo cp /tmp/hypercdr-registry-ca.crt /etc/docker/certs.d/${registry_host}/ca.crt && sudo systemctl restart containerd >/dev/null 2>&1 || true; sudo systemctl restart docker >/dev/null 2>&1 || true"
  done
  log_ok "Registry CA distribution completed"
  rm -f "$ca_file"
}

prompt_registry_ca_remote_nodes() {
  local registry_host="$1"
  if [[ -z "$registry_host" || "$INSTALL_REGISTRY_CA" != "true" || "$INTERACTIVE" != "true" ]]; then
    return 0
  fi
  local node_count
  node_count="$(kubectl get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "${node_count:-0}" -le 1 ]]; then
    return 0
  fi
  if [[ -n "$NODE_SSH_USER" && -n "$NODE_SSH_KEY" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    log_error "Cluster has ${node_count} nodes. Registry CA was installed only on the current node."
    log_error "Rerun with --node-ssh-user and --node-ssh-key so the installer can distribute registry CA to all nodes, or install the CA on each node manually before continuing."
    exit 1
  fi
  log_warn "Cluster has ${node_count} nodes. HyperCDR images may run on worker nodes, so the registry CA must be installed on every node."
  read -r -p "SSH user for Kubernetes nodes [root]: " NODE_SSH_USER
  NODE_SSH_USER="${NODE_SSH_USER:-root}"
  read -r -p "SSH private key path for node access: " NODE_SSH_KEY
  if [[ -z "$NODE_SSH_KEY" || ! -f "$NODE_SSH_KEY" ]]; then
    fail "Valid SSH private key is required for multi-node automatic CA distribution"
  fi
  read -r -p "SSH port [22]: " NODE_SSH_PORT
  NODE_SSH_PORT="${NODE_SSH_PORT:-22}"
}

print_diagnostics() {
  log_warn "Collecting HyperCDR install diagnostics from namespace ${NAMESPACE}"
  kubectl -n "$NAMESPACE" get pods -o wide >&2 || true
  kubectl -n "$NAMESPACE" get deploy,daemonset >&2 || true
  kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp | tail -n 30 >&2 || true
}

wait_timeout_seconds() {
  if [[ "$WAIT_TIMEOUT" =~ ^([0-9]+)s$ ]]; then
    echo "${BASH_REMATCH[1]}"
  elif [[ "$WAIT_TIMEOUT" =~ ^([0-9]+)m$ ]]; then
    echo $(( BASH_REMATCH[1] * 60 ))
  else
    echo 180
  fi
}

print_retry_guidance() {
  log_section "How to resolve and retry"
  log_error "1. Correct the reported Kubernetes, storage, registry, or network problem."
  log_error "2. Verify the control plane: kubectl get --raw='/readyz?verbose'"
  log_error "3. Verify install resources: kubectl -n ${NAMESPACE} get pods,pvc"
  log_error "4. If this cluster is already Online in HyperCDR, do not register it again."
  log_error "5. Otherwise rerun this installer with --wait-timeout 600s."
  log_error "6. If the token is expired or already used, generate a new registration command in HyperCDR before retrying."
  log_error "The installer uses idempotent Kubernetes apply operations, so a partial installation can be retried after the underlying problem is fixed."
}

diagnose_rollout_failure() {
  local workload_kind="$1"
  local workload_name="$2"
  local selector="$3"
  local diagnostic_text=""

  diagnostic_text="$(
    kubectl -n "$NAMESPACE" describe "${workload_kind}/${workload_name}" 2>&1 || true
    kubectl -n "$NAMESPACE" describe pods -l "$selector" 2>&1 || true
    kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp 2>&1 | tail -n 40 || true
  )"

  log_section "Installation failed"
  if ! kubectl --request-timeout=5s get --raw='/readyz' >/dev/null 2>&1 || echo "$diagnostic_text" | grep -Eqi 'connection refused|Unable to connect to the server|etcdserver: request timed out'; then
    log_error "Reason: the Kubernetes API server or etcd is unavailable or timing out."
    log_error "Impact: nodes cannot read PVC or VolumeAttachment objects, so workloads cannot become Ready."
  elif echo "$diagnostic_text" | grep -Eqi 'ErrImagePull|ImagePullBackOff|Failed to pull image|pull access denied|x509: certificate|unauthorized: authentication required'; then
    log_error "Reason: a required container image could not be pulled."
    log_error "Check registry reachability, credentials, CA trust, and whether the requested image tag exists."
  elif echo "$diagnostic_text" | grep -Eqi 'FailedMount|FailedAttachVolume|WaitForAttach|Unable to attach or mount volumes'; then
    log_error "Reason: a persistent volume could not be attached or mounted."
    log_error "Check the PVC, StorageClass, CSI controller, VolumeAttachment, and node storage health."
  elif echo "$diagnostic_text" | grep -Eqi 'unbound immediate PersistentVolumeClaims|ProvisioningFailed|no storage class is set|storageclass.*not found'; then
    log_error "Reason: the installer PVC could not be provisioned."
    log_error "Check that a default or selected StorageClass exists and its CSI provisioner is healthy."
  elif echo "$diagnostic_text" | grep -Eqi 'CrashLoopBackOff|Back-off restarting failed container'; then
    log_error "Reason: the workload container is repeatedly crashing."
    log_error "Inspect its logs: kubectl -n ${NAMESPACE} logs -l ${selector} --all-containers --tail=100"
  elif echo "$diagnostic_text" | grep -Eqi 'forbidden:|cannot (get|list|watch|create|update|patch|delete) resource'; then
    log_error "Reason: Kubernetes RBAC denied an operation required by the installer."
    log_error "Check the HyperCDR ServiceAccount, ClusterRole, and ClusterRoleBinding."
  else
    log_error "Reason: ${workload_kind}/${workload_name} did not become Ready within ${WAIT_TIMEOUT}."
    log_error "Review the diagnostics below for scheduling, probe, runtime, or node errors."
  fi
  print_diagnostics
  print_retry_guidance
}

wait_for_rollout() {
  local workload_kind="$1"
  local workload_name="$2"
  local selector="$3"
  local display_name="$4"
  local timeout_seconds deadline next_update status_output pod_status pvc_status
  timeout_seconds="$(wait_timeout_seconds)"
  if ! [[ "$timeout_seconds" =~ ^[0-9]+$ ]]; then
    timeout_seconds=180
  fi
  deadline=$((SECONDS + timeout_seconds))
  next_update=$SECONDS
  log_info "Waiting up to ${timeout_seconds}s for ${display_name} to become ready"
  while [[ $SECONDS -lt $deadline ]]; do
    if status_output="$(kubectl -n "$NAMESPACE" rollout status "${workload_kind}/${workload_name}" --timeout=2s 2>&1)"; then
      log_ok "${display_name} is ready"
      return 0
    fi
    if [[ $SECONDS -ge $next_update ]]; then
      if ! kubectl --request-timeout=5s get --raw='/readyz' >/dev/null 2>&1; then
        log_warn "${display_name}: Kubernetes API server is currently unavailable; waiting for recovery"
      else
        pod_status="$(kubectl -n "$NAMESPACE" get pods -l "$selector" --no-headers 2>/dev/null | awk '{print $1 "=" $2 "/" $3}' | paste -sd ', ' - || true)"
        pvc_status="$(kubectl -n "$NAMESPACE" get pvc --no-headers 2>/dev/null | awk '{print $1 "=" $2}' | paste -sd ', ' - || true)"
        log_info "${display_name}: ${pod_status:-no matching pod}; PVC: ${pvc_status:-none}"
      fi
      next_update=$((SECONDS + 15))
    fi
    sleep 3
  done
  log_error "${display_name} did not become ready within ${WAIT_TIMEOUT}"
  diagnose_rollout_failure "$workload_kind" "$workload_name" "$selector"
  return 1
}

wait_agent_registration() {
  local timeout_seconds="${WAIT_TIMEOUT%s}"
  if ! [[ "$timeout_seconds" =~ ^[0-9]+$ ]]; then
    timeout_seconds=180
  fi
  local deadline=$((SECONDS + timeout_seconds))
  log_info "Waiting for comm-agent to register with the HyperCDR platform"
  while [[ $SECONDS -lt $deadline ]]; do
    if kubectl -n "$NAMESPACE" get secret hypercdr-agent-credential >/dev/null 2>&1; then
      log_ok "comm-agent registration is confirmed"
      return 0
    fi
    local pod logs
    pod="$(kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=hypercdr-comm-agent -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    if [[ -n "$pod" ]]; then
      logs="$(kubectl -n "$NAMESPACE" logs "$pod" --tail=40 2>/dev/null || true)"
      if echo "$logs" | grep -Eq 'TOKEN_USED|TOKEN_EXPIRED|TOKEN_INVALID|TOKEN_NOT_FOUND|CREDENTIAL_AUTH_FAILED|CREDENTIAL_INVALID'; then
        log_error "comm-agent registration was rejected by the platform. See agent logs below."
        echo "$logs" >&2
        exit 1
      fi
    fi
    sleep 3
  done
  log_error "comm-agent did not register with the platform within ${timeout_seconds}s"
  log_error "Check whether the agent pod can reach ${ENDPOINT}, and whether the install token is still valid."
  print_diagnostics
  local pod
  pod="$(kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=hypercdr-comm-agent -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -n "$pod" ]]; then
    kubectl -n "$NAMESPACE" logs "$pod" --tail=80 >&2 || true
  fi
  exit 1
}

kubectl_retry() {
  local attempt=1
  local max_attempts=5
  local delay=3
  while true; do
    if "$@"; then
      return 0
    fi
    if [[ "$attempt" -ge "$max_attempts" ]]; then
      log_error "kubectl command failed after ${max_attempts} attempts: $*"
      return 1
    fi
    log_warn "kubectl command failed; retrying in ${delay}s (${attempt}/${max_attempts}): $*"
    sleep "$delay"
    attempt=$((attempt + 1))
    delay=$((delay * 2))
  done
}

kubectl_apply_retry() {
  local manifest
  manifest="$(mktemp)"
  cat >"$manifest"
	if kubectl_retry kubectl apply -f "$manifest"; then
		rm -f "$manifest"
		return 0
	else
		local status=$?
		log_error "Failed to apply Kubernetes manifest"
		rm -f "$manifest"
		return "$status"
	fi
}

preflight_image_pull() {
  local name="$1"
  local image="$2"
  local command_yaml="$3"
  log_info "Checking whether cluster nodes can pull image ${image}"
  kubectl -n "$NAMESPACE" delete pod "$name" --ignore-not-found --wait=true >/dev/null 2>&1 || true
  cat <<YAML | kubectl_apply_retry >/dev/null
apiVersion: v1
kind: Pod
metadata:
  name: ${name}
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: hypercdr-image-preflight
spec:
  restartPolicy: Never
${IMAGE_PULL_SECRETS_BLOCK}
  containers:
    - name: image-check
      image: ${image}
      imagePullPolicy: IfNotPresent
${command_yaml}
YAML
  local deadline=$((SECONDS + 90))
  while [[ $SECONDS -lt $deadline ]]; do
    local phase waiting terminated
    phase="$(kubectl -n "$NAMESPACE" get pod "$name" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    waiting="$(kubectl -n "$NAMESPACE" get pod "$name" -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)"
    terminated="$(kubectl -n "$NAMESPACE" get pod "$name" -o jsonpath='{.status.containerStatuses[0].state.terminated.reason}' 2>/dev/null || true)"
    case "$waiting" in
      ErrImagePull|ImagePullBackOff|InvalidImageName)
        log_error "Image pull preflight failed for ${image}: ${waiting}"
        log_error "Check image name, registry reachability, registry certificate trust, and image pull credentials."
        kubectl -n "$NAMESPACE" describe pod "$name" >&2 || true
        kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp | tail -n 20 >&2 || true
        kubectl -n "$NAMESPACE" delete pod "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
        exit 1
        ;;
    esac
    if [[ "$phase" == "Running" || "$phase" == "Succeeded" || "$terminated" != "" ]]; then
      kubectl -n "$NAMESPACE" delete pod "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
      log_ok "Image pull preflight passed for ${image}"
      return 0
    fi
    sleep 3
  done
  log_error "Image pull preflight timed out for ${image}"
  log_error "The cluster did not start the preflight pod within 90s. Check node scheduling, image pull, and registry connectivity."
  kubectl -n "$NAMESPACE" describe pod "$name" >&2 || true
  kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp | tail -n 20 >&2 || true
  kubectl -n "$NAMESPACE" delete pod "$name" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  exit 1
}

check_existing_velero_installation() {
  if [[ "$INSTALL_VELERO" != "true" || "$ALLOW_EXISTING_VELERO" == "true" ]]; then
    return 0
  fi
  local conflicts=()
  if kubectl get namespace velero >/dev/null 2>&1; then
    conflicts+=("namespace/velero")
  fi
  local deployments
  deployments="$(kubectl get deployments -A --no-headers 2>/dev/null | awk '$2=="velero"{print $1"/"$2}' || true)"
  if [[ -n "$deployments" ]]; then
    while IFS= read -r item; do
      if [[ -z "$item" ]]; then
        continue
      fi
      if [[ "$item" == "${NAMESPACE}/velero" && "$AGENT_DEPLOYMENT_EXISTS" == "true" ]]; then
        continue
      fi
      conflicts+=("deployment/${item}")
    done <<< "$deployments"
  fi
  local resource
  for resource in backups.velero.io restores.velero.io schedules.velero.io backupstoragelocations.velero.io backuprepositories.velero.io podvolumebackups.velero.io podvolumerestores.velero.io; do
    if kubectl get "$resource" -A --ignore-not-found --no-headers 2>/dev/null | grep -q .; then
      conflicts+=("${resource}")
    fi
  done
  if [[ ${#conflicts[@]} -gt 0 ]]; then
    echo "existing Velero installation or Velero resources were found in this cluster:" >&2
    printf '  - %s\n' "${conflicts[@]}" >&2
    echo "HyperCDR currently requires a dedicated Velero instance. Uninstall the existing Velero installation or use a clean cluster before installing the HyperCDR agent." >&2
    echo "If this is a deliberate reinstall of a HyperCDR-managed Velero instance, rerun with --allow-existing-velero true." >&2
    exit 1
  fi
}

if [[ "$SKIP_IMAGE_PREFLIGHT" != "true" ]]; then
  log_info "Checking registry host resolution"
  check_registry_host "$AGENT_IMAGE"
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    check_registry_host "$VELERO_IMAGE"
  fi
  log_ok "Registry host resolution check passed"
fi

NAMESPACE_EXISTS="false"
if kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
  NAMESPACE_EXISTS="true"
fi
AGENT_DEPLOYMENT_EXISTS="false"
if kubectl -n "$NAMESPACE" get deployment hypercdr-comm-agent >/dev/null 2>&1; then
  AGENT_DEPLOYMENT_EXISTS="true"
fi
check_existing_velero_installation

rollback_failed_registration() {
  log_warn "Rolling back changes because comm-agent registration did not complete"
  if [[ "$NAMESPACE_EXISTS" == "false" && "$AGENT_DEPLOYMENT_EXISTS" == "false" ]]; then
    kubectl delete namespace "$NAMESPACE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    kubectl delete clusterrolebinding hypercdr-agent hypercdr-velero --ignore-not-found >/dev/null 2>&1 || true
    kubectl delete clusterrole hypercdr-agent hypercdr-velero --ignore-not-found >/dev/null 2>&1 || true
    for crd in backuprepositories.velero.io backups.velero.io backupstoragelocations.velero.io datadownloads.velero.io datauploads.velero.io deletebackuprequests.velero.io downloadrequests.velero.io podvolumebackups.velero.io podvolumerestores.velero.io restores.velero.io schedules.velero.io serverstatusrequests.velero.io volumesnapshotlocations.velero.io; do
      kubectl delete crd "$crd" --ignore-not-found --wait=false >/dev/null 2>&1 || true
    done
    log_ok "Failed first-time installation was rolled back"
  else
    kubectl -n "$NAMESPACE" delete secret hypercdr-agent-credential --ignore-not-found >/dev/null 2>&1 || true
    log_ok "Existing installation was left retryable; rerun with a new registration command"
  fi
}

log_section "Registry trust"
REGISTRY_HOST="$(image_registry_host "$AGENT_IMAGE")"
AGENT_VERSION="${AGENT_IMAGE##*:}"
install_registry_ca_local "$REGISTRY_HOST"
prompt_registry_ca_remote_nodes "$REGISTRY_HOST"
install_registry_ca_remote_nodes "$REGISTRY_HOST"
if [[ -z "$REGISTRY_HOST" || "$INSTALL_REGISTRY_CA" != "true" ]]; then
  log_info "Registry CA installation skipped"
fi

log_section "Namespace and credentials"
log_info "Creating or updating namespace ${NAMESPACE}"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl_apply_retry
log_ok "Namespace ${NAMESPACE} is ready"
if [[ -n "$REGISTRY_SERVER" || -n "$REGISTRY_USERNAME" || -n "$REGISTRY_PASSWORD" ]]; then
  if [[ -z "$REGISTRY_SERVER" || -z "$REGISTRY_USERNAME" || -z "$REGISTRY_PASSWORD" ]]; then
    fail "--registry-server, --registry-username, and --registry-password must be provided together" 2
  fi
  log_info "Creating or updating image pull secret ${IMAGE_PULL_SECRET}"
  kubectl -n "$NAMESPACE" create secret docker-registry "$IMAGE_PULL_SECRET" \
    --docker-server="$REGISTRY_SERVER" \
    --docker-username="$REGISTRY_USERNAME" \
    --docker-password="$REGISTRY_PASSWORD" \
    --docker-email="$REGISTRY_EMAIL" \
    --dry-run=client -o yaml | kubectl_apply_retry
  IMAGE_PULL_SECRETS_BLOCK=$'      imagePullSecrets:\n        - name: '"$IMAGE_PULL_SECRET"
  log_ok "Image pull secret ${IMAGE_PULL_SECRET} is ready"
fi
if [[ "$IMAGE_PULL_PREFLIGHT" == "true" ]]; then
  log_section "Image pull preflight"
  preflight_image_pull "hypercdr-image-check-agent" "$AGENT_IMAGE" '      command: ["/comm-agent"]'
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    preflight_image_pull "hypercdr-image-check-velero" "$VELERO_IMAGE" '      command: ["/velero", "version", "--client-only"]'
    preflight_image_pull "hypercdr-image-check-velero-aws-plugin" "$VELERO_AWS_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-aws"]'
    preflight_image_pull "hypercdr-image-check-velero-azure-plugin" "$VELERO_AZURE_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-microsoft-azure"]'
    preflight_image_pull "hypercdr-image-check-velero-gcp-plugin" "$VELERO_GCP_PLUGIN_IMAGE" '      command: ["/plugins/velero-plugin-for-gcp"]'
  fi
fi
if [[ -n "$VELERO_CRDS_URL" ]]; then
  log_section "Velero CRDs"
  log_info "Installing Velero CRDs from ${VELERO_CRDS_URL}"
  crds_file="$(mktemp)"
  download_url "$VELERO_CRDS_URL" "$crds_file" || {
    rm -f "$crds_file"
    fail "Failed to download Velero CRDs from ${VELERO_CRDS_URL}"
  }
  kubectl_retry kubectl apply -f "$crds_file" || {
    rm -f "$crds_file"
    log_error "Failed to apply Velero CRDs"
        return 1
  }
  rm -f "$crds_file"
  log_ok "Velero CRDs are ready"
fi
if [[ "$INSTALL_VELERO" == "true" ]]; then
log_section "Velero workloads"
log_info "Creating or updating Velero server and node-agent"
cat <<YAML | kubectl_apply_retry
apiVersion: v1
kind: ServiceAccount
metadata:
  name: velero
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: hypercdr-velero
rules:
  - apiGroups: ["*"]
    resources: ["*"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: hypercdr-velero
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: hypercdr-velero
subjects:
  - kind: ServiceAccount
    name: velero
    namespace: ${NAMESPACE}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: node-agent-config
  namespace: ${NAMESPACE}
data:
  loadConcurrency: "1"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: velero
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: velero
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: velero
  template:
    metadata:
      labels:
        app.kubernetes.io/name: velero
    spec:
      serviceAccountName: velero
${IMAGE_PULL_SECRETS_BLOCK}
      initContainers:
        - name: velero-plugin-for-aws
          image: ${VELERO_AWS_PLUGIN_IMAGE}
          imagePullPolicy: IfNotPresent
          volumeMounts:
            - name: plugins
              mountPath: /target
        - name: velero-plugin-for-microsoft-azure
          image: ${VELERO_AZURE_PLUGIN_IMAGE}
          imagePullPolicy: IfNotPresent
          volumeMounts:
            - name: plugins
              mountPath: /target
        - name: velero-plugin-for-gcp
          image: ${VELERO_GCP_PLUGIN_IMAGE}
          imagePullPolicy: IfNotPresent
          volumeMounts:
            - name: plugins
              mountPath: /target
      containers:
        - name: velero
          image: ${VELERO_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - /velero
          args:
            - server
            - --default-volumes-to-fs-backup
            - --plugin-dir=/plugins
          env:
            - name: VELERO_SCRATCH_DIR
              value: /scratch
            - name: VELERO_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: LD_LIBRARY_PATH
              value: /plugins
            - name: HOME
              value: /udmrepo
            - name: XDG_CACHE_HOME
              value: /udmrepo/.cache
          ports:
            - name: metrics
              containerPort: 8085
          volumeMounts:
            - name: plugins
              mountPath: /plugins
            - name: scratch
              mountPath: /scratch
            - name: tmp
              mountPath: /tmp
            - name: udmrepo
              mountPath: /udmrepo
      volumes:
        - name: plugins
          emptyDir: {}
        - name: scratch
          emptyDir: {}
        - name: tmp
          emptyDir: {}
        - name: udmrepo
          emptyDir: {}
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-agent
  namespace: ${NAMESPACE}
  labels:
    app.kubernetes.io/name: velero-node-agent
    role: node-agent
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: velero-node-agent
  template:
    metadata:
      labels:
        app.kubernetes.io/name: velero-node-agent
        role: node-agent
    spec:
      serviceAccountName: velero
${IMAGE_PULL_SECRETS_BLOCK}
      securityContext:
        runAsUser: 0
      containers:
        - name: node-agent
          image: ${VELERO_IMAGE}
          imagePullPolicy: IfNotPresent
          command:
            - /velero
          args:
            - node-agent
            - server
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: VELERO_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          volumeMounts:
            - name: host-pods
              mountPath: /host_pods
              mountPropagation: HostToContainer
            - name: scratch
              mountPath: /scratch
      volumes:
        - name: host-pods
          hostPath:
            path: /var/lib/kubelet/pods
        - name: scratch
          emptyDir: {}
YAML
log_ok "Velero workloads submitted"
fi
log_section "HyperCDR agent"
log_info "Creating or updating agent bootstrap secret"
kubectl -n "$NAMESPACE" create secret generic hypercdr-agent-bootstrap \
  --from-literal=HCDR_INSTALL_TOKEN="$TOKEN" \
  --from-literal=HCDR_PLATFORM_ENDPOINT="$ENDPOINT" \
  --dry-run=client -o yaml | kubectl_apply_retry
log_ok "Agent bootstrap secret is ready"
if [[ "$RESET_AGENT_CREDENTIAL" == "true" ]]; then
  log_info "Resetting previous agent credential secret if it exists"
  kubectl -n "$NAMESPACE" delete secret hypercdr-agent-credential --ignore-not-found
fi

if ! kubectl -n "$NAMESPACE" get pvc hypercdr-agent-state >/dev/null 2>&1; then
  log_info "Creating comm-agent state PVC"
  cat <<YAML | kubectl_apply_retry
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: hypercdr-agent-state
  namespace: ${NAMESPACE}
spec:
  storageClassName: ${STORAGE_CLASS}
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
YAML
else
  log_info "Keeping existing comm-agent state PVC and StorageClass"
fi

log_info "Creating or updating comm-agent RBAC and deployment"
cat <<YAML | kubectl_apply_retry
apiVersion: v1
kind: ServiceAccount
metadata:
  name: hypercdr-agent
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: hypercdr-agent
rules:
  - apiGroups: [""]
    resources: ["namespaces", "nodes", "pods", "services", "configmaps", "serviceaccounts", "persistentvolumeclaims", "persistentvolumes", "resourcequotas", "limitranges"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["create", "delete"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "patch", "update"]
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["create", "patch", "update"]
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["clusterroles", "clusterrolebindings"]
    verbs: ["get", "list", "watch", "patch", "update", "delete"]
  - apiGroups: ["apiextensions.k8s.io"]
    resources: ["customresourcedefinitions"]
    verbs: ["get", "list", "watch", "delete"]
  - apiGroups: ["rbac.authorization.k8s.io"]
    resources: ["roles", "rolebindings"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "patch", "update"]
  - apiGroups: ["apps"]
    resources: ["statefulsets", "replicasets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["daemonsets"]
    verbs: ["get", "list", "watch", "patch", "update"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses", "networkpolicies"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["autoscaling"]
    resources: ["horizontalpodautoscalers"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["policy"]
    resources: ["poddisruptionbudgets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["velero.io"]
    resources: ["backuprepositories", "backups", "backupstoragelocations", "datadownloads", "datauploads", "deletebackuprequests", "downloadrequests", "podvolumebackups", "podvolumerestores", "restores", "schedules", "serverstatusrequests", "volumesnapshotlocations"]
    verbs: ["get", "list", "watch", "create", "patch", "update", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: hypercdr-agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: hypercdr-agent
subjects:
  - kind: ServiceAccount
    name: hypercdr-agent
    namespace: ${NAMESPACE}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hypercdr-comm-agent
  namespace: ${NAMESPACE}
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app.kubernetes.io/name: hypercdr-comm-agent
  template:
    metadata:
      labels:
        app.kubernetes.io/name: hypercdr-comm-agent
    spec:
      serviceAccountName: hypercdr-agent
      securityContext:
        fsGroup: 65532
${IMAGE_PULL_SECRETS_BLOCK}
      containers:
        - name: comm-agent
          image: ${AGENT_IMAGE}
          imagePullPolicy: Always
          envFrom:
            - secretRef:
                name: hypercdr-agent-bootstrap
          env:
            - name: HCDR_AGENT_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: HCDR_POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: HCDR_EXECUTOR_MODE
              value: "${EXECUTOR_MODE}"
            - name: HCDR_AGENT_IMAGE
              value: "${AGENT_IMAGE}"
            - name: HCDR_AGENT_VERSION
              value: "${AGENT_VERSION}"
            - name: HCDR_INVENTORY_MODE
              value: "kubernetes"
            - name: HCDR_CREDENTIAL_SECRET_ENABLED
              value: "true"
            - name: HCDR_CREDENTIAL_SECRET_NAME
              value: "hypercdr-agent-credential"
            - name: HCDR_AGENT_STATE_DIR
              value: "/var/lib/hypercdr-agent"
            - name: HCDR_PLATFORM_TLS_INSECURE_SKIP_VERIFY
              value: "true"
          volumeMounts:
            - name: agent-state
              mountPath: /var/lib/hypercdr-agent
      volumes:
        - name: agent-state
          persistentVolumeClaim:
            claimName: hypercdr-agent-state
YAML

if [[ "$AGENT_DEPLOYMENT_EXISTS" == "true" && "$RESET_AGENT_CREDENTIAL" == "true" ]]; then
  log_info "Restarting existing comm-agent deployment to use the new bootstrap token"
  kubectl_retry kubectl -n "$NAMESPACE" rollout restart deployment/hypercdr-comm-agent
  log_ok "Existing comm-agent deployment restarted with the new bootstrap token"
fi
log_ok "comm-agent deployment submitted in namespace ${NAMESPACE}"
if [[ "$WAIT_READY" == "true" ]]; then
  log_section "Readiness"
  log_info "Waiting for HyperCDR workloads to become ready in namespace ${NAMESPACE}"
  if [[ "$INSTALL_VELERO" == "true" ]]; then
    wait_for_rollout deployment velero app.kubernetes.io/name=velero "Velero deployment" || exit 1
    aws_plugin_exit_code="$(kubectl -n "$NAMESPACE" get pod -l app.kubernetes.io/name=velero -o jsonpath='{.items[0].status.initContainerStatuses[?(@.name=="velero-plugin-for-aws")].state.terminated.exitCode}' 2>/dev/null || true)"
    if [[ "$aws_plugin_exit_code" != "0" ]]; then
      log_error "Velero AWS ObjectStore plugin was not installed successfully"
      kubectl -n "$NAMESPACE" describe pod -l app.kubernetes.io/name=velero >&2 || true
  return 1
    fi
    log_ok "Velero AWS ObjectStore plugin is installed"
    wait_for_rollout daemonset node-agent app.kubernetes.io/name=velero-node-agent "Velero node-agent daemonset" || exit 1
  fi
  wait_for_rollout deployment hypercdr-comm-agent app.kubernetes.io/name=hypercdr-comm-agent "comm-agent deployment" || exit 1
  if ! wait_agent_registration; then
    rollback_failed_registration
    exit 1
  fi
  log_section "Completed"
  log_ok "HyperCDR agent installation is ready"
fi
`
