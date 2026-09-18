package httpserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/store"
)

const validCCEKubeconfig = `apiVersion: v1
kind: Config
current-context: internal
clusters:
- name: internalCluster
  cluster:
    server: https://192.0.2.10:5443
    certificate-authority-data: Y2E=
contexts:
- name: internal
  context:
    cluster: internalCluster
    user: user
users:
- name: user
  user:
    token: test-token
`

func TestCCEKubeconfigUploadListsContextsAndCanBeDeleted(t *testing.T) {
	server := httptest.NewServer(NewRouter(config.Config{}, slog.Default(), store.NewMemoryStore()))
	defer server.Close()

	status, response := uploadTestKubeconfig(t, server.URL, "cce.yaml", validCCEKubeconfig)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body = %#v", status, response)
	}
	id, _ := response["id"].(string)
	if !strings.HasPrefix(id, "ccer_") || response["currentContext"] != "internal" {
		t.Fatalf("unexpected response: %#v", response)
	}
	contexts, ok := response["contexts"].([]any)
	if !ok || len(contexts) != 1 {
		t.Fatalf("contexts = %#v", response["contexts"])
	}
	contextItem := contexts[0].(map[string]any)
	if contextItem["apiServer"] != "https://192.0.2.10:5443" || contextItem["isCurrent"] != true {
		t.Fatalf("context = %#v", contextItem)
	}

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/cluster-registrations/cce/kubeconfigs/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
}

func TestKubeconfigUploadAcceptsStandardExtensionlessFilename(t *testing.T) {
	server := httptest.NewServer(NewRouter(config.Config{}, slog.Default(), store.NewMemoryStore()))
	defer server.Close()

	status, response := uploadTestKubeconfig(t, server.URL, "kubeconfig", validCCEKubeconfig)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body = %#v", status, response)
	}
}

func TestCCEKubeconfigUploadRejectsExecPlugin(t *testing.T) {
	server := httptest.NewServer(NewRouter(config.Config{}, slog.Default(), store.NewMemoryStore()))
	defer server.Close()
	unsafe := strings.ReplaceAll(validCCEKubeconfig, "    token: test-token", "    exec:\n      command: steal-credentials")
	status, response := uploadTestKubeconfig(t, server.URL, "cce.yaml", unsafe)
	if status != http.StatusUnprocessableEntity || response["error"] != "kubeconfig_unsupported" {
		t.Fatalf("status = %d, response = %#v", status, response)
	}
	if !strings.Contains(response["message"].(string), "external authentication plugin") {
		t.Fatalf("message = %v", response["message"])
	}
}

func TestCCEKubeconfigUploadRejectsExternalCredentialFiles(t *testing.T) {
	server := httptest.NewServer(NewRouter(config.Config{}, slog.Default(), store.NewMemoryStore()))
	defer server.Close()
	unsafe := strings.ReplaceAll(validCCEKubeconfig, "    token: test-token", "    client-key: /tmp/client.key")
	status, response := uploadTestKubeconfig(t, server.URL, "cce.yaml", unsafe)
	if status != http.StatusUnprocessableEntity || response["error"] != "kubeconfig_unsupported" {
		t.Fatalf("status = %d, response = %#v", status, response)
	}
}

func TestCCEKubeconfigJanitorRemovesExpiredRestartOrphans(t *testing.T) {
	sessionDir := t.TempDir()
	expiredID := "ccer_abcdefghijklmnopqrstuvwxyz123456"
	recentID := "ccer_abcdefghijklmnopqrstuvwxyz654321"
	for _, id := range []string{expiredID, recentID} {
		dir := filepath.Join(sessionDir, id)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "kubeconfig"), []byte(validCCEKubeconfig), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().UTC().Add(-cceUploadTTL - time.Minute)
	if err := os.Chtimes(filepath.Join(sessionDir, expiredID, "kubeconfig"), old, old); err != nil {
		t.Fatal(err)
	}

	_ = NewRouter(config.Config{RegistrationSessionDir: sessionDir}, slog.Default(), store.NewMemoryStore())
	if _, err := os.Stat(filepath.Join(sessionDir, expiredID)); !os.IsNotExist(err) {
		t.Fatalf("expired restart orphan was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, recentID, "kubeconfig")); err != nil {
		t.Fatalf("unexpired upload was removed: %v", err)
	}
}

func TestCCEDirectRegistrationRequiresInspectionAndCreatesIdempotentTask(t *testing.T) {
	executor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer executor-token" {
			t.Fatalf("missing executor authentication")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"context": "internal", "clusterName": "cce-test", "clusterId": "cluster-uid", "serverVersion": "v1.35.3", "nodeCount": 1, "storageClasses": []string{"csi-disk"}, "defaultStorageClass": "csi-disk"})
	}))
	defer executor.Close()
	sessionDir := t.TempDir()
	repo := store.NewMemoryStore()
	cfg := config.Config{RegistrationSessionDir: sessionDir, RegistrationExecutorEndpoint: executor.URL, RegistrationExecutorToken: "executor-token", AgentNamespace: "hypercdr-agent", BaseURL: "https://platform:18443"}
	server := httptest.NewServer(NewRouter(cfg, slog.Default(), repo))
	defer server.Close()
	status, uploaded := uploadTestKubeconfig(t, server.URL, "cce.yaml", validCCEKubeconfig)
	if status != http.StatusCreated {
		t.Fatalf("upload: %d %#v", status, uploaded)
	}
	sessionID := uploaded["id"].(string)
	registration := map[string]any{"sessionId": sessionID, "context": "internal", "storageClass": "csi-disk", "idempotencyKey": "registration-request-0001"}
	status, response := postTestJSON(t, server.URL+"/api/v1/cluster-registrations/cce/tasks", registration)
	if status != http.StatusConflict || response["error"] != "inspection_required" {
		t.Fatalf("registration bypassed inspection: %d %#v", status, response)
	}
	status, response = postTestJSON(t, server.URL+"/api/v1/cluster-registrations/cce/inspections", map[string]string{"sessionId": sessionID, "context": "internal"})
	if status != http.StatusOK {
		t.Fatalf("inspection: %d %#v", status, response)
	}
	status, first := postTestJSON(t, server.URL+"/api/v1/cluster-registrations/cce/tasks", registration)
	if status != http.StatusAccepted {
		t.Fatalf("registration: %d %#v", status, first)
	}
	status, second := postTestJSON(t, server.URL+"/api/v1/cluster-registrations/cce/tasks", registration)
	if status != http.StatusOK || first["id"] != second["id"] {
		t.Fatalf("idempotency failed: %d %#v %#v", status, first, second)
	}
	requestRaw, err := os.ReadFile(filepath.Join(sessionDir, sessionID, "install-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(requestRaw), "registration-request-0001") || !strings.Contains(string(requestRaw), `"token"`) {
		t.Fatalf("sealed request content invalid: %s", requestRaw)
	}
	var installRequest map[string]any
	if err := json.Unmarshal(requestRaw, &installRequest); err != nil {
		t.Fatal(err)
	}
	if installRequest["installScriptUrl"] != "https://platform:18443/install.sh" || installRequest["endpoint"] != "wss://platform:18443/ws/agent" {
		t.Fatal("automatic registration did not preserve the configured external port")
	}
}

func TestQueuedCCEDirectRegistrationCanBeCanceled(t *testing.T) {
	repo := store.NewMemoryStore()
	task, err := repo.CreateTask(store.TaskInput{TenantID: store.DefaultTenantID, Type: "cluster-registration", Status: "queued", Payload: map[string]any{"sessionId": "ccer_abcdefghijklmnopqrstuvwxyz123456"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouter(config.Config{}, slog.Default(), repo))
	defer server.Close()
	status, response := postTestJSON(t, server.URL+"/api/v1/tasks/"+task.ID+"/cancel", map[string]any{})
	if status != http.StatusAccepted {
		t.Fatalf("cancel: %d %#v", status, response)
	}
	updated, ok, err := repo.GetTask(task.ID)
	if err != nil || !ok || updated.Status != "canceled" || updated.CompletedAt.IsZero() {
		t.Fatalf("queued registration was not canceled: %#v ok=%v err=%v", updated, ok, err)
	}
}

func postTestJSON(t *testing.T, endpoint string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	result := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func uploadTestKubeconfig(t *testing.T, baseURL, filename, contents string) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("kubeconfig", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(contents))
	_ = w.Close()
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/cluster-registrations/cce/kubeconfigs", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	result := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}
