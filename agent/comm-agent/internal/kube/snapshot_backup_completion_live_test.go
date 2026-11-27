package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/velero"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedOrdinarySnapshotBackupCompletion(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_SNAPSHOT_COMPLETION_TEST") != "1" {
		t.Skip("requires explicit isolated snapshot backup opt-in")
	}
	path := os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("snapshot acceptance restricted to authorized cluster 123")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	const source, agent = "hcdr-consistency-test", "hcdr-consistency-velero"
	for _, name := range []string{source, agent} {
		namespace, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil || namespace.Labels["hypercdr.io/isolated-test"] != "application-consistency" {
			t.Fatal("isolated namespace safety label is missing")
		}
	}
	claims, err := client.CoreV1().PersistentVolumeClaims(source).List(ctx, metav1.ListOptions{})
	if err != nil || len(claims.Items) != 3 {
		t.Fatal("expected isolated ledger, commits and storage-probe PVCs")
	}
	for _, claim := range claims.Items {
		if claim.Name != "ledger" && claim.Name != "commits" && claim.Name != "ledger-storage-probe" {
			t.Fatal("unexpected PVC in isolated snapshot fixture")
		}
	}
	writer, err := client.AppsV1().Deployments(source).Get(ctx, "writer", metav1.GetOptions{})
	if err != nil || writer.Spec.Replicas == nil || *writer.Spec.Replicas != 1 || writer.Status.ReadyReplicas != 1 {
		t.Fatal("isolated writer must be running before CSI backup")
	}
	applier, err := NewDynamicManifestApplier(path)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("isolated-snapshot-%d", time.Now().UnixNano())
	task := protocol.TaskDispatchPayload{TaskID: id, CommandID: id, Type: "backup", Backup: &protocol.BackupCommand{SourceNamespace: source, VeleroBackupName: id, StorageRepo: "consistency-test", DataProtectionMode: "snapshot-crash-consistent"}}
	manifest, err := velero.BuildBackupManifest(velero.BackupBuildInput{TaskID: id, CommandID: id, AgentNamespace: agent, Command: *task.Backup})
	if err != nil {
		t.Fatal(err)
	}
	manifest.Metadata.Annotations, err = applier.PrepareSnapshotBackup(ctx, []string{source})
	if err != nil {
		t.Fatal(err)
	}
	object, err := ManifestFromStruct(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applier.ApplyManifest(ctx, object); err != nil {
		t.Fatal(err)
	}
	t.Logf("observing isolated ordinary snapshot backup %s without pausing its writer", id)
	observedOwnership := false
	for {
		backup, err := applier.client.Resource(consistencyBackups).Namespace(agent).Get(ctx, id, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
		snapshots, err := applier.client.Resource(cleanupSnapshots).Namespace(source).List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, snapshot := range snapshots.Items {
			annotations := snapshot.GetAnnotations()
			if annotations["hypercdr.io/snapshot-task-id"] == id && annotations["hypercdr.io/snapshot-backup-uid"] == string(backup.GetUID()) && snapshot.GetLabels()["hypercdr.io/managed-by"] == "hypercdr" {
				observedOwnership = true
			}
		}
		if phase == "Completed" {
			break
		}
		if phase == "Failed" || phase == "PartiallyFailed" || phase == "FailedValidation" {
			t.Fatalf("snapshot backup unsuccessful: %s", phase)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Second):
		}
	}
	if !observedOwnership {
		t.Fatal("did not observe creation-time HyperCDR snapshot ownership")
	}
	result, err := applier.ReadSnapshotBackupCompletion(ctx, agent, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateSnapshotBackupResult(task, result); err != nil {
		t.Fatal(err)
	}
	if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		if err := applier.CleanupTerminalBackupSnapshots(ctx, agent); err != nil {
			return false, err
		}
		snapshots, err := applier.client.Resource(cleanupSnapshots).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, snapshot := range snapshots.Items {
			if snapshot.GetAnnotations()["hypercdr.io/snapshot-task-id"] == id {
				return false, nil
			}
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("HYPERCDR_SNAPSHOT_COMPLETION_RESULT_FILE"); output != "" {
		encoded, err := json.MarshalIndent(map[string]any{"task": task, "result": result}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	current, err := client.AppsV1().Deployments(source).Get(ctx, "writer", metav1.GetOptions{})
	if err != nil || current.UID != writer.UID || current.Generation != writer.Generation || current.Spec.Replicas == nil || *current.Spec.Replicas != 1 || current.Status.ReadyReplicas != 1 {
		t.Fatal("CSI backup changed the writer or left it unavailable")
	}
	t.Logf("all %d captured PVCs have verified durable Kopia snapshots; writer generation unchanged; achieved snapshot-crash-consistent, not application-consistent", len(claims.Items))
}
