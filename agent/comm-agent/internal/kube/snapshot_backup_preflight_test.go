package kube

import (
	"context"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestOrdinarySnapshotBackupPreflightFailsClosed(t *testing.T) {
	classes := schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
	for _, scenario := range []string{"valid", "empty", "missing-class", "non-csi", "changed-uid", "mixed-fsb", "forbidden", "wildcard"} {
		t.Run(scenario, func(t *testing.T) {
			claim := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"namespace": "isolated", "name": "data", "uid": "pvc-uid"}, "spec": map[string]any{"volumeName": "pv"}, "status": map[string]any{"phase": "Bound"}}}
			volume := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolume", "metadata": map[string]any{"name": "pv"}, "spec": map[string]any{"csi": map[string]any{"driver": "test.csi"}, "claimRef": map[string]any{"namespace": "isolated", "name": "data", "uid": "pvc-uid"}}}}
			class := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotClass", "metadata": map[string]any{"name": "snapshot-class"}, "driver": "test.csi"}}
			objects := []runtime.Object{claim, volume, class}
			switch scenario {
			case "empty":
				objects = nil
			case "missing-class":
				objects = []runtime.Object{claim, volume}
			case "non-csi":
				unstructured.RemoveNestedField(volume.Object, "spec", "csi")
			case "changed-uid":
				_ = unstructured.SetNestedField(volume.Object, "old-pvc", "spec", "claimRef", "uid")
			case "mixed-fsb":
				objects = append(objects, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"namespace": "isolated", "name": "writer", "annotations": map[string]any{"backup.velero.io/backup-volumes": "data"}}}})
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{classes: "VolumeSnapshotClassList", {Version: "v1", Resource: "pods"}: "PodList", {Version: "v1", Resource: "persistentvolumeclaims"}: "PersistentVolumeClaimList"}, objects...)
			if scenario == "forbidden" {
				client.PrependReactor("list", "persistentvolumeclaims", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumeclaims"}, "", fmt.Errorf("denied"))
				})
			}
			namespaces := []string{"isolated"}
			if scenario == "wildcard" {
				namespaces = []string{"*"}
			}
			annotations, err := NewDynamicManifestApplierWithClient(client).PrepareSnapshotBackup(context.Background(), namespaces)
			if (err == nil) != (scenario == "valid" || scenario == "empty") {
				t.Fatalf("unexpected preflight result: %v, %v", annotations, err)
			}
			if scenario == "valid" && annotations["velero.io/csi-volumesnapshot-class_test.csi"] != "snapshot-class" {
				t.Fatal("exact snapshot class was not selected")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatal("preflight mutated source resources")
				}
			}
		})
	}
}
