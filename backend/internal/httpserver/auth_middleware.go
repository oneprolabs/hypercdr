package httpserver

import (
	"context"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strings"
)

func (r *Router) withPlatformAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path
		publicAuth := path == "/api/v1/product-info" || path == "/api/v1/auth/captcha" || path == "/api/v1/auth/login" || path == "/api/v1/auth/forgot-password" || path == "/api/v1/auth/reset-password" || path == "/api/v1/auth/config" || path == "/api/v1/disaster-handovers/validate" || strings.HasPrefix(path, "/api/v1/community-migrations/source/")
		if _, isMemory := r.store.(*store.MemoryStore); isMemory || !strings.HasPrefix(path, "/api/v1/") || publicAuth || path == "/api/v1/agent-tokens/validate" {
			next.ServeHTTP(w, req)
			return
		}
		pipelineReleaseMutation := req.Method == http.MethodPost && path == "/api/v1/platform/releases"
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
		identity, ok, err := r.identityProvider.AuthenticateSession(req.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
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
		user := storeUserFromIdentity(identity)
		if user.MustChangePassword && !passwordChangeAllowed {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "password_change_required", "message": "Change your temporary password before continuing."})
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodOptions && !strings.HasPrefix(path, "/api/v1/community-migrations") && !strings.HasPrefix(path, "/api/v1/auth/") {
			frozen, freezeErr := r.store.HasCommunityMigrationFreeze()
			if freezeErr != nil {
				r.logger.Error("migration freeze check failed", "error", freezeErr)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "migration_freeze_check_failed"})
				return
			}
			if frozen {
				writeJSON(w, http.StatusLocked, map[string]any{"error": "community_migration_frozen", "message": "Community is frozen for Enterprise migration. Business mutations remain disabled until commit or rollback."})
				return
			}
		}
		if requiresAdmin(req) && user.Role != "admin" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "administrator_required", "message": "Administrator permission is required."})
			return
		}
		if requiresSystemAdmin(req) && !user.SystemAdmin && user.Email != "release-pipeline" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "system_administrator_required", "message": "System administrator permission is required."})
			return
		}
		if r.editionAuthorizer != nil {
			decision := r.editionAuthorizer(req.Context(), EditionAuthorizationRequest{
				Method: req.Method, Path: req.URL.Path,
				Principal: EditionPrincipal{ID: user.ID, TenantID: user.TenantID, Email: user.Email, Role: user.Role, SystemAdmin: user.SystemAdmin},
			})
			if !decision.Allowed {
				code := strings.TrimSpace(decision.Code)
				if code == "" {
					code = "edition_policy_denied"
				}
				message := strings.TrimSpace(decision.Message)
				if message == "" {
					message = "This operation is not permitted by the edition policy."
				}
				writeJSON(w, http.StatusForbidden, map[string]any{"error": code, "message": message})
				return
			}
		}
		next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), requestUserContextKey{}, user)))
	})
}
