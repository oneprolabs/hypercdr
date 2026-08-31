package httpserver

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hypercdr-platform/platform/backend/internal/config"
)

func TestCreateRegistrationExecutorJobUsesFixedHardenedTemplate(t *testing.T) {
	var received map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/apis/batch/v1/namespaces/hypercdr/jobs" || req.Header.Get("Authorization") != "Bearer service-token" {
			t.Fatalf("unexpected request: %s %s", req.URL.Path, req.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(raw, &received); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	dir := t.TempDir()
	tokenPath, caPath := filepath.Join(dir, "token"), filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(tokenPath, []byte("service-token"), 0600); err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(server.Certificate().Raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	router := &Router{cfg: config.Config{DeployMode: "helm", RegistrationExecutorImage: "registry/executor:v1", RegistrationExecutorNamespace: "hypercdr", RegistrationConfigSecret: "hypercdr-config", RegistrationSessionPVC: "hypercdr-sessions", RegistrationKubernetesAPI: server.URL, RegistrationServiceTokenPath: tokenPath, RegistrationServiceCAPath: caPath}, logger: slog.Default()}
	if err = router.createRegistrationExecutorJob(context.Background(), "task-123"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(received)
	text := string(raw)
	for _, expected := range []string{`"automountServiceAccountToken":false`, `"backoffLimit":0`, `"readOnlyRootFilesystem":true`, `"drop":["ALL"]`, `"HCDR_REGISTRATION_TASK_ID"`, `"task-123"`, `"claimName":"hypercdr-sessions"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Job is missing %s: %s", expected, text)
		}
	}
}
