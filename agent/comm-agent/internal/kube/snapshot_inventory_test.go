package kube

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestSnapshotVolumeInventoryIsReadOnlyAndIdentityScoped(t *testing.T) {
	resource := schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
	for _, scenario := range []struct{ name, status string }{
		{"configured", "configured"}, {"unbound", "unknown"}, {"stale-owner", "unknown"},
		{"non-csi", "unsupported"}, {"no-class", "unsupported"}, {"wrong-override", "unsupported"},
		{"forbidden", "error"}, {"missing-api", "unsupported"}, {"ambiguous", "unknown"}, {"pv-read-failed", "error"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "isolated", UID: "current-pvc"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
			storageClass := "existing-csi-storage"
			claim.Spec.StorageClassName = &storageClass
			volume := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: claim.Namespace, Name: claim.Name, UID: claim.UID}, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "test.csi"}}}}
			class := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotClass", "metadata": map[string]any{"name": "snapshots"}, "driver": "test.csi"}}
			objects := []runtime.Object{class}
			switch scenario.name {
			case "unbound":
				claim.Status.Phase = corev1.ClaimPending
			case "stale-owner":
				volume.Spec.ClaimRef.UID = "previous-pvc"
			case "non-csi":
				volume.Spec.CSI = nil
			case "no-class":
				objects = nil
			case "wrong-override":
				claim.Annotations = map[string]string{"velero.io/csi-volumesnapshot-class": "other"}
			case "ambiguous":
				second := class.DeepCopy()
				second.SetName("second")
				objects = append(objects, second)
			}
			dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{resource: "VolumeSnapshotClassList"}, objects...)
			dynamicClient.PrependReactor("list", resource.Resource, func(clienttesting.Action) (bool, runtime.Object, error) {
				if scenario.name == "forbidden" {
					return true, nil, apierrors.NewForbidden(resource.GroupResource(), "", fmt.Errorf("denied"))
				}
				if scenario.name == "missing-api" {
					return true, nil, apierrors.NewNotFound(resource.GroupResource(), "")
				}
				return false, nil, nil
			})
			client := fake.NewSimpleClientset(claim, volume)
			if scenario.name == "pv-read-failed" {
				client.PrependReactor("get", "persistentvolumes", func(clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumes"}, "pv", fmt.Errorf("denied"))
				})
			}
			reader := NewKubernetesClusterReaderWithClients(client, dynamicClient, nil)
			items, err := reader.ReadSnapshotVolumePrerequisites(context.Background(), claim.Namespace)
			if err != nil || len(items) != 1 {
				t.Fatalf("inventory: %+v, %v", items, err)
			}
			if items[0].Fields["storageClass"] != storageClass {
				t.Fatalf("storage class missing from volume inventory: %+v", items[0])
			}
			if items[0].Fields["status"] != scenario.status || items[0].Fields["uid"] != string(claim.UID) || items[0].Fields["namespace"] != claim.Namespace {
				t.Fatalf("wrong status or identity: %+v", items[0])
			}
			expectedCodes := map[string]string{"configured": "prerequisites-configured", "unbound": "pvc-not-ready", "stale-owner": "pv-identity-mismatch", "non-csi": "non-csi-volume", "no-class": "snapshot-class-missing", "wrong-override": "snapshot-class-missing", "forbidden": "snapshot-class-read-failed", "missing-api": "snapshot-api-missing", "ambiguous": "snapshot-class-ambiguous", "pv-read-failed": "pv-read-failed"}
			if items[0].Fields["reasonCode"] != expectedCodes[scenario.name] {
				t.Fatalf("wrong actionable reason code: %+v", items[0])
			}
			for _, action := range append(client.Actions(), dynamicClient.Actions()...) {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatalf("preflight mutated cluster: %v", action)
				}
			}
			if _, err := reader.ReadSnapshotVolumePrerequisites(context.Background(), ""); err == nil {
				t.Fatal("unscoped inventory was allowed")
			}
		})
	}
}
