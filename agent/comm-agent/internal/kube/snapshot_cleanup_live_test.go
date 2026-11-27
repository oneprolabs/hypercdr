package kube

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func TestIsolatedOwnedSnapshotCleanup(t *testing.T) {
	if os.Getenv("HYPERCDR_RUN_ISOLATED_SNAPSHOT_CLEANUP_TEST") != "1" {
		t.Skip("requires explicit isolated snapshot cleanup opt-in")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("HYPERCDR_CONSISTENCY_TEST_KUBECONFIG"))
	if err != nil || config.Host != "https://192.168.7.123:6443" {
		t.Fatal("snapshot cleanup acceptance restricted to authorized cluster 123")
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	const source, agent = "hcdr-consistency-test", "hcdr-consistency-velero"
	for _, namespace := range []string{source, agent} {
		object, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(ctx, namespace, metav1.GetOptions{})
		if err != nil || object.GetLabels()["hypercdr.io/isolated-test"] != "application-consistency" {
			t.Fatal("isolated namespace safety label is missing")
		}
	}
	backup, err := client.Resource(consistencyBackups).Namespace(agent).Get(ctx, "isolated-snapshot-1795603248623673581", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
	if phase != "Completed" || backup.GetLabels()["hypercdr.io/managed-by"] != "hypercdr" || backup.GetLabels()["hypercdr.io/task-id"] == "" {
		t.Fatal("requires the existing completed isolated CSI backup; never changes its status")
	}
	deployments := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(source)
	writer, err := deployments.Get(ctx, "writer", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ready, _, _ := unstructured.NestedInt64(writer.Object, "status", "readyReplicas")
	if ready != 1 {
		t.Fatal("source writer must be ready")
	}
	resource := client.Resource(cleanupSnapshots).Namespace(source)
	var owned, foreign *unstructured.Unstructured
	for _, managed := range []bool{true, false} {
		annotations := map[string]any{}
		if managed {
			annotations["hypercdr.io/snapshot-backup-uid"] = string(backup.GetUID())
			annotations["hypercdr.io/snapshot-task-id"] = backup.GetLabels()["hypercdr.io/task-id"]
		}
		object := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot",
			"metadata": map[string]any{"name": fmt.Sprintf("hcdr-cleanup-proof-%t-%d", managed, time.Now().UnixNano()), "namespace": source, "annotations": annotations},
			"spec":     map[string]any{"source": map[string]any{"persistentVolumeClaimName": "ledger"}, "volumeSnapshotClassName": "hcdr-test-longhorn"},
		}}
		created, err := resource.Create(ctx, object, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
			defer cleanupCancel()
			uid := created.GetUID()
			if err := resource.Delete(cleanupCtx, created.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("cleanup seeded test snapshot: %v", err)
			}
			if err := wait.PollUntilContextCancel(cleanupCtx, time.Second, true, func(ctx context.Context) (bool, error) {
				_, err := resource.Get(ctx, created.GetName(), metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			}); err != nil {
				t.Errorf("test snapshot deletion not confirmed: %v", err)
			}
		})
		if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			current, err := resource.Get(ctx, created.GetName(), metav1.GetOptions{})
			if managed && os.Getenv("HYPERCDR_ISOLATED_CLEANUP_EXTERNAL") == "1" && apierrors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			ready, _, _ := unstructured.NestedBool(current.Object, "status", "readyToUse")
			return ready, nil
		}); err != nil {
			t.Fatal(err)
		}
		if managed {
			owned = created
		} else {
			foreign = created
		}
	}
	cleanupConfig := *config
	cleanupConfig.Impersonate.UserName = "system:serviceaccount:hypercdr-agent:hypercdr-agent"
	cleanupClient, err := dynamic.NewForConfig(&cleanupConfig)
	if err != nil {
		t.Fatal(err)
	}
	applier := NewDynamicManifestApplierWithClient(cleanupClient)
	for range 2 {
		if os.Getenv("HYPERCDR_ISOLATED_CLEANUP_EXTERNAL") != "1" {
			if err := applier.CleanupTerminalBackupSnapshots(ctx, agent); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := resource.Get(ctx, owned.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}); err != nil {
		t.Fatal(err)
	}
	preserved, err := resource.Get(ctx, foreign.GetName(), metav1.GetOptions{})
	if err != nil || preserved.GetUID() != foreign.GetUID() || preserved.GetDeletionTimestamp() != nil || len(preserved.GetAnnotations()) != 0 {
		t.Fatalf("non-owned snapshot was changed: %v", err)
	}
	currentWriter, err := deployments.Get(ctx, "writer", metav1.GetOptions{})
	if err != nil || currentWriter.GetUID() != writer.GetUID() || currentWriter.GetGeneration() != writer.GetGeneration() {
		t.Fatalf("source writer identity changed: %v", err)
	}
	t.Logf("seeded snapshots: owned %s/%s deleted, non-owned %s preserved; writer UID/generation unchanged; external cleanup=%s", source, owned.GetName(), foreign.GetName(), os.Getenv("HYPERCDR_ISOLATED_CLEANUP_EXTERNAL"))
}
