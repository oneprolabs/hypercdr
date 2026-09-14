package httpserver

import (
	"encoding/json"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strconv"
	"strings"
)

func (r *Router) listApplications(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	filter := store.ApplicationFilter{ClusterID: query.Get("clusterId"), Summary: query.Get("view") == "summary"}
	if user, ok := requestUser(req); ok && !user.SystemAdmin {
		filter.TenantID = user.TenantID
	}
	if pageSize, err := strconv.Atoi(query.Get("pageSize")); err == nil && pageSize > 0 {
		if pageSize > 500 {
			pageSize = 500
		}
		filter.Limit = pageSize
		if page, pageErr := strconv.Atoi(query.Get("page")); pageErr == nil && page > 1 {
			filter.Offset = (page - 1) * pageSize
		}
	}
	apps, err := r.store.ListApplicationsFiltered(filter)
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
		"Storage type is not supported for DR. Supported storage: stateless namespaces, or PVCs backed by dynamically provisioned CSI storage available on the source and target clusters (for example cloud disk/file CSI or Longhorn). Unsupported storage: local-path, hostPath, and local PV. Detected storage: %s.",
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
