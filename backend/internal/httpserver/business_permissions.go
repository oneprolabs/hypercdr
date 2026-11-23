package httpserver

import (
	"net/http"
	"strings"

	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/pkg/accesscontrol"
)

func businessPermission(method, path string) string {
	if method == http.MethodGet || method == http.MethodHead {
		if resourcePath(path) || strings.HasPrefix(path, "/api/v1/diagnostic-") || path == "/api/v1/audit-logs" {
			return "resources.view"
		}
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
	switch parts[0] {
	case "agent-tokens", "cluster-registrations":
		return "cluster.register"
	case "clusters":
		if method == http.MethodDelete || strings.HasSuffix(path, "/unregister") || strings.HasSuffix(path, "/force-cleanup") {
			return "cluster.unregister"
		}
		if strings.Contains(path, "/logs/") {
			return "diagnostics.manage"
		}
		return "cluster.manage"
	case "applications", "tags", "protection-plans":
		return "sync.manage"
	case "storage-repositories":
		return "storage.manage"
	case "policies":
		return "policy.manage"
	case "restore-points":
		return "backup.delete"
	case "tasks":
		if len(parts) == 2 {
			switch parts[1] {
			case "backup":
				return "sync.manage"
			case "drill":
				return "drill.execute"
			case "restore", "takeover":
				return "recovery.execute"
			}
		}
		if len(parts) == 3 {
			switch parts[2] {
			case "cancel", "retry", "cleanup-drill":
				return "task-operation"
			case "abort":
				return "task.abort"
			}
		}
		return "unmapped-business-operation"
	}
	return ""
}

func taskOperationPermission(taskType string) string {
	switch taskType {
	case "backup", "backup-cancel", "protection-cleanup", "schedule-sync", "plan-capacity":
		return "sync.manage"
	case "drill", "drill-cleanup":
		return "drill.execute"
	case "restore", "takeover":
		return "recovery.execute"
	case "retention-cleanup":
		return "backup.delete"
	case "storage-sync":
		return "storage.manage"
	case "unregister":
		return "cluster.unregister"
	case "cache-cleanup", "cache-settings", "agent-upgrade", "velero-upgrade":
		return "cluster.manage"
	case "cluster-registration", "register":
		return "cluster.register"
	}
	return "unmapped-business-operation"
}

func hasBusinessPermission(user store.User, permission string) bool {
	customID := user.RoleID
	if customID == "admin" || customID == "operator" {
		customID = ""
	}
	return accesscontrol.Allows(user.Role, user.SystemAdmin, customID, user.Permissions, permission)
}

func (r *Router) authorizeBusinessRequest(w http.ResponseWriter, req *http.Request, user store.User) bool {
	if r.productInfo.Edition != "community" || user.SystemAdmin || user.Role == "admin" {
		return true
	}
	permission := businessPermission(req.Method, req.URL.Path)
	if permission == "" {
		return true
	}
	if req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/api/v1/protection-plans/") && !hasBusinessPermission(user, "backup.delete") {
		permission = "backup.delete"
	}
	if permission == "task-operation" || permission == "task.abort" {
		parts := strings.Split(strings.TrimPrefix(req.URL.Path, "/api/v1/"), "/")
		task, found, err := r.store.GetTask(parts[1])
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "permission_check_unavailable", "message": "Task permissions could not be checked. Retry shortly."})
			return false
		}
		if !found || task.TenantID != user.TenantID {
			writeJSON(w, 404, map[string]string{"error": "task_not_found", "message": "Task not found."})
			return false
		}
		operation := taskOperationPermission(task.Type)
		if task.Type == "protection-cleanup" && task.Payload["cleanupMode"] == "drill" {
			operation = "drill.execute"
		}
		if permission == "task-operation" || hasBusinessPermission(user, permission) {
			permission = operation
		}
	}
	if hasBusinessPermission(user, permission) {
		return true
	}
	if r.auditSink != nil {
		if err := r.auditSink.RecordAudit(req.Context(), EditionAuditEvent{TenantID: user.TenantID, ActorID: user.ID, Actor: user.Username, Action: "permission.denied", ResourceType: "permission", ResourceID: permission, Result: "denied", Message: req.Method + " " + req.URL.Path, HTTPStatus: 403}); err != nil {
			r.logger.Warn("failed to record permission denial", "error", err)
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "organization_permission_required", "permission": permission, "message": "Your role does not allow this operation. Contact your organization administrator."})
	return false
}
