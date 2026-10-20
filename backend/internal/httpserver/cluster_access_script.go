package httpserver

import "net/http"

// This public asset contains no tenant data or registration credentials.
func (r *Router) clusterAccessScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("#!/usr/bin/env bash\nset -euo pipefail\nfail() { printf 'ERROR: %s\\n' \"$*\" >&2; exit 1; }\n"))
	for _, name := range []string{"installers/kubeconfig-selection.sh", "installers/check-cluster.sh"} {
		data, _ := installerModuleFS.ReadFile(name)
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n"))
	}
}
