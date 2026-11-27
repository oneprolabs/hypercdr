package kube

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestSnapshotClassResolutionRejectsMultipleClassesForOneDriver(t *testing.T) {
	var objects []runtime.Object
	var volumes []SnapshotVolumeRequirement
	for _, name := range []string{"first", "second"} {
		objects = append(objects,
			&unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": name, "namespace": "app", "uid": name}, "spec": map[string]any{"volumeName": name}, "status": map[string]any{"phase": "Bound"}}},
			&unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolume", "metadata": map[string]any{"name": name}, "spec": map[string]any{"csi": map[string]any{"driver": "driver.test"}, "claimRef": map[string]any{"uid": name}}}},
			&unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotClass", "metadata": map[string]any{"name": name}, "driver": "driver.test"}},
		)
		volumes = append(volumes, SnapshotVolumeRequirement{Namespace: "app", Name: name, UID: name, Class: name})
	}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}: "VolumeSnapshotClassList"}, objects...)
	annotations, err := NewDynamicManifestApplierWithClient(client).ResolveConsistencySnapshotClassAnnotations(context.Background(), volumes)
	if err == nil || !strings.Contains(err.Error(), "multiple snapshot classes") || annotations != nil {
		t.Fatalf("conflicting per-driver selection accepted: %v %v", annotations, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatalf("class resolution mutated cluster: %s", action.GetVerb())
		}
	}
}

func TestSnapshotPreflightRejectsIncompleteOrChangedVolumeInventory(t *testing.T) {
	for _, scenario := range []string{"valid", "empty", "missing-class", "wrong-driver", "changed-uid", "unbound", "wrong-owner", "duplicate", "cross-namespace", "pvc-class-override", "deleting-class"} {
		t.Run(scenario, func(t *testing.T) {
			pvc := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": "data", "namespace": "app", "uid": "pvc-original"}, "spec": map[string]any{"volumeName": "pv-data"}, "status": map[string]any{"phase": "Bound"}}}
			pv := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "PersistentVolume", "metadata": map[string]any{"name": "pv-data"}, "spec": map[string]any{"csi": map[string]any{"driver": "driver.test"}, "claimRef": map[string]any{"uid": "pvc-original"}}}}
			class := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotClass", "metadata": map[string]any{"name": "snapshot-test"}, "driver": "driver.test"}}
			volumes := []SnapshotVolumeRequirement{{Namespace: "app", Name: "data", UID: "pvc-original", Class: "snapshot-test"}}
			switch scenario {
			case "pvc-class-override":
				pvc.SetAnnotations(map[string]string{"velero.io/csi-volumesnapshot-class": "other-class"})
			case "deleting-class":
				deleting := metav1.NewTime(time.Now())
				class.SetDeletionTimestamp(&deleting)
			case "empty":
				volumes = nil
			case "missing-class":
				volumes[0].Class = "absent"
			case "wrong-driver":
				class.Object["driver"] = "different-driver"
			case "changed-uid":
				volumes[0].UID = "replacement-pvc"
			case "unbound":
				_ = unstructured.SetNestedField(pvc.Object, "Pending", "status", "phase")
			case "wrong-owner":
				_ = unstructured.SetNestedField(pv.Object, "other-pvc", "spec", "claimRef", "uid")
			case "duplicate":
				volumes = append(volumes, volumes[0])
			case "cross-namespace":
				volumes[0].Namespace = "other-app"
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}: "VolumeSnapshotClassList"}, pvc, pv, class)
			err := NewDynamicManifestApplierWithClient(client).CheckSnapshotPrerequisites(context.Background(), volumes)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("scenario %s returned %v", scenario, err)
			}
			if scenario == "valid" {
				annotations, err := NewDynamicManifestApplierWithClient(client).ResolveConsistencySnapshotClassAnnotations(context.Background(), volumes)
				if err != nil || annotations["velero.io/csi-volumesnapshot-class_driver.test"] != "snapshot-test" {
					t.Fatalf("explicit class resolution failed: %v %v", annotations, err)
				}
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatalf("preflight mutated cluster: %s", action.GetVerb())
				}
			}
		})
	}
}
