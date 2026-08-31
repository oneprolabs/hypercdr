package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sessionIDPattern = regexp.MustCompile(`^ccer_[A-Za-z0-9_-]{20,64}$`)

type inspectRequest struct {
	SessionID string `json:"sessionId"`
	Context   string `json:"context"`
}

type inspection struct {
	Context             string   `json:"context"`
	ClusterName         string   `json:"clusterName"`
	ClusterID           string   `json:"clusterId"`
	Region              string   `json:"region,omitempty"`
	ServerVersion       string   `json:"serverVersion"`
	NodeCount           int      `json:"nodeCount"`
	StorageClasses      []string `json:"storageClasses"`
	DefaultStorageClass string   `json:"defaultStorageClass,omitempty"`
}

type kubectlRunner interface {
	Run(context.Context, string, string, ...string) ([]byte, error)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, kubeconfig, contextName string, args ...string) ([]byte, error) {
	base := []string{"--kubeconfig", kubeconfig, "--context", contextName, "--request-timeout=15s"}
	cmd := exec.CommandContext(ctx, "kubectl", append(base, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return output, nil
}

type server struct {
	baseDir string
	token   string
	runner  kubectlRunner
	logger  *slog.Logger
}

func main() {
	baseDir := env("HCDR_REGISTRATION_SESSION_DIR", "/var/lib/hypercdr/registration-sessions")
	token := strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_EXECUTOR_TOKEN"))
	if token == "" {
		slog.Error("HCDR_REGISTRATION_EXECUTOR_TOKEN is required")
		os.Exit(1)
	}
	s := &server{baseDir: filepath.Clean(baseDir), token: token, runner: commandRunner{}, logger: slog.Default()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /v1/inspect", s.inspect)
	if err := http.ListenAndServe(env("HCDR_REGISTRATION_EXECUTOR_ADDR", ":18082"), mux); err != nil {
		slog.Error("registration executor stopped", "error", err)
		os.Exit(1)
	}
}

func (s *server) inspect(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Authorization") != "Bearer "+s.token {
		writeError(w, http.StatusUnauthorized, "executor_unauthorized", "Executor authentication failed.")
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, 16<<10)
	var body inspectRequest
	if json.NewDecoder(req.Body).Decode(&body) != nil || !sessionIDPattern.MatchString(body.SessionID) || strings.TrimSpace(body.Context) == "" || len(body.Context) > 253 {
		writeError(w, http.StatusBadRequest, "inspection_request_invalid", "A valid registration session and Kubernetes context are required.")
		return
	}
	kubeconfig := filepath.Join(s.baseDir, body.SessionID, "kubeconfig")
	if info, err := os.Stat(kubeconfig); err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "registration_session_not_found", "The registration session expired or was removed.")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 45*time.Second)
	defer cancel()
	result, err := inspectCluster(ctx, s.runner, kubeconfig, body.Context)
	if err != nil {
		s.logger.Warn("CCE inspection failed", "session", body.SessionID, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "cce_inspection_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func inspectCluster(ctx context.Context, runner kubectlRunner, kubeconfig, contextName string) (inspection, error) {
	run := func(args ...string) (string, error) {
		out, err := runner.Run(ctx, kubeconfig, contextName, args...)
		return strings.TrimSpace(string(out)), err
	}
	version, err := run("version", "-o", "json")
	if err != nil {
		return inspection{}, fmt.Errorf("Kubernetes API connection failed: %w", err)
	}
	var versions struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if json.Unmarshal([]byte(version), &versions) != nil || versions.ServerVersion.GitVersion == "" {
		return inspection{}, errors.New("Kubernetes server version could not be determined.")
	}
	alias, _ := run("-n", "kube-system", "get", "configmap", "cluster-config", "-o", "jsonpath={.data.alias}")
	clusterID, err := run("get", "namespace", "kube-system", "-o", "jsonpath={.metadata.uid}")
	if err != nil || clusterID == "" {
		return inspection{}, errors.New("CCE cluster identity could not be read.")
	}
	providerIDs, err := run("get", "nodes", "-o", "jsonpath={range .items[*]}{.spec.providerID}{\"\\n\"}{end}")
	if err != nil || (!strings.Contains(strings.ToLower(providerIDs), "huaweicloud") && alias == "") {
		return inspection{}, errors.New("The selected context could not be verified as Huawei Cloud CCE.")
	}
	region, _ := run("get", "nodes", "-o", "jsonpath={.items[0].metadata.labels.topology\\.kubernetes\\.io/region}")
	nodes, err := run("get", "nodes", "-o", "name")
	if err != nil {
		return inspection{}, fmt.Errorf("Worker nodes could not be listed: %w", err)
	}
	classes, err := run("get", "storageclass", "-o", `jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`)
	if err != nil {
		return inspection{}, fmt.Errorf("StorageClasses could not be listed: %w", err)
	}
	result := inspection{Context: contextName, ClusterName: alias, ClusterID: clusterID, Region: region, ServerVersion: versions.ServerVersion.GitVersion}
	if result.ClusterName == "" {
		result.ClusterName = "cce-" + clusterID[:min(8, len(clusterID))]
	}
	for _, line := range strings.Split(nodes, "\n") {
		if strings.TrimSpace(line) != "" {
			result.NodeCount++
		}
	}
	for _, line := range strings.Split(classes, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if parts[0] == "" {
			continue
		}
		result.StorageClasses = append(result.StorageClasses, parts[0])
		if len(parts) == 2 && parts[1] == "true" {
			result.DefaultStorageClass = parts[0]
		}
	}
	return result, nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}
func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
