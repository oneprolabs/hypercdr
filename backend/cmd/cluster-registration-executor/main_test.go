package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

type fakeRunner struct{ responses map[string]string }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func (f fakeRunner) Run(_ context.Context, _, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	value, ok := f.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected fixed kubectl invocation: %s", key)
	}
	return []byte(value), nil
}

func TestRegistrationTaskDestroysSessionAndCompletes(t *testing.T) {
	installer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("#!/usr/bin/env bash\nexit 0\n")) }))
	defer installer.Close()
	base := t.TempDir()
	sessionID := "ccer_abcdefghijklmnopqrstuvwxyz123456"
	sessionDir := filepath.Join(base, sessionID)
	if err := os.Mkdir(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	request := directInstallRequest{Token: "secret-token", InstallScriptURL: installer.URL, Endpoint: "wss://platform/ws/agent", Namespace: "hypercdr-agent", Context: "internal", StorageClass: "csi-disk"}
	raw, _ := json.Marshal(request)
	if err := os.WriteFile(filepath.Join(sessionDir, "install-request.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	repo := store.NewMemoryStore()
	task, err := repo.CreateTask(store.TaskInput{TenantID: "tenant-a", Type: "cluster-registration", Status: "running", Payload: map[string]any{"sessionId": sessionID, "context": "internal"}})
	if err != nil {
		t.Fatal(err)
	}
	s := &server{baseDir: base, runner: fakeRunner{}, logger: discardLogger()}
	s.runRegistrationTask(repo, task)
	updated, ok, err := repo.GetTask(task.ID)
	if err != nil || !ok || updated.Status != "succeeded" || updated.Progress != 100 {
		t.Fatalf("unexpected task result: %#v ok=%v err=%v", updated, ok, err)
	}
	if _, err = os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("registration credential was not destroyed: %v", err)
	}
}

func TestRegistrationFailureIsRedactedAndSessionDestroyed(t *testing.T) {
	installer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/usr/bin/env bash\necho token=secret-token >&2\nexit 23\n"))
	}))
	defer installer.Close()
	base, sessionID := t.TempDir(), "ccer_abcdefghijklmnopqrstuvwxyz123456"
	sessionDir := filepath.Join(base, sessionID)
	if err := os.Mkdir(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(directInstallRequest{Token: "secret-token", InstallScriptURL: installer.URL, Endpoint: "wss://platform/ws/agent", Namespace: "hypercdr-agent", Context: "internal"})
	if err := os.WriteFile(filepath.Join(sessionDir, "install-request.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	repo := store.NewMemoryStore()
	task, _ := repo.CreateTask(store.TaskInput{TenantID: "tenant-a", Type: "cluster-registration", Status: "running", Payload: map[string]any{"sessionId": sessionID, "context": "internal"}})
	(&server{baseDir: base, runner: fakeRunner{}, logger: discardLogger()}).runRegistrationTask(repo, task)
	updated, _, _ := repo.GetTask(task.ID)
	if updated.Status != "failed" || strings.Contains(updated.ErrorMessage, "secret-token") || !strings.Contains(updated.ErrorMessage, "[REDACTED]") {
		t.Fatalf("failure was not safely recorded: %#v", updated)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("failed session remains: %v", err)
	}
}

func TestRunInstallProcessHonorsCancellationAndTermTrap(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	marker := filepath.Join(dir, "rolled-back")
	contents := "#!/usr/bin/env bash\ntrap 'touch \"" + marker + "\"; exit 130' TERM INT\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(contents), 0700); err != nil {
		t.Fatal(err)
	}
	repo := store.NewMemoryStore()
	task, err := repo.CreateTask(store.TaskInput{TenantID: "tenant-a", Type: "cluster-registration", Status: "canceling"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, canceled, timedOut, err := runInstallProcess(ctx, repo, task.ID, []string{script})
	if !canceled || timedOut {
		t.Fatalf("cancel result: canceled=%v timedOut=%v err=%v", canceled, timedOut, err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("TERM rollback trap did not execute: %v", statErr)
	}
}

func TestInspectClusterUsesFixedReadOnlyQueries(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}":             "cce-test-001",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                             "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:              "huaweicloud://node-1",
		`get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "ap-southeast-1",
		"get nodes -o name": "node/node-1\nnode/node-2\n",
		`get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true\ncsi-nas|false\n",
		"auth can-i create namespaces --all-namespaces":                                    "yes",
		"auth can-i create clusterroles.rbac.authorization.k8s.io --all-namespaces":        "yes",
		"auth can-i create clusterrolebindings.rbac.authorization.k8s.io --all-namespaces": "yes",
		"auth can-i create deployments.apps --all-namespaces":                              "yes",
		"auth can-i create daemonsets.apps --all-namespaces":                               "yes",
		"auth can-i create secrets --all-namespaces":                                       "yes",
		"auth can-i create persistentvolumeclaims --all-namespaces":                        "yes",
	}}
	result, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "internal")
	if err != nil {
		t.Fatal(err)
	}
	if result.ClusterName != "cce-test-001" || result.ServerVersion != "v1.35.3" || result.NodeCount != 2 || result.DefaultStorageClass != "csi-disk" {
		t.Fatalf("unexpected inspection: %#v", result)
	}
}

func TestInspectionJobWritesAtomicResult(t *testing.T) {
	base := t.TempDir()
	sessionID := "ccer_abcdefghijklmnopqrstuvwxyz123456"
	sessionDir := filepath.Join(base, sessionID)
	if err := os.Mkdir(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`, "-n kube-system get configmap cluster-config -o jsonpath={.data.alias}": "cce-test", "get namespace kube-system -o jsonpath={.metadata.uid}": "12345678-abcd", `get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`: "huaweicloud://node", `get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "region-a", "get nodes -o name": "node/node-a", `get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true",
		"auth can-i create namespaces --all-namespaces": "yes", "auth can-i create clusterroles.rbac.authorization.k8s.io --all-namespaces": "yes", "auth can-i create clusterrolebindings.rbac.authorization.k8s.io --all-namespaces": "yes", "auth can-i create deployments.apps --all-namespaces": "yes", "auth can-i create daemonsets.apps --all-namespaces": "yes", "auth can-i create secrets --all-namespaces": "yes", "auth can-i create persistentvolumeclaims --all-namespaces": "yes",
	}}
	s := &server{baseDir: base, runner: runner, logger: discardLogger()}
	if err := s.runInspectionJob(sessionID, "internal"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(sessionDir, "inspection-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result inspectionJobResult
	if json.Unmarshal(raw, &result) != nil || result.Inspection == nil || result.Inspection.ClusterName != "cce-test" {
		t.Fatalf("unexpected result: %s", raw)
	}
	if _, err = os.Stat(filepath.Join(sessionDir, "inspection-result.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary result remains: %v", err)
	}
}

func TestInspectClusterRejectsNonCCE(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.33.0"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}": "",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                 "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:  "kind://node-1",
	}}
	if _, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "kind"); err == nil || !strings.Contains(err.Error(), "Huawei Cloud CCE") {
		t.Fatalf("expected CCE rejection, got %v", err)
	}
}

func TestInspectClusterRejectsIncompletePermissions(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}":             "cce-test",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                             "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:              "huaweicloud://node-1",
		`get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "region-a",
		"get nodes -o name": "node/node-1",
		`get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true",
	}}
	if _, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "internal"); err == nil || !strings.Contains(err.Error(), "lacks required permissions") {
		t.Fatalf("expected permission gate failure, got %v", err)
	}
}
