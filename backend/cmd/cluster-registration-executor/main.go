package main

import (
	"bytes"
	"context"
	"crypto/tls"
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

var sessionIDPattern = regexp.MustCompile(`^ccer_[A-Za-z0-9_-]{20,64}$`)

type inspectRequest struct {
	SessionID   string `json:"sessionId"`
	Context     string `json:"context"`
	ClusterType string `json:"clusterType"`
}

type inspection struct {
	Context             string           `json:"context"`
	ClusterName         string           `json:"clusterName"`
	ClusterID           string           `json:"clusterId"`
	Region              string           `json:"region,omitempty"`
	ServerVersion       string           `json:"serverVersion"`
	NodeCount           int              `json:"nodeCount"`
	StorageClasses      []string         `json:"storageClasses"`
	DefaultStorageClass string           `json:"defaultStorageClass,omitempty"`
	Gates               []inspectionGate `json:"gates"`
}

type inspectionGate struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Keep these requirements versioned and deliberately modest. They cover the
// registration-time Agent and Velero control-plane footprint; application
// restore capacity remains a workload-specific concern checked by Kubernetes.
const (
	registrationCapacityPolicyVersion = "v1"
	minimumRegistrationMilliCPU       = int64(500)
	minimumRegistrationMemoryBytes    = int64(512 * 1024 * 1024)
	minimumSupportedKubernetesMinor   = 27
	maximumSupportedKubernetesMinor   = 35
)

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
	s := &server{baseDir: filepath.Clean(baseDir), runner: commandRunner{}, logger: slog.Default()}
	if sessionID := strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_INSPECT_SESSION_ID")); sessionID != "" {
		contextName := strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_INSPECT_CONTEXT"))
		clusterType := strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_INSPECT_CLUSTER_TYPE"))
		if err := s.runInspectionJob(sessionID, contextName, clusterType); err != nil {
			slog.Error("CCE inspection Job failed", "session", sessionID, "error", err)
			os.Exit(1)
		}
		return
	}
	if db := strings.TrimSpace(os.Getenv("HCDR_DATABASE_URL")); db != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		repo, err := store.NewPostgresStoreWithoutMigrations(ctx, db)
		cancel()
		if err != nil {
			slog.Error("connect registration task store", "error", err)
			os.Exit(1)
		}
		defer repo.Close()
		executorID := env("HOSTNAME", "cluster-registration-executor")
		if taskID := strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_TASK_ID")); taskID != "" {
			task, ok, claimErr := repo.ClaimQueuedTaskByID(taskID, "cluster-registration", executorID)
			if claimErr != nil || !ok {
				slog.Error("claim specified registration task", "task", taskID, "error", claimErr)
				os.Exit(1)
			}
			s.runRegistrationTask(repo, task)
			return
		}
		go s.runTaskLoop(repo, executorID)
	}
	token := strings.TrimSpace(os.Getenv("HCDR_REGISTRATION_EXECUTOR_TOKEN"))
	if token == "" {
		slog.Error("HCDR_REGISTRATION_EXECUTOR_TOKEN is required in HTTP service mode")
		os.Exit(1)
	}
	s.token = token
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /v1/inspect", s.inspect)
	if err := http.ListenAndServe(env("HCDR_REGISTRATION_EXECUTOR_ADDR", ":18082"), mux); err != nil {
		slog.Error("registration executor stopped", "error", err)
		os.Exit(1)
	}
}

type inspectionJobResult struct {
	Inspection *inspection `json:"inspection,omitempty"`
	Error      string      `json:"error,omitempty"`
}

func (s *server) runInspectionJob(sessionID, contextName, clusterType string) error {
	if !sessionIDPattern.MatchString(sessionID) || contextName == "" || len(contextName) > 253 {
		return errors.New("invalid inspection session or context")
	}
	sessionDir := filepath.Join(s.baseDir, sessionID)
	result, inspectErr := inspectCluster(context.Background(), s.runner, filepath.Join(sessionDir, "kubeconfig"), contextName, clusterType)
	payload := inspectionJobResult{}
	if inspectErr != nil {
		payload.Error = inspectErr.Error()
	} else {
		payload.Inspection = &result
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	temporary := filepath.Join(sessionDir, "inspection-result.json.tmp")
	if err = os.WriteFile(temporary, raw, 0600); err != nil {
		return err
	}
	if err = os.Rename(temporary, filepath.Join(sessionDir, "inspection-result.json")); err != nil {
		return err
	}
	return inspectErr
}

type directInstallRequest struct {
	Token            string `json:"token"`
	InstallScriptURL string `json:"installScriptUrl"`
	Endpoint         string `json:"endpoint"`
	EndpointPublic   string `json:"endpointPublic"`
	Namespace        string `json:"namespace"`
	Context          string `json:"context"`
	StorageClass     string `json:"storageClass"`
	ClusterType      string `json:"clusterType"`
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
	clusterType := normalizeClusterType(request.ClusterType)
	if clusterType == "" {
		clusterType = normalizeClusterType(stringValue(task.Payload, "clusterType"))
	}
	if clusterType == "" {
		clusterType = "huaweicloud-cce"
	}
	registrationTimeout := registrationTaskTimeout(clusterType)
	_ = repo.AddTaskEvent(store.TaskEventInput{TaskID: task.ID, Level: "info", Reason: "preflight_started", Message: "Running provider and cluster preflight checks."})
	_, _, _ = repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: task.ID, Status: "running", Progress: 5, Payload: map[string]any{"stage": "preflight"}, MarkStarted: true})
	ctx, cancel := context.WithTimeout(context.Background(), registrationTimeout)
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
	args := []string{scriptPath, "--token", request.Token, "--endpoint", request.Endpoint, "--cluster-type", clusterType, "--kubeconfig", filepath.Join(sessionDir, "kubeconfig"), "--context", request.Context, "--namespace", request.Namespace, "--executor-mode", "kubernetes", "--install-registry-ca", "false", "--interactive", "false"}
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
		fail("REGISTRATION_TIMEOUT", fmt.Sprintf("Registration exceeded %d minutes and was stopped; installer rollback was requested.", int(registrationTimeout/time.Minute)))
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

func registrationTaskTimeout(clusterType string) time.Duration {
	if normalizeClusterType(clusterType) == "openshift" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HCDR_OPENSHIFT_REGISTRATION_TIMEOUT_SECONDS"))); err == nil && seconds >= 60 {
			return time.Duration(seconds) * time.Second
		}
		return 35 * time.Minute
	}
	return 20 * time.Minute
}

func runInstallProcess(ctx context.Context, repo store.Store, taskID string, args []string) ([]byte, bool, bool, error) {
	cmd := exec.Command("/bin/bash", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output := &registrationProgressWriter{repo: repo, taskID: taskID}
	cmd.Stdout, cmd.Stderr = output, output
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

type registrationStage struct {
	needle, reason, message, stage string
	progress                       int
}

var registrationStages = []registrationStage{
	{"==> Isolated installation preflight", "image_preflight_started", "Verifying cluster access and required images.", "image-preflight", 10},
	{"==> Namespace and credentials", "agent_install_started", "Creating the HyperCDR namespace and credentials.", "agent-install", 20},
	{"==> OADP backend", "oadp_install_started", "Installing and reconciling the OADP Operator.", "oadp-operator", 40},
	{"OADP and Kopia node-agent are ready", "oadp_ready", "OADP, Velero, and the Kopia node-agent are ready.", "oadp-ready", 65},
	{"==> HyperCDR agent", "agent_deployment_started", "Deploying the HyperCDR communication agent and state volume.", "agent-deployment", 80},
	{"==> Readiness", "readiness_started", "Waiting for workloads and the Agent connection to become ready.", "readiness", 90},
}

type registrationProgressWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	pending string
	seen    map[string]bool
	repo    store.Store
	taskID  string
}

func (w *registrationProgressWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buffer.Write(data)
	w.pending += string(data)
	for {
		index := strings.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		w.observe(w.pending[:index])
		w.pending = w.pending[index+1:]
	}
	return n, err
}

func (w *registrationProgressWriter) observe(line string) {
	if w.seen == nil {
		w.seen = map[string]bool{}
	}
	for _, stage := range registrationStages {
		if w.seen[stage.reason] || !strings.Contains(line, stage.needle) {
			continue
		}
		w.seen[stage.reason] = true
		_, _, _ = w.repo.UpdateTaskStatus(store.TaskStatusInput{TaskID: w.taskID, Status: "running", Progress: stage.progress, Payload: map[string]any{"stage": stage.stage}})
		_ = w.repo.AddTaskEvent(store.TaskEventInput{TaskID: w.taskID, Level: "info", Reason: stage.reason, Message: stage.message})
	}
}

func (w *registrationProgressWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...)
}

func downloadInstaller(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("The platform installer URL is invalid.")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	client := http.DefaultClient
	// Bootstrap install commands intentionally use curl -k for the platform's
	// self-signed development certificate. Keep executor behavior explicit and
	// opt-in rather than silently weakening TLS verification.
	if os.Getenv("HCDR_REGISTRATION_TLS_INSECURE_SKIP_VERIFY") == "true" {
		client = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: 30 * time.Second} //nolint:gosec
	}
	resp, err := client.Do(req)
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
	clusterType := normalizeClusterType(body.ClusterType)
	if clusterType == "" {
		writeError(w, http.StatusBadRequest, "cluster_type_invalid", "Select Native Kubernetes or Huawei Cloud CCE.")
		return
	}
	result, err := inspectCluster(ctx, s.runner, kubeconfig, body.Context, clusterType)
	if err != nil {
		s.logger.Warn("CCE inspection failed", "session", body.SessionID, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "cce_inspection_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func inspectCluster(ctx context.Context, runner kubectlRunner, kubeconfig, contextName, clusterType string) (inspection, error) {
	run := func(args ...string) (string, error) {
		out, err := runner.Run(ctx, kubeconfig, contextName, args...)
		return strings.TrimSpace(string(out)), err
	}
	version, err := run("version", "-o", "json")
	if err != nil {
		return inspection{}, fmt.Errorf("Kubernetes API connection failed: %s", conciseCommandError(err))
	}
	var versions struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if json.Unmarshal([]byte(version), &versions) != nil || versions.ServerVersion.GitVersion == "" {
		return inspection{}, errors.New("Kubernetes server version could not be determined.")
	}
	if err = validateKubernetesVersion(versions.ServerVersion.GitVersion); err != nil {
		return inspection{}, err
	}
	alias, _ := run("-n", "kube-system", "get", "configmap", "cluster-config", "-o", "jsonpath={.data.alias}")
	clusterID, err := run("get", "namespace", "kube-system", "-o", "jsonpath={.metadata.uid}")
	if err != nil || clusterID == "" {
		return inspection{}, errors.New("CCE cluster identity could not be read.")
	}
	providerIDs, err := run("get", "nodes", "-o", "jsonpath={range .items[*]}{.spec.providerID}{\"\\n\"}{end}")
	if clusterType == "huaweicloud-cce" && (err != nil || (!strings.Contains(strings.ToLower(providerIDs), "huaweicloud") && alias == "")) {
		return inspection{}, errors.New("The selected context could not be verified as Huawei Cloud CCE.")
	}
	if clusterType == "openshift" {
		openshiftVersion, versionErr := run("get", "clusterversion", "version", "-o", "jsonpath={.status.desired.version}")
		if versionErr != nil || (openshiftVersion != "4.14" && openshiftVersion != "4.15" && !strings.HasPrefix(openshiftVersion, "4.14.") && !strings.HasPrefix(openshiftVersion, "4.15.")) {
			return inspection{}, fmt.Errorf("The selected context must be OpenShift 4.14 or 4.15; detected %q", openshiftVersion)
		}
		architectures, architectureErr := run("get", "nodes", "-o", `jsonpath={range .items[*]}{.status.nodeInfo.operatingSystem}{"/"}{.status.nodeInfo.architecture}{"|"}{.status.nodeInfo.osImage}{"\n"}{end}`)
		if architectureErr != nil || strings.TrimSpace(architectures) == "" {
			return inspection{}, errors.New("OpenShift node operating systems and architectures could not be read.")
		}
		for _, line := range strings.Split(architectures, "\n") {
			parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
			if parts[0] == "" {
				continue
			}
			if parts[0] != "linux/amd64" && parts[0] != "linux/x86_64" {
				return inspection{}, fmt.Errorf("OpenShift phase one supports Linux AMD64 nodes only; detected %s", parts[0])
			}
			if len(parts) != 2 || (!strings.Contains(parts[1], "Red Hat Enterprise Linux CoreOS") && !strings.Contains(parts[1], "Red Hat Enterprise Linux")) {
				return inspection{}, fmt.Errorf("OpenShift nodes must use supported RHCOS or RHEL; detected %q", strings.TrimSpace(strings.Join(parts[1:], "|")))
			}
		}
	}
	region, _ := run("get", "nodes", "-o", "jsonpath={.items[0].metadata.labels.topology\\.kubernetes\\.io/region}")
	nodes, err := run("get", "nodes", "-o", "json")
	if err != nil {
		return inspection{}, fmt.Errorf("Worker node capacity could not be read: %w", err)
	}
	classes, err := run("get", "storageclass", "-o", `jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`)
	if err != nil {
		return inspection{}, fmt.Errorf("StorageClasses could not be listed: %w", err)
	}
	result := inspection{Context: contextName, ClusterName: alias, ClusterID: clusterID, Region: region, ServerVersion: versions.ServerVersion.GitVersion}
	if result.ClusterName == "" {
		controlPlaneName, _ := run("get", "nodes", "-l", "node-role.kubernetes.io/control-plane", "-o", "jsonpath={.items[0].metadata.name}")
		if clusterType == "huaweicloud-cce" {
			result.ClusterName = "cce-" + clusterID[:min(8, len(clusterID))]
		} else if controlPlaneName != "" {
			result.ClusterName = controlPlaneName
		} else {
			result.ClusterName = "k8s-" + clusterID[:min(8, len(clusterID))]
		}
	}
	readyNodes, milliCPU, memoryBytes, err := schedulableCapacity([]byte(nodes))
	if err != nil {
		return inspection{}, fmt.Errorf("Worker node capacity is invalid: %w", err)
	}
	result.NodeCount = readyNodes
	if readyNodes == 0 {
		return inspection{}, errors.New("No Ready and schedulable worker node is available for HyperCDR.")
	}
	if milliCPU < minimumRegistrationMilliCPU || memoryBytes < minimumRegistrationMemoryBytes {
		return inspection{}, fmt.Errorf("Ready worker capacity is below registration policy %s: available %dm CPU and %d MiB memory; require at least %dm CPU and %d MiB memory", registrationCapacityPolicyVersion, milliCPU, memoryBytes/(1024*1024), minimumRegistrationMilliCPU, minimumRegistrationMemoryBytes/(1024*1024))
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
	identityLabel := "Kubernetes identity"
	if clusterType == "huaweicloud-cce" {
		identityLabel = "CCE identity"
	} else if clusterType == "openshift" {
		identityLabel = "OpenShift identity"
	}
	result.Gates = append(result.Gates,
		inspectionGate{ID: "identity", Label: identityLabel, Status: "passed", Detail: result.ClusterName + " · " + result.ClusterID},
		inspectionGate{ID: "version", Label: "Kubernetes version", Status: "passed", Detail: result.ServerVersion},
		inspectionGate{ID: "capacity", Label: "Worker capacity", Status: "passed", Detail: fmt.Sprintf("%d Ready schedulable worker node(s) · %dm CPU · %d MiB memory · policy %s", result.NodeCount, milliCPU, memoryBytes/(1024*1024), registrationCapacityPolicyVersion)},
	)
	permissions := [][2]string{{"create", "namespaces"}, {"create", "clusterroles.rbac.authorization.k8s.io"}, {"create", "clusterrolebindings.rbac.authorization.k8s.io"}, {"create", "deployments.apps"}, {"create", "daemonsets.apps"}, {"create", "secrets"}, {"create", "persistentvolumeclaims"}}
	if clusterType == "openshift" {
		permissions = append(permissions, [2]string{"create", "subscriptions.operators.coreos.com"}, [2]string{"create", "operatorgroups.operators.coreos.com"}, [2]string{"create", "dataprotectionapplications.oadp.openshift.io"}, [2]string{"create", "securitycontextconstraints.security.openshift.io"})
	}
	missing := []string{}
	for _, permission := range permissions {
		answer, permissionErr := run("auth", "can-i", permission[0], permission[1], "--all-namespaces")
		answerFields := strings.Fields(answer)
		if permissionErr != nil || len(answerFields) == 0 || answerFields[len(answerFields)-1] != "yes" {
			missing = append(missing, permission[0]+" "+permission[1])
		}
	}
	if len(missing) > 0 {
		return inspection{}, fmt.Errorf("Kubeconfig lacks required installation permissions: %s", strings.Join(missing, ", "))
	}
	result.Gates = append(result.Gates, inspectionGate{ID: "permissions", Label: "Kubernetes permissions", Status: "passed", Detail: "All required namespace, RBAC, workload, Secret, and PVC permissions are available."})
	storageStatus, storageDetail := "passed", result.DefaultStorageClass
	if len(result.StorageClasses) == 0 {
		return inspection{}, errors.New("No compatible StorageClass is available in this Kubernetes cluster.")
	}
	if storageDetail == "" {
		storageStatus, storageDetail = "warning", "No default StorageClass; select one before registration."
	}
	result.Gates = append(result.Gates,
		inspectionGate{ID: "storage", Label: "StorageClass", Status: storageStatus, Detail: storageDetail},
		inspectionGate{ID: "network", Label: "Network and image pull", Status: "deferred", Detail: "An isolated temporary Pod performs DNS, TLS, platform, and image-pull checks before installation."},
	)
	return result, nil
}

func conciseCommandError(err error) string {
	if err == nil {
		return "unknown error"
	}
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if message := strings.TrimSpace(lines[index]); message != "" {
			return message
		}
	}
	return "unknown error"
}

func normalizeClusterType(value string) string {
	switch strings.TrimSpace(value) {
	case "native-kubernetes", "huaweicloud-cce", "openshift":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func schedulableCapacity(raw []byte) (int, int64, int64, error) {
	var list struct {
		Items []struct {
			Spec struct {
				Unschedulable bool `json:"unschedulable"`
			} `json:"spec"`
			Status struct {
				Allocatable map[string]string `json:"allocatable"`
				Conditions  []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return 0, 0, 0, err
	}
	var ready int
	var milliCPU, memoryBytes int64
	for _, node := range list.Items {
		isReady := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				isReady = true
				break
			}
		}
		if node.Spec.Unschedulable || !isReady {
			continue
		}
		cpu, err := parseCPU(node.Status.Allocatable["cpu"])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid allocatable CPU: %w", err)
		}
		memory, err := parseMemory(node.Status.Allocatable["memory"])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid allocatable memory: %w", err)
		}
		ready++
		milliCPU += cpu
		memoryBytes += memory
	}
	return ready, milliCPU, memoryBytes, nil
}

func parseCPU(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "m") {
		return strconv.ParseInt(strings.TrimSuffix(value, "m"), 10, 64)
	}
	cores, err := strconv.ParseFloat(value, 64)
	return int64(cores * 1000), err
}

func parseMemory(value string) (int64, error) {
	value = strings.TrimSpace(value)
	multipliers := map[string]int64{"Ki": 1024, "Mi": 1024 * 1024, "Gi": 1024 * 1024 * 1024, "Ti": 1024 * 1024 * 1024 * 1024, "K": 1000, "M": 1000 * 1000, "G": 1000 * 1000 * 1000}
	for suffix, multiplier := range multipliers {
		if strings.HasSuffix(value, suffix) {
			number, err := strconv.ParseInt(strings.TrimSuffix(value, suffix), 10, 64)
			return number * multiplier, err
		}
	}
	return strconv.ParseInt(value, 10, 64)
}

func validateKubernetesVersion(value string) error {
	match := regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.|$)`).FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 3 {
		return fmt.Errorf("Kubernetes version %q is invalid", value)
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	if major != 1 || minor < minimumSupportedKubernetesMinor || minor > maximumSupportedKubernetesMinor {
		return fmt.Errorf("Kubernetes %s is outside the qualified range v1.%d-v1.%d; use command-based registration only after compatibility qualification", value, minimumSupportedKubernetesMinor, maximumSupportedKubernetesMinor)
	}
	return nil
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
