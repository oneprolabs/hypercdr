package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/config"
	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestIsolatedNormalAgentConsistencyPublication(t *testing.T) {
	runIsolatedNormalAgentConsistency(t, false)
}

func TestIsolatedNormalAgentRejectsMissingSnapshotClass(t *testing.T) {
	runIsolatedNormalAgentConsistency(t, true)
}

func runIsolatedNormalAgentConsistency(t *testing.T, rejectMissingClass bool) {
	t.Helper()
	if os.Getenv("HYPERCDR_RUN_ISOLATED_AGENT_PLATFORM_TEST") != "1" {
		t.Skip("requires explicit normal Agent and authorized isolated 123 acceptance")
	}
	kubeconfig := os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG")
	binary := os.Getenv("HYPERCDR_CONSISTENCY_AGENT_BINARY")
	directory := filepath.Clean(os.Getenv("HYPERCDR_CONSISTENCY_PLATFORM_EVIDENCE_DIR"))
	if !filepath.IsAbs(binary) || !strings.HasPrefix(directory, "/data/hypercdr-runtime/") {
		t.Fatal("requires built Agent and runtime evidence directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	read := func(arguments ...string) []byte {
		t.Helper()
		command := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", kubeconfig}, arguments...)...)
		output, err := command.Output()
		if err != nil {
			t.Fatalf("isolated read-only preflight failed: %v", err)
		}
		return output
	}
	if strings.TrimSpace(string(read("config", "view", "--minify", "-o", "jsonpath={.clusters[0].cluster.server}"))) != "https://192.168.7.123:6443" {
		t.Fatal("only authorized 123 may be used")
	}
	const source = "hcdr-consistency-test"
	const agentNamespace = "hcdr-consistency-velero"
	for _, namespace := range []string{source, agentNamespace} {
		if strings.TrimSpace(string(read("get", "namespace", namespace, "-o", `jsonpath={.metadata.labels.hypercdr\.io/isolated-test}`))) != "application-consistency" {
			t.Fatal("isolated namespace safety label is missing")
		}
	}
	uid := func(kind, name string) string {
		return strings.TrimSpace(string(read("-n", source, "get", kind, name, "-o", "jsonpath={.metadata.uid}")))
	}
	group := &protocol.ConsistencyGroup{ID: "isolated-platform-writer", Strategy: "offline-v1", ApplicationProfile: "paired-ledger-v1", MaxPauseSeconds: 120,
		Workloads: []protocol.ConsistencyWorkload{{Namespace: source, Name: "writer", Kind: "Deployment", UID: uid("deployment", "writer")}},
		Volumes:   []protocol.ConsistencyVolume{{Namespace: source, Name: "ledger", UID: uid("pvc", "ledger"), SnapshotClass: "hcdr-test-longhorn"}, {Namespace: source, Name: "commits", UID: uid("pvc", "commits"), SnapshotClass: "hcdr-test-longhorn"}}}
	originalGeneration := strings.TrimSpace(string(read("-n", source, "get", "deployment", "writer", "-o", "jsonpath={.metadata.generation}")))
	if rejectMissingClass {
		missingClass := fmt.Sprintf("hcdr-isolated-absent-%d", time.Now().UnixNano())
		if output := read("get", "volumesnapshotclasses.snapshot.storage.k8s.io", missingClass, "--ignore-not-found", "-o", "name"); strings.TrimSpace(string(output)) != "" {
			t.Fatal("negative-test snapshot class unexpectedly exists")
		}
		for index := range group.Volumes {
			group.Volumes[index].SnapshotClass = missingClass
		}
	}
	repo := newTestStore(t)
	handler := NewRouter(config.Config{}, slog.Default(), repo).(*managedRouter)
	server := httptest.NewServer(handler)
	defer server.Close()
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := handler.Close(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	token, err := repo.CreateAgentToken(store.DefaultTenantID, "", "isolated-normal-agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	logFile, err := os.OpenFile(filepath.Join(directory, "normal-agent.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	command := exec.Command(binary)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "HCDR_") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env,
		"HCDR_PLATFORM_ENDPOINT=ws"+server.URL[4:]+"/ws/agent", "HCDR_INSTALL_TOKEN="+token.Token,
		"HCDR_AGENT_STATE_DIR="+filepath.Join(directory, "state"), "HCDR_AGENT_NAMESPACE="+agentNamespace,
		"HCDR_AGENT_ID=isolated-platform-acceptance", "HCDR_CLUSTER_NAME=isolated-platform-acceptance",
		"HCDR_EXECUTOR_MODE=kubernetes", "HCDR_INVENTORY_MODE=static", "HCDR_BACKUP_BACKEND=velero",
		"HCDR_KUBECONFIG="+kubeconfig, "HCDR_HEARTBEAT_INTERVAL=5s",
		"HCDR_CONSISTENCY_JOURNAL_NAMESPACE="+source, "HCDR_CONSISTENCY_APPLICATION_PROFILE=paired-ledger-v1",
		"HCDR_CONSISTENCY_RECOVERY_HEARTBEAT=consistency-recovery-heartbeat")
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}()
	clusterID := ""
	for clusterID == "" {
		clusters, err := repo.ListClusters()
		if err != nil {
			t.Fatal(err)
		}
		for _, cluster := range clusters {
			if cluster.Name == "isolated-platform-acceptance" && handler.router.hub.supportsApplicationConsistency(cluster.ID, group.ApplicationProfile) {
				clusterID = cluster.ID
			}
		}
		select {
		case err := <-done:
			done <- err
			t.Fatalf("normal Agent exited before qualified registration: %v", err)
		case <-ctx.Done():
			t.Fatal("normal Agent registration timed out; inspect retained process log")
		case <-time.After(time.Second):
		}
	}
	app := seedSchedulerApplication(t, repo, clusterID, source)
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{SourceClusterID: clusterID, AppID: app.ID, Status: "active", DataProtection: store.DataProtectionSettings{Mode: "application-consistent", Group: group}})
	if err != nil {
		t.Fatal(err)
	}
	backupName := fmt.Sprintf("isolated-platform-%d", time.Now().UnixNano())
	task, err := repo.CreateTask(store.TaskInput{ClusterID: clusterID, ProtectionPlanID: plan.ID, Type: "backup", Status: "queued", CommandID: store.NewPublicID(), Payload: map[string]any{"sourceNamespace": source, "storageRepo": "consistency-test", "veleroBackupName": backupName, "labelSelector": map[string]any{"matchLabels": map[string]string{"hypercdr.io/consistency-fixture": "writer"}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		state, found, err := repo.GetTask(task.ID)
		if err != nil || !found {
			t.Errorf("retaining observed task failed: %v", err)
			return
		}
		encoded, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(directory, "observed-task.json"), encoded, 0600); err != nil {
			t.Error(err)
		}
	}()
	connection, connected := handler.router.hub.get(clusterID)
	if !connected {
		t.Fatal("normal Agent disconnected before dispatch")
	}
	if err := handler.router.dispatchStoredTask(connection, task); err != nil {
		t.Fatal(err)
	}
	t.Logf("normal Agent dispatched task %s backup %s", task.ID, backupName)
	for {
		state, _, err := repo.GetTask(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !state.CompletedAt.IsZero() {
			encoded, _ := json.MarshalIndent(state, "", "  ")
			if err := os.WriteFile(filepath.Join(directory, "terminal-task.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			if rejectMissingClass {
				if state.Status != "failed" || state.RestorePointID != "" || state.ErrorCode == "" {
					t.Fatalf("preflight failure did not fail closed: %#v", state)
				}
				if output := read("-n", agentNamespace, "get", "backups.velero.io", backupName, "--ignore-not-found", "-o", "name"); strings.TrimSpace(string(output)) != "" {
					t.Fatal("missing snapshot class created a native backup")
				}
				if generation := strings.TrimSpace(string(read("-n", source, "get", "deployment", "writer", "-o", "jsonpath={.metadata.generation}"))); generation != originalGeneration {
					t.Fatal("preflight failure mutated source workload generation")
				}
				if replicas := strings.TrimSpace(string(read("-n", source, "get", "deployment", "writer", "-o", "jsonpath={.spec.replicas}"))); replicas != "1" {
					t.Fatal("source workload did not remain at one replica")
				}
				waitForIsolatedAgentCompletionACK(t, ctx, directory, task.ID)
				t.Logf("normal Agent rejected missing snapshot class: task %s; no backup, restore point or source mutation", task.ID)
				return
			}
			if state.Status != "succeeded" {
				t.Fatalf("normal Agent backup failed: %s %s", state.ErrorCode, state.ErrorMessage)
			}
			point, found, err := repo.GetRestorePoint(state.RestorePointID)
			if err != nil || !found {
				t.Fatalf("published restore point missing: %v", err)
			}
			evidence, err := store.BuildConsistencyRestoreEvidence(point, state)
			if err != nil || evidence == nil || len(evidence.Record.Snapshots) != 2 {
				t.Fatalf("publication lost accepted two-volume evidence: %v", err)
			}
			encoded, _ = json.MarshalIndent(evidence, "", "  ")
			if err := os.WriteFile(filepath.Join(directory, "restore-evidence.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			encoded, _ = json.MarshalIndent(point, "", "  ")
			if err := os.WriteFile(filepath.Join(directory, "restore-point.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			for {
				logs, err := os.ReadFile(filepath.Join(directory, "normal-agent.log"))
				if err != nil {
					t.Fatal(err)
				}
				acknowledged := false
				for _, line := range strings.Split(string(logs), "\n") {
					var entry struct {
						Message string `json:"msg"`
						TaskID  string `json:"task_id"`
						AckType string `json:"ack_type"`
					}
					if json.Unmarshal([]byte(line), &entry) == nil && entry.Message == "platform acknowledged event" && entry.TaskID == task.ID && entry.AckType == protocol.MessageAgentTaskCompleted {
						acknowledged = true
					}
				}
				if acknowledged {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("published backup lacks normal Agent durable acknowledgement")
				case <-time.After(time.Second):
				}
			}
			t.Logf("normal Agent publication verified: task %s point %s", state.ID, point.ID)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("observation timed out; inspect the same task and journal before any retry")
		case <-time.After(time.Second):
		}
	}
}

func waitForIsolatedAgentCompletionACK(t *testing.T, ctx context.Context, directory, taskID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		logs, err := os.ReadFile(filepath.Join(directory, "normal-agent.log"))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(logs), "\n") {
			var entry struct {
				Message string `json:"msg"`
				TaskID  string `json:"task_id"`
				AckType string `json:"ack_type"`
			}
			if json.Unmarshal([]byte(line), &entry) == nil && entry.Message == "platform acknowledged event" && entry.TaskID == taskID && entry.AckType == protocol.MessageAgentTaskFailed {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("failed backup lacks normal Agent durable acknowledgement")
		case <-time.After(time.Second):
		}
	}
}

func TestIsolatedPublishedConsistencyEvidenceExport(t *testing.T) {
	if os.Getenv("HYPERCDR_EXPORT_ISOLATED_PUBLISHED_EVIDENCE") != "1" {
		t.Skip("requires explicit retained isolated publication export opt-in")
	}
	directory := os.Getenv("HYPERCDR_CONSISTENCY_PLATFORM_EVIDENCE_DIR")
	var task store.Task
	var point store.RestorePoint
	for name, destination := range map[string]any{"terminal-task.json": &task, "restore-point.json": &point} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, destination); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.HasPrefix(point.VeleroBackupName, "isolated-platform-") || point.SourceNamespace != "hcdr-consistency-test" {
		t.Fatal("export is restricted to retained isolated platform backup artifacts")
	}
	evidence, err := store.BuildConsistencyRestoreEvidence(point, task)
	if err != nil || evidence == nil || len(evidence.Record.Snapshots) != 2 {
		t.Fatalf("retained publication lacks accepted two-volume evidence: %v", err)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(filepath.Join(directory, "restore-evidence.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if _, err := output.Write(data); err != nil {
		t.Fatal(err)
	}
	t.Log("exported accepted publication evidence from retained task/point artifacts; no cluster mutation or new backup")
}
