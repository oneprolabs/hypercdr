package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

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
		sink := r.auditSink
		if sink == nil {
			sink = storeAuditSink{store: r.store}
		}
		err := sink.RecordAudit(req.Context(), EditionAuditEvent{TenantID: user.TenantID, ActorID: user.ID, Actor: user.Email, Action: action, ResourceType: resourceType, ResourceID: validAuditUUID(resourceID), ResourceName: resourceName, Result: result, Message: message, HTTPStatus: status})
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
		"POST /api/v1/platform/releases": "Register HyperCDR Release", "POST /api/v1/platform/upgrades": "Start Platform Upgrade",
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
