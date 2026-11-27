package httpserver

import "net/http"

func (r *Router) clusterCSIUpgradeScript(w http.ResponseWriter, _ *http.Request) {
	script, err := installerModuleFS.ReadFile("installers/csi-upgrade-rbac.sh")
	if err != nil {
		http.Error(w, "CSI upgrade script unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(script)
}
