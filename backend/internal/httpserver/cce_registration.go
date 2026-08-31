package httpserver

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hypercdr-platform/platform/backend/internal/store"

	"gopkg.in/yaml.v3"
)

const (
	maxCCEKubeconfigBytes = 1 << 20
	cceUploadTTL          = 15 * time.Minute
)

type cceKubeconfigUpload struct {
	ID        string
	TenantID  string
	Path      string
	ExpiresAt time.Time
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
			ClientCertificate string         `yaml:"client-certificate"`
			ClientKey         string         `yaml:"client-key"`
			Exec              map[string]any `yaml:"exec"`
			AuthProvider      map[string]any `yaml:"auth-provider"`
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
	file, header, err := req.FormFile("kubeconfig")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "kubeconfig_required", "message": "Select a kubeconfig file to continue."})
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(filepath.Base(header.Filename)))
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "kubeconfig_file_type_invalid", "message": "Only .yaml, .yml, and .json kubeconfig files are accepted."})
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
	dir, err := os.MkdirTemp("", "hypercdr-cce-registration-")
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
	upload := cceKubeconfigUpload{ID: id, TenantID: registrationTenantID(req), Path: path, ExpiresAt: expiresAt}
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
