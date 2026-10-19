package httpserver

import (
	"encoding/json"
	"hypercdr-platform/platform/backend/pkg/platformaccess"
	"net/http"
)

func (r *Router) accessSettings(w http.ResponseWriter, req *http.Request) {
	user, ok := requestUser(req)
	if !ok || !user.SystemAdmin {
		writeJSON(w, 403, map[string]string{"error": "system_administrator_required"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if req.Method == http.MethodPatch {
		var in platformAccessRequest
		req.Body = http.MaxBytesReader(w, req.Body, 4096)
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		if _, err := platformaccess.Normalize(in.PublicURL); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid_public_url", "message": err.Error()})
			return
		}
		if _, err := platformaccess.Save(r.store, in.PublicURL); err != nil {
			writeJSON(w, 500, map[string]string{"error": "access_settings_failed"})
			return
		}
	}
	origin := r.publicBaseURL(req)
	writeJSON(w, 200, platformAccessResponse{PublicURL: origin, GoogleCallback: origin + "/api/v1/saas/auth/google/callback", VerificationExample: origin + "/signup?token=…", InvitationExample: origin + "/join?token=…", Source: "database", RestartRequired: false})
}
