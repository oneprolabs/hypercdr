package kube

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/velero"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedSnapshotCancellationCleansTemporarySnapshots(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_SNAPSHOT_CANCEL_TEST") != "1" {
		t.Skip("requires explicit isolated CSI cancellation opt-in")
	}
	runIsolatedSnapshotTermination(t, false, false)
}

func TestIsolatedSnapshotTimeoutCleansTemporarySnapshots(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_SNAPSHOT_TIMEOUT_TEST") != "1" {
		t.Skip("requires explicit isolated CSI timeout failure opt-in")
	}
	runIsolatedSnapshotTermination(t, true, false)
}

func TestIsolatedSnapshotTaskFailureRequestsCancellation(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_SNAPSHOT_TASK_FAILURE_TEST") != "1" {
		t.Skip("requires explicit isolated CSI task failure opt-in")
	}
	runIsolatedSnapshotTermination(t, false, true)
}

func runIsolatedSnapshotTermination(t *testing.T, failByTimeout, failByTask bool) {
	t.Helper()
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG"))
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("CSI cancellation acceptance restricted to authorized cluster 123")
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	const source, agent = "hcdr-consistency-test", "hcdr-consistency-velero"
	for _, name := range []string{source, agent} {
		namespace, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(ctx, name, metav1.GetOptions{})
		if err != nil || namespace.GetLabels()["hypercdr.io/isolated-test"] != "application-consistency" {
			t.Fatal("isolated namespace safety label is missing")
		}
	}
	snapshots := client.Resource(cleanupSnapshots).Namespace(source)
	probe, err := snapshots.Get(ctx, "ledger-storage-probe", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deployments := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(source)
	writer, err := deployments.Get(ctx, "writer", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ready, _, _ := unstructured.NestedInt64(writer.Object, "status", "readyReplicas")
	if ready != 1 {
		t.Fatal("isolated writer must be ready")
	}
	applier := NewDynamicManifestApplierWithClient(client)
	prefix := "isolated-csi-cancel"
	if failByTimeout {
		prefix = "isolated-csi-timeout"
	}
	id := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	command := protocol.BackupCommand{SourceNamespace: source, VeleroBackupName: id, StorageRepo: "consistency-test", DataProtectionMode: "snapshot-crash-consistent"}
	manifest, err := velero.BuildBackupManifest(velero.BackupBuildInput{TaskID: id, CommandID: id, AgentNamespace: agent, Command: command})
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
	if failByTimeout {
		if err := unstructured.SetNestedField(map[string]any(object), "1s", "spec", "itemOperationTimeout"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := applier.ApplyManifest(ctx, object); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if err := applier.CancelBackupVolumeOperations(cleanupCtx, agent, id); err != nil {
			t.Errorf("could not confirm isolated movement stopped: %v", err)
		}
	})
	backup, err := client.Resource(consistencyBackups).Namespace(agent).Get(ctx, id, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	uploadResource := client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace(agent)
	options := metav1.ListOptions{LabelSelector: "velero.io/backup-uid=" + string(backup.GetUID())}
	var captured *unstructured.UnstructuredList
	if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		uploads, err := uploadResource.List(ctx, options)
		if err != nil {
			return false, err
		}
		if len(uploads.Items) != 3 {
			return false, nil
		}
		if failByTimeout {
			captured = uploads
			return true, nil
		}
		for _, upload := range uploads.Items {
			phase, _, _ := unstructured.NestedString(upload.Object, "status", "phase")
			if phase == "Accepted" || phase == "Prepared" || phase == "InProgress" {
				captured = uploads
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if failByTask {
		if err := applier.RequestSnapshotBackupCancellation(ctx, agent, id, id, id); err != nil {
			t.Fatal(err)
		}
	} else if !failByTimeout {
		if err := applier.CancelBackupVolumeOperations(ctx, agent, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		if failByTask {
			if err := applier.CleanupTerminalBackupSnapshots(ctx, agent); err != nil {
				return false, err
			}
		}
		current, err := client.Resource(consistencyBackups).Namespace(agent).Get(ctx, id, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		phase, _, _ := unstructured.NestedString(current.Object, "status", "phase")
		if failByTimeout && phase == "Completed" {
			return false, fmt.Errorf("timeout injection did not fail the backup")
		}
		return snapshotCleanupTerminal(phase), nil
	}); err != nil {
		t.Fatal(err)
	}
	if failByTimeout {
		if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			if err := applier.CleanupTerminalBackupSnapshots(ctx, agent); err != nil {
				return false, err
			}
			currentUploads, err := uploadResource.List(ctx, options)
			if err != nil {
				return false, err
			}
			for _, upload := range currentUploads.Items {
				phase, _, _ := unstructured.NestedString(upload.Object, "status", "phase")
				if phase != "Completed" && phase != "Failed" && phase != "Canceled" {
					return false, nil
				}
			}
			return true, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	uploads, err := uploadResource.List(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	canceled := 0
	failed := 0
	for _, upload := range uploads.Items {
		phase, _, _ := unstructured.NestedString(upload.Object, "status", "phase")
		if phase == "Canceled" {
			canceled++
		} else if failByTimeout && phase == "Failed" {
			failed++
		} else if phase != "Completed" {
			t.Fatalf("unexpected final upload phase %q", phase)
		}
	}
	if canceled+failed == 0 {
		t.Fatal("test did not cancel or fail a CSI upload")
	}
	if err := applier.CleanupTerminalBackupSnapshots(ctx, agent); err != nil {
		t.Fatal(err)
	}
	for _, upload := range captured.Items {
		sourceName, _, _ := unstructured.NestedString(upload.Object, "spec", "csiSnapshot", "volumeSnapshot")
		for namespace, name := range map[string]string{source: sourceName, agent: upload.GetName()} {
			if name == "" {
				t.Fatal("CSI snapshot identity is missing")
			}
			if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
				_, err := client.Resource(cleanupSnapshots).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	currentProbe, err := snapshots.Get(ctx, probe.GetName(), metav1.GetOptions{})
	if err != nil || currentProbe.GetUID() != probe.GetUID() || currentProbe.GetDeletionTimestamp() != nil {
		t.Fatalf("pre-existing snapshot was changed: %v", err)
	}
	currentWriter, err := deployments.Get(ctx, "writer", metav1.GetOptions{})
	if err != nil || currentWriter.GetUID() != writer.GetUID() || currentWriter.GetGeneration() != writer.GetGeneration() {
		t.Fatalf("writer identity changed: %v", err)
	}
	ready, _, _ = unstructured.NestedInt64(currentWriter.Object, "status", "readyReplicas")
	if ready != 1 {
		t.Fatal("writer is not ready after cancellation")
	}
	t.Logf("backup=%s timeoutFailure=%t taskFailure=%t canceledUploads=%d failedUploads=%d: original and exposure snapshots absent; pre-existing probe and writer unchanged", id, failByTimeout, failByTask, canceled, failed)
}
