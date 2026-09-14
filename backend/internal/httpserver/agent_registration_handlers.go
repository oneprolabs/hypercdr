package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"hypercdr-platform/platform/backend/internal/store"
	"hypercdr-platform/platform/backend/internal/veleroassets"
	"net/http"
	"os"
	"strings"
	"time"
)

func (r *Router) createAgentToken(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Description string `json:"description"`
		TTLSeconds  int    `json:"ttlSeconds"`
		ClusterType string `json:"clusterType"`
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
	clusterType := storeClusterType(body.ClusterType)
	token, err := r.store.CreateAgentToken(tenantID, actor.ID, body.Description, ttl, clusterType)
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

	primaryEndpoint := strings.TrimSpace(r.cfg.AgentPrivateWSEndpoint)
	if primaryEndpoint == "" {
		primaryEndpoint = r.agentWSEndpoint(req)
	}
	installCommand := curlCommand + baseURL + "/install.sh | bash -s -- --token " +
		token.Token + " --endpoint " + primaryEndpoint
	if publicEndpoint := strings.TrimSpace(r.cfg.AgentPublicWSEndpoint); publicEndpoint != "" && publicEndpoint != primaryEndpoint {
		installCommand += " --endpoint-public " + publicEndpoint
	}
	// Always make the selected provider explicit. The installer default is kept
	// only for backward compatibility; generated commands must remain stable as
	// more cluster providers are added.
	installCommand += " --cluster-type " + clusterType
	installCommand += " --namespace " + r.agentNamespaceForType(clusterType) +
		" --executor-mode kubernetes --install-registry-ca false"
	response := map[string]any{
		"id":             token.ID,
		"token":          token.Token,
		"expiresAt":      token.ExpiresAt,
		"clusterType":    clusterType,
		"installCommand": installCommand,
	}
	if r.cfg.RegistryCAPath != "" {
		response["prepareNodeCommand"] = curlCommand + baseURL + "/prepare-node.sh | bash"
	}
	writeJSON(w, http.StatusCreated, response)
}

func storeClusterType(value string) string {
	switch strings.TrimSpace(value) {
	case "huaweicloud-cce", "openshift":
		return strings.TrimSpace(value)
	}
	return "native-kubernetes"
}

func (r *Router) agentNamespaceForType(clusterType string) string {
	if strings.TrimSpace(clusterType) == "openshift" {
		return "openshift-adp"
	}
	return r.cfg.AgentNamespace
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
	script = strings.ReplaceAll(script, "{{PLATFORM_CA_URL}}", r.publicBaseURL(req)+"/assets/platform/ca.crt")
	_, _ = w.Write([]byte(script))
}

func (r *Router) installScript(w http.ResponseWriter, req *http.Request) {
	agentTarget, agentErr := r.componentTarget(req.Context(), "comm-agent")
	oadpAgentTarget, oadpAgentErr := r.componentTarget(req.Context(), "oadp-comm-agent")
	oadpCatalogTarget, oadpCatalogErr := r.componentTarget(req.Context(), "oadp-catalog")
	oadpRuntimeNames := []string{"oadp-bundle", "oadp-operator", "oadp-velero", "oadp-openshift-plugin", "oadp-aws-plugin", "oadp-restore-helper"}
	oadpRuntimeImages := make([]string, 0, len(oadpRuntimeNames))
	var oadpRuntimeErr error
	for _, name := range oadpRuntimeNames {
		target, err := r.componentTarget(req.Context(), name)
		if err != nil || strings.TrimSpace(target.Image) == "" {
			oadpRuntimeErr = fmt.Errorf("%s target unavailable: %w", name, err)
			break
		}
		oadpRuntimeImages = append(oadpRuntimeImages, immutableImageReference(target.Image, target.ImageDigest))
	}
	veleroTarget, veleroErr := r.componentTarget(req.Context(), "velero")
	awsTarget, awsErr := r.componentTarget(req.Context(), "velero-plugin-for-aws")
	azureTarget, azureErr := r.componentTarget(req.Context(), "velero-plugin-for-microsoft-azure")
	gcpTarget, gcpErr := r.componentTarget(req.Context(), "velero-plugin-for-gcp")
	if agentErr != nil || veleroErr != nil || awsErr != nil || azureErr != nil || gcpErr != nil {
		r.logger.Error("failed to resolve active release manifest for install script", "agent_error", agentErr, "velero_error", veleroErr, "aws_plugin_error", awsErr, "azure_plugin_error", azureErr, "gcp_plugin_error", gcpErr)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "component_target_unavailable", "message": "Active cluster component versions are not available."})
		return
	}
	if oadpAgentErr != nil || strings.TrimSpace(oadpAgentTarget.Image) == "" {
		// Backward-compatible fallback for an active release created before the
		// OpenShift component was added. New releases publish it explicitly.
		oadpAgentTarget.Image = strings.Replace(agentTarget.Image, "/comm-agent:", "/oadp-comm-agent:", 1)
	}
	if oadpCatalogErr != nil || strings.TrimSpace(oadpCatalogTarget.Image) == "" {
		oadpCatalogTarget.Image = strings.Replace(agentTarget.Image, "/comm-agent:", "/oadp-catalog:", 1)
	}
	if oadpRuntimeErr != nil {
		r.logger.Warn("active release does not contain a complete OADP runtime manifest", "error", oadpRuntimeErr)
	}
	modules, moduleErr := assembledInstallerModules()
	if moduleErr != nil {
		r.logger.Error("failed to assemble installer provider modules", "error", moduleErr)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "installer_modules_unavailable"})
		return
	}
	script := strings.ReplaceAll(installScriptTemplate, "{{AGENT_IMAGE}}", agentTarget.Image)
	script = strings.ReplaceAll(script, "{{OADP_AGENT_IMAGE}}", immutableImageReference(oadpAgentTarget.Image, oadpAgentTarget.ImageDigest))
	script = strings.ReplaceAll(script, "{{OADP_CATALOG_IMAGE}}", immutableImageReference(oadpCatalogTarget.Image, oadpCatalogTarget.ImageDigest))
	script = strings.ReplaceAll(script, "{{OADP_RUNTIME_IMAGES}}", strings.Join(oadpRuntimeImages, " "))
	script = strings.ReplaceAll(script, "{{INSTALLER_PROVIDER_MODULES}}", modules)
	script = strings.ReplaceAll(script, "{{AGENT_NAMESPACE}}", r.cfg.AgentNamespace)
	script = strings.ReplaceAll(script, "{{AGENT_WS_ENDPOINT}}", r.agentWSEndpoint(req))
	script = strings.ReplaceAll(script, "{{TOKEN_VALIDATE_URL}}", r.publicBaseURL(req)+"/api/v1/agent-tokens/validate")
	script = strings.ReplaceAll(script, "{{AGENT_UNINSTALL_URL}}", r.publicBaseURL(req)+"/uninstall-agent.sh")
	script = strings.ReplaceAll(script, "{{VELERO_CRDS_URL}}", r.publicBaseURL(req)+veleroCRDsPath)
	script = strings.ReplaceAll(script, "{{REGISTRY_CA_URL}}", r.publicBaseURL(req)+"/assets/registry/ca.crt")
	script = strings.ReplaceAll(script, "{{PLATFORM_CA_URL}}", r.publicBaseURL(req)+"/assets/platform/ca.crt")
	script = strings.ReplaceAll(script, "{{VELERO_IMAGE}}", veleroTarget.Image)
	script = strings.ReplaceAll(script, "{{VELERO_AWS_PLUGIN_IMAGE}}", awsTarget.Image)
	script = strings.ReplaceAll(script, "{{VELERO_AZURE_PLUGIN_IMAGE}}", azureTarget.Image)
	script = strings.ReplaceAll(script, "{{VELERO_GCP_PLUGIN_IMAGE}}", gcpTarget.Image)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
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

func (r *Router) platformCA(w http.ResponseWriter, req *http.Request) {
	if strings.TrimSpace(r.cfg.TLSCertFile) == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "platform_ca_not_configured"})
		return
	}
	data, err := os.ReadFile(r.cfg.TLSCertFile)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "platform_ca_not_found"})
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
