package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
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
	if db := strings.TrimSpace(os.Getenv("HCDR_DATABASE_URL")); db != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		repo, err := store.NewPostgresStoreWithoutMigrations(ctx, db)
		cancel()
		if err != nil {
			slog.Error("connect registration task store", "error", err)
			os.Exit(1)
		}
		defer repo.Close()
		go s.runTaskLoop(repo, env("HOSTNAME", "cluster-registration-executor"))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /v1/inspect", s.inspect)
	if err := http.ListenAndServe(env("HCDR_REGISTRATION_EXECUTOR_ADDR", ":18082"), mux); err != nil {
		slog.Error("registration executor stopped", "error", err)
		os.Exit(1)
	}
}

type directInstallRequest struct {
	Token            string `json:"token"`
	InstallScriptURL string `json:"installScriptUrl"`
	Endpoint         string `json:"endpoint"`
	EndpointPublic   string `json:"endpointPublic"`
	Namespace        string `json:"namespace"`
	Context          string `json:"context"`
	StorageClass     string `json:"storageClass"`
}

func (s *server) runTaskLoop(repo store.Store, executorID string) {
	for {
		task, ok, err := repo.ClaimQueuedTask("cluster-registration", executorID)
		if err != nil {
			s.logger.Error("claim registration task", "error", err)
			time.Sleep(3 * time.Second)
			continue
		}
		if !ok {
			time.Sleep(2 * time.Second)
			continue
		}
		s.runRegistrationTask(repo, task)
	}
}

func (s *server) runRegistrationTask(repo store.Store, task store.Task) {
	sessionID := stringValue(task.Payload, "sessionId")
	contextName := stringValue(task.Payload, "context")
	fail := func(code, message string) {
		_, _, _ = repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "failed", Progress: 100, ErrorCode: code, ErrorMessage: message, Payload: map[string]any{"stage": "failed"}, MarkDone: true})
		_ = repo.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "error", Reason: code, Message: message})
		if sessionIDPattern.MatchString(sessionID) {
			_ = os.RemoveAll(filepath.Join(s.baseDir, sessionID))
		}
	}
	if !sessionIDPattern.MatchString(sessionID) || contextName == "" {
		fail("REGISTRATION_SESSION_INVALID", "The queued registration task does not reference a valid session.")
		return
	}
	sessionDir := filepath.Join(s.baseDir, sessionID)
	raw, err := os.ReadFile(filepath.Join(sessionDir, "install-request.json"))
	if err != nil {
		fail("REGISTRATION_SESSION_NOT_FOUND", "The temporary registration credential expired before execution started.")
		return
	}
	var request directInstallRequest
	if err = json.Unmarshal(raw, &request); err != nil || request.Token == "" || request.InstallScriptURL == "" || request.Endpoint == "" || request.Context != contextName {
		fail("REGISTRATION_REQUEST_INVALID", "The sealed registration request is incomplete or inconsistent.")
		return
	}
	_ = repo.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "preflight_started", Message: "Running provider and cluster preflight checks."})
	_, _, _ = repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "running", Progress: 5, Payload: map[string]any{"stage": "preflight"}, MarkStarted: true})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	script, err := downloadInstaller(ctx, request.InstallScriptURL)
	if err != nil {
		fail("INSTALLER_DOWNLOAD_FAILED", err.Error())
		return
	}
	scriptPath := filepath.Join(os.TempDir(), "hypercdr-install-"+task.ID+".sh")
	if err = os.WriteFile(scriptPath, script, 0700); err != nil {
		fail("INSTALLER_PREPARE_FAILED", "The executor could not prepare the versioned installer.")
		return
	}
	defer os.Remove(scriptPath)
	args := []string{scriptPath, "--token", request.Token, "--endpoint", request.Endpoint, "--cluster-type", "huaweicloud-cce", "--kubeconfig", filepath.Join(sessionDir, "kubeconfig"), "--context", request.Context, "--namespace", request.Namespace, "--executor-mode", "kubernetes", "--install-registry-ca", "false", "--interactive", "false"}
	if request.EndpointPublic != "" && request.EndpointPublic != request.Endpoint {
		args = append(args, "--endpoint-public", request.EndpointPublic)
	}
	if request.StorageClass != "" {
		args = append(args, "--storage-class", request.StorageClass)
	}
	output, canceled, timedOut, runErr := runInstallProcess(ctx, repo, task.ID, args)
	_ = os.RemoveAll(sessionDir)
	if canceled {
		_, _, _ = repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "canceled", Progress: 100, ErrorCode: "REGISTRATION_CANCELED", ErrorMessage: "Registration was canceled and rollback completed.", Payload: map[string]any{"stage": "canceled"}, MarkDone: true})
		_ = repo.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "warning", Reason: "registration_canceled", Message: "Registration was canceled and rollback completed."})
		return
	}
	if timedOut {
		fail("REGISTRATION_TIMEOUT", "Registration exceeded 20 minutes and was stopped; installer rollback was requested.")
		return
	}
	if runErr != nil {
		message := sanitizeInstallFailure(output, request.Token, runErr)
		code := "REGISTRATION_INSTALL_FAILED"
		fail(code, message)
		return
	}
	_ = repo.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "registration_completed", Message: "Agent and Velero installation completed and agent registration was confirmed."})
	_, _, _ = repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "succeeded", Progress: 100, Payload: map[string]any{"stage": "completed"}, MarkDone: true})
}

func runInstallProcess(ctx context.Context, repo store.Store, taskID string, args []string) ([]byte, bool, bool, error) {
	cmd := exec.Command("/bin/bash", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		return output.Bytes(), false, false, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	terminate := func(canceled, timedOut bool) ([]byte, bool, bool, error) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case err := <-done:
			return output.Bytes(), canceled, timedOut, err
		case <-time.After(20 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			return output.Bytes(), canceled, timedOut, <-done
		}
	}
	for {
		select {
		case err := <-done:
			return output.Bytes(), false, false, err
		case <-ctx.Done():
			return terminate(false, true)
		case <-ticker.C:
			task, ok, err := repo.GetTask(taskID)
			if err == nil && ok && task.Status == "canceling" {
				return terminate(true, false)
			}
		}
	}
}

func downloadInstaller(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("The platform installer URL is invalid.")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("installer download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("installer download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 3<<20))
	if err != nil || len(data) == 0 || len(data) >= 3<<20 {
		return nil, errors.New("The installer response is empty or exceeds 3 MiB.")
	}
	if !strings.HasPrefix(string(data), "#!/usr/bin/env bash") {
		return nil, errors.New("The platform returned an invalid installer document.")
	}
	return data, nil
}

func sanitizeInstallFailure(output []byte, token string, runErr error) string {
	text := strings.ReplaceAll(string(output), token, "[REDACTED]")
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	message := strings.TrimSpace(strings.Join(lines, "\n"))
	if message == "" {
		message = runErr.Error()
	}
	if len(message) > 4000 {
		message = message[len(message)-4000:]
	}
	return message
}

func stringValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
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
