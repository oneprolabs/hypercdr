package kube

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestDynamicManifestApplierCreatesAndUpdatesVeleroBackup(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		{Group: "velero.io", Version: "v1", Resource: "backups"}: "BackupList",
	})
	applier := NewDynamicManifestApplierWithClient(client)

	manifest := Manifest{
		"apiVersion": "velero.io/v1",
		"kind":       "Backup",
		"metadata": map[string]any{
			"name":      "backup-a",
			"namespace": "hypercdr-agent",
		},
		"spec": map[string]any{
			"storageLocation": "default",
		},
	}
	object, err := applier.ApplyManifest(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if object.Kind != "Backup" || object.Name != "backup-a" || object.Namespace != "hypercdr-agent" {
		t.Fatalf("unexpected applied object: %#v", object)
	}

	manifest["spec"] = map[string]any{"storageLocation": "secondary"}
	if _, err := applier.ApplyManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	created, err := client.Resource(schema.GroupVersionResource{
		Group:    "velero.io",
		Version:  "v1",
		Resource: "backups",
	}).Namespace("hypercdr-agent").Get(context.Background(), "backup-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := created.Object["spec"].(map[string]any)
	if !ok || spec["storageLocation"] != "secondary" {
		t.Fatalf("expected updated spec, got %#v", created.Object["spec"])
	}
}

func TestDeleteMatchingDataDownloadsRemovesOnlyStaleRestoreArtifacts(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datadownloads"}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "DataDownloadList"},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v2alpha1", "kind": "DataDownload",
			"metadata": map[string]any{"name": "stale-download", "namespace": "openshift-adp", "labels": map[string]any{"velero.io/restore-name": "stale-restore"}},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v2alpha1", "kind": "DataDownload",
			"metadata": map[string]any{"name": "current-download", "namespace": "openshift-adp", "labels": map[string]any{"velero.io/restore-name": "current-restore"}},
		}},
	)
	applier := NewDynamicManifestApplierWithClient(client)
	if err := applier.deleteMatchingDataDownloads(context.Background(), "openshift-adp", map[string]struct{}{"stale-restore": {}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resource(gvr).Namespace("openshift-adp").Get(context.Background(), "stale-download", metav1.GetOptions{}); err == nil {
		t.Fatal("stale DataDownload was not deleted")
	}
	if _, err := client.Resource(gvr).Namespace("openshift-adp").Get(context.Background(), "current-download", metav1.GetOptions{}); err != nil {
		t.Fatalf("current DataDownload was deleted: %v", err)
	}
}

func TestDeleteVeleroBackupArtifactsIncludesKopiaDataUploads(t *testing.T) {
	backupGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}
	restoreGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}
	pvbGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}
	dataUploadGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		backupGVR: "BackupList", restoreGVR: "RestoreList", pvbGVR: "PodVolumeBackupList", dataUploadGVR: "DataUploadList",
	},
		&unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "metadata": map[string]any{"name": "backup-a", "namespace": "openshift-adp"}}},
		&unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v2alpha1", "kind": "DataUpload", "metadata": map[string]any{"name": "upload-a", "namespace": "openshift-adp", "labels": map[string]any{"velero.io/backup-name": "backup-a"}}}},
	)
	applier := NewDynamicManifestApplierWithClient(client)
	deleted, err := applier.DeleteVeleroBackupArtifacts(context.Background(), "openshift-adp", []string{"backup-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted["dataUploads"]) != 1 || deleted["dataUploads"][0] != "upload-a" {
		t.Fatalf("Kopia DataUpload cleanup result = %#v", deleted)
	}
}

func TestRestoreObjectPrefix(t *testing.T) {
	prefix, err := restoreObjectPrefix("hypercdr/clusters/cluster-a", "hcdr-restore-demo-1234")
	if err != nil {
		t.Fatal(err)
	}
	if prefix != "hypercdr/clusters/cluster-a/restores/hcdr-restore-demo-1234/" {
		t.Fatalf("unexpected restore prefix %q", prefix)
	}
	for _, invalid := range []string{"", "../other", "nested/name", ".", ".."} {
		if _, err := restoreObjectPrefix("hypercdr/clusters/cluster-a", invalid); err == nil {
			t.Fatalf("expected restore name %q to be rejected", invalid)
		}
	}
}

func TestDynamicManifestApplierAppliesConfigMap(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	applier := NewDynamicManifestApplierWithClient(client)
	_, err := applier.ApplyManifest(context.Background(), Manifest{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "cm",
			"namespace": "default",
		},
		"data": map[string]any{
			"resource-modifiers.yaml": "version: v1\n",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDynamicManifestApplierRejectsUnsupportedKind(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	applier := NewDynamicManifestApplierWithClient(client)
	_, err := applier.ApplyManifest(context.Background(), Manifest{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":      "sa",
			"namespace": "default",
		},
	})
	if err == nil {
		t.Fatal("expected unsupported kind error")
	}
}

func TestDynamicManifestApplierRejectsNamespaceManifest(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	applier := NewDynamicManifestApplierWithClient(client)
	_, err := applier.ApplyManifest(context.Background(), Manifest{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": "target",
		},
	})
	if err == nil {
		t.Fatal("expected Namespace to remain outside the generic manifest allowlist")
	}
}

func TestDynamicManifestApplierReplacesNamespaceThroughDedicatedAPI(t *testing.T) {
	namespaceGVR := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	oldNamespace := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": "target",
			"uid":  "old-uid",
		},
		"status": map[string]any{"phase": "Active"},
	}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), oldNamespace)
	client.PrependReactor("create", "namespaces", func(action clienttesting.Action) (bool, runtime.Object, error) {
		createAction := action.(clienttesting.CreateAction)
		object := createAction.GetObject().(*unstructured.Unstructured)
		object.SetUID(types.UID("new-uid"))
		if err := unstructured.SetNestedField(object.Object, "Active", "status", "phase"); err != nil {
			t.Fatal(err)
		}
		return false, nil, nil
	})
	applier := NewDynamicManifestApplierWithClient(client)

	if err := applier.ReplaceNamespaceAndWait(context.Background(), "target"); err != nil {
		t.Fatal(err)
	}
	recreated, err := client.Resource(namespaceGVR).Get(context.Background(), "target", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if recreated.GetUID() != types.UID("new-uid") {
		t.Fatalf("recreated namespace UID = %q, want new-uid", recreated.GetUID())
	}
	phase, _, _ := unstructured.NestedString(recreated.Object, "status", "phase")
	if phase != "Active" || recreated.GetDeletionTimestamp() != nil {
		t.Fatalf("recreated namespace is not stable and Active: %#v", recreated.Object)
	}
}

func TestDynamicManifestApplierReadsVeleroStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		{Group: "velero.io", Version: "v1", Resource: "backups"}: "BackupList",
	})
	applier := NewDynamicManifestApplierWithClient(client)
	manifest := Manifest{
		"apiVersion": "velero.io/v1",
		"kind":       "Backup",
		"metadata": map[string]any{
			"name":      "backup-a",
			"namespace": "hypercdr-agent",
		},
		"spec": map[string]any{
			"storageLocation": "default",
		},
		"status": map[string]any{
			"phase":    "Completed",
			"errors":   int64(0),
			"warnings": int64(1),
		},
	}
	if _, err := applier.ApplyManifest(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	status, err := applier.GetManifestStatus(context.Background(), AppliedObject{
		APIVersion: "velero.io/v1",
		Kind:       "Backup",
		Namespace:  "hypercdr-agent",
		Name:       "backup-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != "Completed" || status.Errors != 0 || status.Warnings != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}
}
