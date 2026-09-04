package httpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/store"

	"gopkg.in/yaml.v3"
)

const (
	maxCCEKubeconfigBytes = 1 << 20
	cceUploadTTL          = 15 * time.Minute
	cceJanitorInterval    = time.Minute
)

var cceRegistrationSessionIDPattern = regexp.MustCompile(`^ccer_[A-Za-z0-9_-]{20,64}$`)

type cceKubeconfigUpload struct {
	ID          string
	TenantID    string
	Path        string
	ExpiresAt   time.Time
	Contexts    []string
	Inspected   map[string]bool
	ClusterType string
}

type kubeconfigDocument struct {
	APIVersion     string `yaml:"apiVersion"`
	Kind           string `yaml:"kind"`
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server               string `yaml:"server"`
			CertificateAuthority string `yaml:"certificate-authority"`
			ProxyURL             string `yaml:"proxy-url"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			ClientCertificate     string         `yaml:"client-certificate"`
			ClientKey             string         `yaml:"client-key"`
			ClientCertificateData string         `yaml:"client-certificate-data"`
			ClientKeyData         string         `yaml:"client-key-data"`
			Token                 string         `yaml:"token"`
			Username              string         `yaml:"username"`
			Password              string         `yaml:"password"`
			Exec                  map[string]any `yaml:"exec"`
			AuthProvider          map[string]any `yaml:"auth-provider"`
		} `yaml:"user"`
	} `yaml:"users"`
}

type cceContextSummary struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	APIServer string `json:"apiServer"`
	IsCurrent bool   `json:"isCurrent"`
}

func (r *Router) uploadCCEKubeconfig(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, maxCCEKubeconfigBytes+64*1024)
	if err := req.ParseMultipartForm(maxCCEKubeconfigBytes + 64*1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kubeconfig_upload_invalid", "message": "Upload one YAML or JSON kubeconfig no larger than 1 MiB."})
		return
	}
	clusterType := normalizeDirectRegistrationClusterType(req.FormValue("clusterType"))
	if clusterType == "" && strings.Contains(req.URL.Path, "/cce/") {
		clusterType = "huaweicloud-cce"
	}
	if clusterType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cluster_type_invalid", "message": "Select Native Kubernetes or Huawei Cloud CCE before uploading a kubeconfig."})
		return
	}
	file, header, err := req.FormFile("kubeconfig")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kubeconfig_required", "message": "Select a kubeconfig file to continue."})
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(filepath.Base(header.Filename)))
	filename := strings.ToLower(filepath.Base(header.Filename))
	if ext != ".yaml" && ext != ".yml" && ext != ".json" && filename != "kubeconfig" && filename != "config" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "kubeconfig_file_type_invalid", "message": "Upload a .yaml, .yml, or .json kubeconfig, or a standard file named kubeconfig or config."})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxCCEKubeconfigBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxCCEKubeconfigBytes {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kubeconfig_size_invalid", "message": "The kubeconfig must contain data and be no larger than 1 MiB."})
		return
	}
	doc, contexts, err := inspectPlatformKubeconfig(raw)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "kubeconfig_unsupported", "message": err.Error()})
		return
	}
	id, err := secureRegistrationID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "kubeconfig_session_failed"})
		return
	}
	baseDir := strings.TrimSpace(r.cfg.RegistrationSessionDir)
	if baseDir == "" {
		baseDir = filepath.Join(os.TempDir(), "hypercdr-registration-sessions")
	}
	baseDir = filepath.Clean(baseDir)
	if !filepath.IsAbs(baseDir) || baseDir == "/" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "kubeconfig_session_failed"})
		return
	}
	if err = os.MkdirAll(baseDir, 0700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "kubeconfig_session_failed"})
		return
	}
	dir := filepath.Join(baseDir, id)
	err = os.Mkdir(dir, 0700)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "kubeconfig_session_failed"})
		return
	}
	path := filepath.Join(dir, "kubeconfig")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		_ = os.RemoveAll(dir)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "kubeconfig_session_failed"})
		return
	}
	expiresAt := time.Now().UTC().Add(cceUploadTTL)
	contextNames := make([]string, 0, len(contexts))
	for _, context := range contexts {
		contextNames = append(contextNames, context.Name)
	}
	upload := cceKubeconfigUpload{ID: id, TenantID: registrationTenantID(req), Path: path, ExpiresAt: expiresAt, Contexts: contextNames, Inspected: map[string]bool{}, ClusterType: clusterType}
	r.cceRegistrationMu.Lock()
	r.cleanupExpiredCCEKubeconfigsLocked(time.Now().UTC())
	r.cceRegistrationUploads[id] = upload
	r.cceRegistrationMu.Unlock()
	go r.expireCCEKubeconfig(id, expiresAt)
	fingerprint := sha256.Sum256(raw)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "fingerprint": "sha256:" + hex.EncodeToString(fingerprint[:]), "currentContext": doc.CurrentContext,
		"contexts": contexts, "expiresAt": expiresAt,
	})
}

func (r *Router) inspectCCEKubeconfig(w http.ResponseWriter, req *http.Request) {
	var body struct {
		SessionID   string `json:"sessionId"`
		Context     string `json:"context"`
		ClusterType string `json:"clusterType"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 16<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "inspection_request_invalid", "message": "A registration session and Kubernetes context are required."})
		return
	}
	r.cceRegistrationMu.Lock()
	r.cleanupExpiredCCEKubeconfigsLocked(time.Now().UTC())
	upload, ok := r.cceRegistrationUploads[body.SessionID]
	clusterType := normalizeDirectRegistrationClusterType(body.ClusterType)
	if clusterType == "" && strings.Contains(req.URL.Path, "/cce/") {
		clusterType = "huaweicloud-cce"
	}
	allowed := ok && clusterType != "" && upload.ClusterType == clusterType && upload.TenantID == registrationTenantID(req) && slices.Contains(upload.Contexts, body.Context)
	r.cceRegistrationMu.Unlock()
	if !allowed {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "kubeconfig_session_not_found", "message": "The registration session expired, was removed, or does not contain this context."})
		return
	}
	if r.cfg.DeployMode != "helm" && r.cfg.DeployMode != "kubernetes" && r.cfg.RegistrationExecutorToken == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "registration_executor_unavailable", "message": "Platform-direct registration executor is not configured. Use command-based registration or contact the platform administrator."})
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 50*time.Second)
	defer cancel()
	var result []byte
	statusCode := http.StatusOK
	if r.cfg.DeployMode == "helm" || r.cfg.DeployMode == "kubernetes" {
		if err := r.createRegistrationInspectionJob(ctx, body.SessionID, body.Context, clusterType); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "registration_inspection_start_failed", "message": "The isolated CCE inspection Job could not be started. No cluster resources were changed."})
			return
		}
		resultPath := filepath.Join(filepath.Dir(upload.Path), "inspection-result.json")
		for {
			raw, readErr := os.ReadFile(resultPath)
			if readErr == nil {
				_ = os.Remove(resultPath)
				var jobResult struct {
					Inspection json.RawMessage `json:"inspection"`
					Error      string          `json:"error"`
				}
				if json.Unmarshal(raw, &jobResult) != nil {
					writeJSON(w, http.StatusBadGateway, map[string]any{"error": "registration_executor_invalid_response"})
					return
				}
				if jobResult.Error != "" {
					statusCode = http.StatusUnprocessableEntity
					result, _ = json.Marshal(map[string]string{"error": "cce_inspection_failed", "message": jobResult.Error})
				} else {
					result = jobResult.Inspection
				}
				break
			}
			select {
			case <-ctx.Done():
				writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "cce_inspection_timeout", "message": "CCE inspection did not finish within 50 seconds. Retry or use command-based registration."})
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	} else {
		payload, _ := json.Marshal(map[string]string{"sessionId": body.SessionID, "context": body.Context, "clusterType": clusterType})
		executorReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.RegistrationExecutorEndpoint+"/v1/inspect", bytes.NewReader(payload))
		executorReq.Header.Set("Content-Type", "application/json")
		executorReq.Header.Set("Authorization", "Bearer "+r.cfg.RegistrationExecutorToken)
		response, err := http.DefaultClient.Do(executorReq)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "registration_executor_unreachable", "message": "The registration executor could not be reached. Retry or use command-based registration."})
			return
		}
		defer response.Body.Close()
		result, err = io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "registration_executor_invalid_response"})
			return
		}
		statusCode = response.StatusCode
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(result)
	if statusCode >= 200 && statusCode < 300 {
		r.cceRegistrationMu.Lock()
		if current, exists := r.cceRegistrationUploads[body.SessionID]; exists && current.TenantID == registrationTenantID(req) {
			current.Inspected[body.Context] = true
			r.cceRegistrationUploads[body.SessionID] = current
		}
		r.cceRegistrationMu.Unlock()
	}
}

type cceDirectInstallRequest struct {
	SessionID      string `json:"sessionId"`
	Context        string `json:"context"`
	StorageClass   string `json:"storageClass"`
	IdempotencyKey string `json:"idempotencyKey"`
	ClusterType    string `json:"clusterType"`
}

func (r *Router) startCCEDirectRegistration(w http.ResponseWriter, req *http.Request) {
	var body cceDirectInstallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 16<<10)).Decode(&body); err != nil || len(body.IdempotencyKey) < 16 || len(body.IdempotencyKey) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "registration_request_invalid", "message": "A validated session, context, and stable idempotency key are required."})
		return
	}
	tenantID := registrationTenantID(req)
	clusterType := normalizeDirectRegistrationClusterType(body.ClusterType)
	if clusterType == "" && strings.Contains(req.URL.Path, "/cce/") {
		clusterType = "huaweicloud-cce"
	}
	r.cceRegistrationMu.Lock()
	r.cleanupExpiredCCEKubeconfigsLocked(time.Now().UTC())
	upload, ok := r.cceRegistrationUploads[body.SessionID]
	allowed := ok && clusterType != "" && upload.ClusterType == clusterType && upload.TenantID == tenantID && slices.Contains(upload.Contexts, body.Context) && upload.Inspected[body.Context]
	if allowed {
		upload.ExpiresAt = time.Now().UTC().Add(25 * time.Minute)
		r.cceRegistrationUploads[body.SessionID] = upload
	}
	r.cceRegistrationMu.Unlock()
	if !allowed {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "inspection_required", "message": "Inspect this exact kubeconfig context successfully before registration."})
		return
	}
	go r.expireCCEKubeconfig(body.SessionID, upload.ExpiresAt)
	existing, err := r.store.ListTasksFiltered(store.TaskFilter{TenantID: tenantID, Types: []string{"cluster-registration"}, Limit: 200})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "registration_task_lookup_failed"})
		return
	}
	for _, task := range existing {
		if stringPayload(task.Payload, "idempotencyKey") == body.IdempotencyKey {
			writeJSON(w, http.StatusOK, task)
			return
		}
	}
	actor, _ := requestUser(req)
	displayType := "Native Kubernetes"
	if clusterType == "huaweicloud-cce" {
		displayType = "Huawei Cloud CCE"
	} else if clusterType == "openshift" {
		displayType = "OpenShift"
	}
	token, err := r.store.CreateAgentToken(tenantID, actor.ID, displayType+" platform-direct registration", 30*time.Minute, clusterType)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "registration_token_create_failed"})
		return
	}
	endpoint := strings.TrimSpace(r.cfg.AgentPrivateWSEndpoint)
	if endpoint == "" {
		endpoint = r.agentWSEndpoint(req)
	}
	requestData := map[string]string{
		"token": token.Token, "installScriptUrl": r.publicBaseURL(req) + "/install.sh", "endpoint": endpoint,
		"endpointPublic": strings.TrimSpace(r.cfg.AgentPublicWSEndpoint), "namespace": r.agentNamespaceForType(clusterType),
		"context": body.Context, "storageClass": strings.TrimSpace(body.StorageClass),
		"clusterType": clusterType,
	}
	raw, _ := json.Marshal(requestData)
	requestPath := filepath.Join(filepath.Dir(upload.Path), "install-request.json")
	if err = os.WriteFile(requestPath, raw, 0600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "registration_session_write_failed"})
		return
	}
	task, err := r.store.CreateTask(store.TaskInput{TenantID: tenantID, Type: "cluster-registration", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{
		"sessionId": body.SessionID, "context": body.Context, "storageClass": strings.TrimSpace(body.StorageClass), "idempotencyKey": body.IdempotencyKey, "provider": clusterType, "clusterType": clusterType, "stage": "queued",
	}})
	if err != nil {
		_ = os.Remove(requestPath)
		r.logger.Error("failed to create CCE registration task", "tenant_id", tenantID, "session_id", body.SessionID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "registration_task_create_failed", "message": "The registration task could not be created. No cluster resources were changed; retry the operation."})
		return
	}
	if err = r.createRegistrationExecutorJob(req.Context(), task.ID); err != nil {
		_ = os.RemoveAll(filepath.Dir(upload.Path))
		r.cceRegistrationMu.Lock()
		delete(r.cceRegistrationUploads, body.SessionID)
		r.cceRegistrationMu.Unlock()
		task, _, _ = r.store.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "failed", Progress: 100, ErrorCode: "REGISTRATION_EXECUTOR_START_FAILED", ErrorMessage: err.Error(), Payload: map[string]any{"stage": "failed"}, MarkDone: true})
		_ = r.store.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "error", Reason: "REGISTRATION_EXECUTOR_START_FAILED", Message: err.Error()})
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "registration_executor_start_failed", "message": "The isolated registration executor could not be started. No cluster resources were changed.", "task": task})
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}

func normalizeDirectRegistrationClusterType(value string) string {
	switch strings.TrimSpace(value) {
	case "native-kubernetes", "huaweicloud-cce", "openshift":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func inspectPlatformKubeconfig(raw []byte) (kubeconfigDocument, []cceContextSummary, error) {
	var doc kubeconfigDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return doc, nil, fmt.Errorf("The file is not a valid kubeconfig: %w", err)
	}
	if doc.Kind != "Config" || len(doc.Clusters) == 0 || len(doc.Contexts) == 0 || len(doc.Users) == 0 {
		return doc, nil, errors.New("The file does not contain the required kubeconfig clusters, contexts, and users.")
	}
	clusters := make(map[string]string, len(doc.Clusters))
	for _, item := range doc.Clusters {
		name, server := strings.TrimSpace(item.Name), strings.TrimSpace(item.Cluster.Server)
		parsed, err := url.Parse(server)
		if name == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return doc, nil, fmt.Errorf("Cluster %q must use a valid HTTPS API server.", name)
		}
		if item.Cluster.CertificateAuthority != "" || item.Cluster.ProxyURL != "" {
			return doc, nil, fmt.Errorf("Cluster %q references an external CA file or proxy; use command-based registration instead.", name)
		}
		clusters[name] = server
	}
	users := make(map[string]struct{}, len(doc.Users))
	for _, item := range doc.Users {
		name := strings.TrimSpace(item.Name)
		if name == "" || item.User.ClientCertificate != "" || item.User.ClientKey != "" {
			return doc, nil, fmt.Errorf("User %q references external credential files; use command-based registration instead.", name)
		}
		if len(item.User.Exec) > 0 || len(item.User.AuthProvider) > 0 {
			return doc, nil, fmt.Errorf("User %q uses an external authentication plugin, which platform-direct registration does not execute.", name)
		}
		if item.User.ClientCertificateData == "" && item.User.ClientKeyData == "" && item.User.Token == "" && (item.User.Username == "" || item.User.Password == "") {
			return doc, nil, fmt.Errorf("User %q does not contain embedded credentials; use command-based registration instead.", name)
		}
		users[name] = struct{}{}
	}
	contexts := make([]cceContextSummary, 0, len(doc.Contexts))
	seen := map[string]struct{}{}
	for _, item := range doc.Contexts {
		name := strings.TrimSpace(item.Name)
		server, clusterOK := clusters[item.Context.Cluster]
		_, userOK := users[item.Context.User]
		if name == "" || !clusterOK || !userOK {
			return doc, nil, fmt.Errorf("Context %q references an unknown cluster or user.", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return doc, nil, fmt.Errorf("Context %q is duplicated.", name)
		}
		seen[name] = struct{}{}
		contexts = append(contexts, cceContextSummary{Name: name, Cluster: item.Context.Cluster, User: item.Context.User, APIServer: server, IsCurrent: name == doc.CurrentContext})
	}
	sort.Slice(contexts, func(i, j int) bool { return contexts[i].Name < contexts[j].Name })
	return doc, contexts, nil
}

func registrationTenantID(req *http.Request) string {
	if actor, ok := requestUser(req); ok && actor.TenantID != "" {
		return actor.TenantID
	}
	return store.DefaultTenantID
}

func secureRegistrationID() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "ccer_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (r *Router) deleteCCEKubeconfig(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	r.cceRegistrationMu.Lock()
	upload, ok := r.cceRegistrationUploads[id]
	if ok && upload.TenantID == registrationTenantID(req) {
		delete(r.cceRegistrationUploads, id)
	} else {
		ok = false
	}
	r.cceRegistrationMu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "kubeconfig_session_not_found"})
		return
	}
	_ = os.RemoveAll(filepath.Dir(upload.Path))
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) cleanupExpiredCCEKubeconfigsLocked(now time.Time) {
	for id, upload := range r.cceRegistrationUploads {
		if !upload.ExpiresAt.After(now) {
			delete(r.cceRegistrationUploads, id)
			_ = os.RemoveAll(filepath.Dir(upload.Path))
		}
	}
}

// cleanupOrphanedCCEKubeconfigs removes expired upload directories that are no
// longer represented in memory, for example after an API restart. The
// kubeconfig modification time is the upload time and remains unchanged while
// inspection and installation artifacts are written beside it.
func (r *Router) cleanupOrphanedCCEKubeconfigs(now time.Time) {
	baseDir := strings.TrimSpace(r.cfg.RegistrationSessionDir)
	if baseDir == "" {
		return
	}
	baseDir = filepath.Clean(baseDir)
	if !filepath.IsAbs(baseDir) || baseDir == "/" {
		return
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	cutoff := now.UTC().Add(-cceUploadTTL)
	for _, entry := range entries {
		if !entry.IsDir() || !cceRegistrationSessionIDPattern.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(baseDir, entry.Name())
		info, statErr := os.Stat(filepath.Join(dir, "kubeconfig"))
		if statErr != nil {
			info, statErr = entry.Info()
		}
		if statErr == nil && !info.ModTime().After(cutoff) {
			_ = os.RemoveAll(dir)
		}
	}
}

func (r *Router) startCCEKubeconfigJanitor() {
	if strings.TrimSpace(r.cfg.RegistrationSessionDir) == "" {
		return
	}
	r.cleanupOrphanedCCEKubeconfigs(time.Now().UTC())
	go func() {
		ticker := time.NewTicker(cceJanitorInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			r.cleanupOrphanedCCEKubeconfigs(now)
		}
	}()
}

func (r *Router) expireCCEKubeconfig(id string, expiresAt time.Time) {
	timer := time.NewTimer(time.Until(expiresAt))
	defer timer.Stop()
	<-timer.C
	r.cceRegistrationMu.Lock()
	upload, ok := r.cceRegistrationUploads[id]
	if ok && !upload.ExpiresAt.After(time.Now().UTC()) {
		delete(r.cceRegistrationUploads, id)
	}
	r.cceRegistrationMu.Unlock()
	if ok {
		_ = os.RemoveAll(filepath.Dir(upload.Path))
	}
}
