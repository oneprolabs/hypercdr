package httpserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
