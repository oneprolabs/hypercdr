package kube

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
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
