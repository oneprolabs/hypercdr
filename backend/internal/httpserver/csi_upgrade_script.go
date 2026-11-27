package httpserver

import (
	"hypercdr-platform/platform/backend/internal/registration"
	"net/http"
)

func (r *Router) clusterCSIUpgradeScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(registration.CSIUpgradeScript)
}
