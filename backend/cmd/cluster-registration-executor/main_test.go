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

const readyNodesJSON = `{"items":[{"spec":{"unschedulable":false},"status":{"allocatable":{"cpu":"2","memory":"4Gi"},"conditions":[{"type":"Ready","status":"True"}]}},{"spec":{"unschedulable":false},"status":{"allocatable":{"cpu":"1500m","memory":"2Gi"},"conditions":[{"type":"Ready","status":"True"}]}}]}`

const oneReadyNodeJSON = `{"items":[{"spec":{"unschedulable":false},"status":{"allocatable":{"cpu":"2","memory":"4Gi"},"conditions":[{"type":"Ready","status":"True"}]}}]}`

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

func TestRegistrationProgressWriterReportsInstallerStages(t *testing.T) {
	repo := store.NewMemoryStore()
	task, err := repo.CreateTask(store.TaskInput{TenantID: "tenant-a", Type: "cluster-registration", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	writer := &registrationProgressWriter{repo: repo, taskID: task.ID}
	_, _ = writer.Write([]byte("==> OADP backend\n[OK] OADP and Kopia node-agent are ready\n==> Readiness\n"))
	updated, ok, err := repo.GetTask(task.ID)
	if err != nil || !ok || updated.Progress != 90 {
		t.Fatalf("progress = %#v ok=%v err=%v", updated, ok, err)
	}
	events, err := repo.ListTaskEvents(task.ID)
	if err != nil || len(events) != 3 {
		t.Fatalf("events = %#v err=%v", events, err)
	}
}

func TestInspectClusterUsesFixedReadOnlyQueries(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}":             "cce-test-001",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                             "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:              "huaweicloud://node-1",
		`get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "ap-southeast-1",
		"get nodes -o json": readyNodesJSON,
		`get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true\ncsi-nas|false\n",
		"auth can-i create namespaces --all-namespaces":                                    "yes",
		"auth can-i create clusterroles.rbac.authorization.k8s.io --all-namespaces":        "yes",
		"auth can-i create clusterrolebindings.rbac.authorization.k8s.io --all-namespaces": "yes",
		"auth can-i create deployments.apps --all-namespaces":                              "yes",
		"auth can-i create daemonsets.apps --all-namespaces":                               "yes",
		"auth can-i create secrets --all-namespaces":                                       "yes",
		"auth can-i create persistentvolumeclaims --all-namespaces":                        "yes",
	}}
	result, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "internal", "huaweicloud-cce")
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
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`, "-n kube-system get configmap cluster-config -o jsonpath={.data.alias}": "cce-test", "get namespace kube-system -o jsonpath={.metadata.uid}": "12345678-abcd", `get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`: "huaweicloud://node", `get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "region-a", "get nodes -o json": oneReadyNodeJSON, `get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true",
		"auth can-i create namespaces --all-namespaces": "yes", "auth can-i create clusterroles.rbac.authorization.k8s.io --all-namespaces": "yes", "auth can-i create clusterrolebindings.rbac.authorization.k8s.io --all-namespaces": "yes", "auth can-i create deployments.apps --all-namespaces": "yes", "auth can-i create daemonsets.apps --all-namespaces": "yes", "auth can-i create secrets --all-namespaces": "yes", "auth can-i create persistentvolumeclaims --all-namespaces": "yes",
	}}
	s := &server{baseDir: base, runner: runner, logger: discardLogger()}
	if err := s.runInspectionJob(sessionID, "internal", "huaweicloud-cce"); err != nil {
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
	if _, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "kind", "huaweicloud-cce"); err == nil || !strings.Contains(err.Error(), "Huawei Cloud CCE") {
		t.Fatalf("expected CCE rejection, got %v", err)
	}
}

func TestInspectClusterAcceptsQualifiedOpenShift(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.28.15"}}`, "-n kube-system get configmap cluster-config -o jsonpath={.data.alias}": "", "get namespace kube-system -o jsonpath={.metadata.uid}": "12345678-abcd", `get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`: "", "get clusterversion version -o jsonpath={.status.desired.version}": "4.15.31", `get nodes -o jsonpath={range .items[*]}{.status.nodeInfo.operatingSystem}{"/"}{.status.nodeInfo.architecture}{"|"}{.status.nodeInfo.osImage}{"\n"}{end}`: "linux/amd64|Red Hat Enterprise Linux CoreOS 415\nlinux/amd64|Red Hat Enterprise Linux CoreOS 415", `get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "region-a", "get nodes -o json": oneReadyNodeJSON, `get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "ocs-storagecluster-ceph-rbd|true", "auth can-i create namespaces --all-namespaces": "yes", "auth can-i create clusterroles.rbac.authorization.k8s.io --all-namespaces": "yes", "auth can-i create clusterrolebindings.rbac.authorization.k8s.io --all-namespaces": "yes", "auth can-i create deployments.apps --all-namespaces": "yes", "auth can-i create daemonsets.apps --all-namespaces": "yes", "auth can-i create secrets --all-namespaces": "yes", "auth can-i create persistentvolumeclaims --all-namespaces": "yes", "auth can-i create subscriptions.operators.coreos.com --all-namespaces": "yes", "auth can-i create operatorgroups.operators.coreos.com --all-namespaces": "yes", "auth can-i create dataprotectionapplications.oadp.openshift.io --all-namespaces": "yes", "auth can-i create securitycontextconstraints.security.openshift.io --all-namespaces": "yes",
	}}
	result, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "ocp", "openshift")
	if err != nil {
		t.Fatal(err)
	}
	if result.DefaultStorageClass != "ocs-storagecluster-ceph-rbd" {
		t.Fatalf("unexpected inspection: %#v", result)
	}
}

func TestRegistrationTaskTimeoutAllowsOADPInstallation(t *testing.T) {
	t.Setenv("HCDR_OPENSHIFT_REGISTRATION_TIMEOUT_SECONDS", "")
	if got := registrationTaskTimeout("openshift"); got != 35*time.Minute {
		t.Fatalf("OpenShift registration timeout = %s", got)
	}
	for _, clusterType := range []string{"native-kubernetes", "huaweicloud-cce", ""} {
		if got := registrationTaskTimeout(clusterType); got != 20*time.Minute {
			t.Fatalf("%q registration timeout = %s", clusterType, got)
		}
	}
}

func TestOpenShiftRegistrationTaskTimeoutIsConfigurable(t *testing.T) {
	t.Setenv("HCDR_OPENSHIFT_REGISTRATION_TIMEOUT_SECONDS", "2700")
	if got := registrationTaskTimeout("openshift"); got != 45*time.Minute {
		t.Fatalf("configured OpenShift registration timeout = %s", got)
	}
	if got := registrationTaskTimeout("native-kubernetes"); got != 20*time.Minute {
		t.Fatalf("native timeout changed to %s", got)
	}
}

func TestInspectClusterRejectsUnsupportedOpenShiftVersion(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{"version -o json": `{"serverVersion":{"gitVersion":"v1.29.0"}}`, "-n kube-system get configmap cluster-config -o jsonpath={.data.alias}": "", "get namespace kube-system -o jsonpath={.metadata.uid}": "12345678-abcd", `get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`: "", "get clusterversion version -o jsonpath={.status.desired.version}": "4.16.1"}}
	_, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "ocp", "openshift")
	if err == nil || !strings.Contains(err.Error(), "OpenShift 4.14 or 4.15") {
		t.Fatalf("expected version rejection, got %v", err)
	}
}

func TestInspectClusterRejectsUnsupportedOpenShiftNodeOS(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{"version -o json": `{"serverVersion":{"gitVersion":"v1.28.15"}}`, "-n kube-system get configmap cluster-config -o jsonpath={.data.alias}": "", "get namespace kube-system -o jsonpath={.metadata.uid}": "12345678-abcd", `get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`: "", "get clusterversion version -o jsonpath={.status.desired.version}": "4.15.31", `get nodes -o jsonpath={range .items[*]}{.status.nodeInfo.operatingSystem}{"/"}{.status.nodeInfo.architecture}{"|"}{.status.nodeInfo.osImage}{"\n"}{end}`: "linux/amd64|Ubuntu 22.04"}}
	_, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "ocp", "openshift")
	if err == nil || !strings.Contains(err.Error(), "RHCOS or RHEL") {
		t.Fatalf("expected node OS rejection, got %v", err)
	}
}

func TestInspectClusterRejectsIncompletePermissions(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}":             "cce-test",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                             "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:              "huaweicloud://node-1",
		`get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "region-a",
		"get nodes -o json": oneReadyNodeJSON,
		`get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true",
	}}
	if _, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "internal", "huaweicloud-cce"); err == nil || !strings.Contains(err.Error(), "lacks required installation permissions") {
		t.Fatalf("expected permission gate failure, got %v", err)
	}
}

func TestSchedulableCapacityExcludesUnreadyAndCordonNodes(t *testing.T) {
	raw := []byte(`{"items":[
		{"spec":{},"status":{"allocatable":{"cpu":"750m","memory":"1Gi"},"conditions":[{"type":"Ready","status":"True"}]}},
		{"spec":{"unschedulable":true},"status":{"allocatable":{"cpu":"8","memory":"32Gi"},"conditions":[{"type":"Ready","status":"True"}]}},
		{"spec":{},"status":{"allocatable":{"cpu":"8","memory":"32Gi"},"conditions":[{"type":"Ready","status":"False"}]}}
	]}`)
	nodes, cpu, memory, err := schedulableCapacity(raw)
	if err != nil || nodes != 1 || cpu != 750 || memory != 1024*1024*1024 {
		t.Fatalf("unexpected capacity nodes=%d cpu=%d memory=%d err=%v", nodes, cpu, memory, err)
	}
}

func TestValidateKubernetesVersionUsesQualifiedRange(t *testing.T) {
	for _, version := range []string{"v1.27.0", "v1.35.3", "1.33.7-cce"} {
		if err := validateKubernetesVersion(version); err != nil {
			t.Fatalf("expected %s to be supported: %v", version, err)
		}
	}
	for _, version := range []string{"v1.26.9", "v1.36.0", "v2.0.0", "unknown"} {
		if err := validateKubernetesVersion(version); err == nil {
			t.Fatalf("expected %s to be rejected", version)
		}
	}
}

func TestInspectClusterRejectsInsufficientCapacityBeforePermissions(t *testing.T) {
	runner := fakeRunner{responses: map[string]string{
		"version -o json": `{"serverVersion":{"gitVersion":"v1.35.3"}}`,
		"-n kube-system get configmap cluster-config -o jsonpath={.data.alias}":             "cce-test",
		"get namespace kube-system -o jsonpath={.metadata.uid}":                             "12345678-abcd",
		`get nodes -o jsonpath={range .items[*]}{.spec.providerID}{"\n"}{end}`:              "huaweicloud://node-1",
		`get nodes -o jsonpath={.items[0].metadata.labels.topology\.kubernetes\.io/region}`: "region-a",
		"get nodes -o json": `{"items":[{"spec":{},"status":{"allocatable":{"cpu":"250m","memory":"256Mi"},"conditions":[{"type":"Ready","status":"True"}]}}]}`,
		`get storageclass -o jsonpath={range .items[*]}{.metadata.name}{"|"}{.metadata.annotations.storageclass\.kubernetes\.io/is-default-class}{"\n"}{end}`: "csi-disk|true",
	}}
	_, err := inspectCluster(context.Background(), runner, "/session/kubeconfig", "internal", "huaweicloud-cce")
	if err == nil || !strings.Contains(err.Error(), "below registration policy v1") {
		t.Fatalf("expected capacity policy failure, got %v", err)
	}
}
