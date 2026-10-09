package httpserver

import (
	"hypercdr-platform/platform/backend/internal/store"
	"net/http"
	"strings"
)

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
	writeJSON(w, http.StatusOK, listResponse[store.Policy]{Items: nonNilSlice(items)})
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
	writeJSON(w, http.StatusOK, deletedResponse{Deleted: true})
}
