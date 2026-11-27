package kube

import (
	"context"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestFilesystemBackupCleanupToleratesAbsentSnapshotAPI(t *testing.T) {
	backup := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "velero.io/v1", "kind": "Backup",
		"metadata": map[string]any{"namespace": "agent", "name": "filesystem", "uid": "backup-uid", "labels": map[string]any{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/task-id": "task"}},
		"spec":     map[string]any{"snapshotVolumes": false, "defaultVolumesToFsBackup": true},
		"status":   map[string]any{"phase": "Completed"},
	}}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{consistencyBackups: "BackupList", cleanupSnapshots: "VolumeSnapshotList"}, backup)
	client.PrependReactor("list", "volumesnapshots", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(cleanupSnapshots.GroupResource(), "")
	})
	applier := &DynamicManifestApplier{client: client}
	if err := applier.CleanupTerminalBackupSnapshots(context.Background(), "agent"); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "list" || (action.GetResource() != consistencyBackups && action.GetResource() != cleanupSnapshots) {
			t.Fatalf("filesystem backup triggered task-specific CSI cleanup: %v", action)
		}
	}
}

func TestTerminalSnapshotCleanupOwnershipAndSafety(t *testing.T) {
	for _, scenario := range []string{"completed", "failed", "canceled", "running-backup", "running-upload", "failed-running-upload", "failed-foreign-upload", "timeout-running-upload", "wrong-cancel-task", "unknown-upload", "foreign", "wrong-uid", "wrong-task", "referenced", "delete-error", "orphan-retry", "orphan-running-upload"} {
		t.Run(scenario, func(t *testing.T) {
			backup := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "velero.io/v1", "kind": "Backup",
				"metadata": map[string]any{"namespace": "agent", "name": "previous-run", "uid": "backup-uid", "resourceVersion": "1", "labels": map[string]any{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/task-id": "previous-task"}},
				"spec":     map[string]any{"snapshotMoveData": true, "includedNamespaces": []any{"source"}}, "status": map[string]any{"phase": "Completed"},
			}}
			snapshot := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot",
				"metadata": map[string]any{"namespace": "source", "name": "temporary", "uid": "snapshot-uid", "resourceVersion": "2", "annotations": map[string]any{"hypercdr.io/snapshot-backup-uid": "backup-uid", "hypercdr.io/snapshot-task-id": "previous-task"}},
			}}
			foreign := snapshot.DeepCopy()
			foreign.SetName("user-snapshot")
			foreign.SetAnnotations(nil)
			upload := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "velero.io/v2alpha1", "kind": "DataUpload",
				"metadata": map[string]any{"namespace": "agent", "name": "upload", "labels": map[string]any{"velero.io/backup-uid": "backup-uid"}}, "status": map[string]any{"phase": "Completed"},
			}}
			upload.SetUID("upload-uid")
			upload.SetResourceVersion("1")
			upload.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "velero.io/v1", Kind: "Backup", Name: backup.GetName(), UID: backup.GetUID()}})
			objects := []runtime.Object{backup, snapshot, foreign, upload}
			switch scenario {
			case "failed":
				_ = unstructured.SetNestedField(backup.Object, "Failed", "status", "phase")
				_ = unstructured.SetNestedField(upload.Object, "Failed", "status", "phase")
			case "canceled":
				_ = unstructured.SetNestedField(backup.Object, "Canceled", "status", "phase")
				_ = unstructured.SetNestedField(upload.Object, "Canceled", "status", "phase")
			case "running-backup":
				_ = unstructured.SetNestedField(backup.Object, "InProgress", "status", "phase")
			case "running-upload":
				_ = unstructured.SetNestedField(upload.Object, "InProgress", "status", "phase")
			case "timeout-running-upload", "wrong-cancel-task":
				_ = unstructured.SetNestedField(backup.Object, "WaitingForPluginOperations", "status", "phase")
				_ = unstructured.SetNestedField(upload.Object, "Accepted", "status", "phase")
				cancelTask := "previous-task"
				if scenario == "wrong-cancel-task" {
					cancelTask = "other-task"
				}
				backup.SetAnnotations(map[string]string{"hypercdr.io/snapshot-cancel-task-id": cancelTask})
			case "failed-running-upload", "failed-foreign-upload":
				_ = unstructured.SetNestedField(backup.Object, "Failed", "status", "phase")
				_ = unstructured.SetNestedField(upload.Object, "InProgress", "status", "phase")
				if scenario == "failed-foreign-upload" {
					upload.SetOwnerReferences(nil)
				}
			case "unknown-upload":
				_ = unstructured.SetNestedField(upload.Object, "", "status", "phase")
			case "foreign":
				snapshot.SetAnnotations(nil)
			case "wrong-uid":
				snapshot.SetAnnotations(map[string]string{"hypercdr.io/snapshot-backup-uid": "other-uid", "hypercdr.io/snapshot-task-id": "previous-task"})
			case "wrong-task":
				snapshot.SetAnnotations(map[string]string{"hypercdr.io/snapshot-backup-uid": "backup-uid", "hypercdr.io/snapshot-task-id": "other-task"})
			case "referenced":
				objects = append(objects, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"namespace": "source", "name": "exposure"}, "spec": map[string]any{"dataSource": map[string]any{"kind": "VolumeSnapshot", "name": "temporary"}}}})
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
				consistencyBackups: "BackupList", cleanupSnapshots: "VolumeSnapshotList",
				{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}: "DataUploadList",
				{Version: "v1", Resource: "persistentvolumeclaims"}:                "PersistentVolumeClaimList",
			}, objects...)
			deletionAttempts := 0
			client.PrependReactor("delete", "volumesnapshots", func(action clienttesting.Action) (bool, runtime.Object, error) {
				deletionAttempts++
				if scenario == "delete-error" || ((scenario == "orphan-retry" || scenario == "orphan-running-upload") && deletionAttempts == 1) {
					return true, nil, fmt.Errorf("injected deletion failure")
				}
				return false, nil, nil
			})
			applier := NewDynamicManifestApplierWithClient(cleanupCheckingClient{Interface: client, check: func(options metav1.DeleteOptions) {
				if options.Preconditions == nil || options.Preconditions.UID == nil || string(*options.Preconditions.UID) != "snapshot-uid" || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "2" {
					t.Fatal("snapshot deletion lacks exact UID/version preconditions")
				}
			}})
			err := applier.CleanupTerminalBackupSnapshots(context.Background(), "agent")
			if (err != nil) != (scenario == "delete-error" || scenario == "orphan-retry" || scenario == "orphan-running-upload") {
				t.Fatalf("unexpected cleanup error: %v", err)
			}
			_, err = client.Resource(cleanupSnapshots).Namespace("source").Get(context.Background(), "temporary", metav1.GetOptions{})
			deleted := scenario == "completed" || scenario == "failed" || scenario == "canceled"
			if apierrors.IsNotFound(err) != deleted {
				t.Fatalf("unexpected deletion result: %v", err)
			}
			if scenario == "failed-running-upload" || scenario == "failed-foreign-upload" || scenario == "timeout-running-upload" || scenario == "wrong-cancel-task" {
				currentUpload, err := client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace("agent").Get(context.Background(), upload.GetName(), metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				requested, _, _ := unstructured.NestedBool(currentUpload.Object, "spec", "cancel")
				if requested != (scenario == "failed-running-upload" || scenario == "timeout-running-upload") {
					t.Fatal("failed backup cancellation ignored DataUpload ownership")
				}
			}
			if _, err := client.Resource(cleanupSnapshots).Namespace("source").Get(context.Background(), "user-snapshot", metav1.GetOptions{}); err != nil {
				t.Fatalf("external snapshot changed: %v", err)
			}
			if scenario == "orphan-retry" || scenario == "orphan-running-upload" {
				if err := client.Resource(consistencyBackups).Namespace("agent").Delete(context.Background(), backup.GetName(), metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				approved, err := client.Resource(cleanupSnapshots).Namespace("source").Get(context.Background(), "temporary", metav1.GetOptions{})
				if err != nil || approved.GetAnnotations()["hypercdr.io/snapshot-cleanup-approved-uid"] != "snapshot-uid" {
					t.Fatalf("terminal cleanup proof was not retained: %v", err)
				}
				copied := approved.DeepCopy()
				copied.SetName("copied-snapshot")
				copied.SetUID("other-snapshot-uid")
				if _, err := client.Resource(cleanupSnapshots).Namespace("source").Create(context.Background(), copied, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				if scenario == "orphan-running-upload" {
					_ = unstructured.SetNestedField(upload.Object, "InProgress", "status", "phase")
					if _, err := client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace("agent").Update(context.Background(), upload, metav1.UpdateOptions{}); err != nil {
						t.Fatal(err)
					}
				}
				if err := applier.CleanupTerminalBackupSnapshots(context.Background(), "agent"); err != nil {
					t.Fatalf("orphan retry failed: %v", err)
				}
				_, err = client.Resource(cleanupSnapshots).Namespace("source").Get(context.Background(), "temporary", metav1.GetOptions{})
				if apierrors.IsNotFound(err) != (scenario == "orphan-retry") {
					t.Fatalf("orphan cleanup ignored upload activity: %v", err)
				}
				if _, err := client.Resource(cleanupSnapshots).Namespace("source").Get(context.Background(), copied.GetName(), metav1.GetOptions{}); err != nil {
					t.Fatalf("copied ownership proof allowed deleting a different UID: %v", err)
				}
			}
			if scenario != "delete-error" {
				if err := applier.CleanupTerminalBackupSnapshots(context.Background(), "agent"); err != nil {
					t.Fatalf("idempotent cleanup failed: %v", err)
				}
			}
		})
	}
}

type cleanupCheckingClient struct {
	dynamic.Interface
	check func(metav1.DeleteOptions)
}

func TestSnapshotCleanupRequiresExplicitAgentNamespace(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	applier := NewDynamicManifestApplierWithClient(client)
	for _, namespace := range []string{"", "*"} {
		if err := applier.CleanupTerminalBackupSnapshots(context.Background(), namespace); err == nil {
			t.Fatal("cleanup accepted an unscoped namespace")
		}
	}
	if len(client.Actions()) != 0 {
		t.Fatal("unscoped cleanup accessed Kubernetes resources")
	}
}

func (client cleanupCheckingClient) Resource(resource schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return cleanupCheckingNamespace{NamespaceableResourceInterface: client.Interface.Resource(resource), check: client.check}
}

type cleanupCheckingNamespace struct {
	dynamic.NamespaceableResourceInterface
	check func(metav1.DeleteOptions)
}

func (resource cleanupCheckingNamespace) Namespace(namespace string) dynamic.ResourceInterface {
	return cleanupCheckingResource{ResourceInterface: resource.NamespaceableResourceInterface.Namespace(namespace), check: resource.check}
}

type cleanupCheckingResource struct {
	dynamic.ResourceInterface
	check func(metav1.DeleteOptions)
}

func (resource cleanupCheckingResource) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	resource.check(options)
	return resource.ResourceInterface.Delete(ctx, name, options, subresources...)
}
