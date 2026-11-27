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
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestSnapshotCapabilityInventoryDoesNotHideReadFailures(t *testing.T) {
	snapshots := schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
	issuers := schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "clusterissuers"}
	for _, scenario := range []string{"valid", "absent-api", "forbidden", "unavailable", "no-client", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			class := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshotClass", "metadata": map[string]any{"name": "test-snapshots"}, "driver": "test.csi"}}
			if scenario == "deleting" {
				stamp := metav1.Now()
				class.SetDeletionTimestamp(&stamp)
			}
			dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{snapshots: "VolumeSnapshotClassList", issuers: "ClusterIssuerList"}, class)
			dynamicClient.PrependReactor("list", "volumesnapshotclasses", func(clienttesting.Action) (bool, runtime.Object, error) {
				switch scenario {
				case "absent-api":
					return true, nil, apierrors.NewNotFound(snapshots.GroupResource(), "")
				case "forbidden":
					return true, nil, apierrors.NewForbidden(snapshots.GroupResource(), "", fmt.Errorf("not allowed"))
				case "unavailable":
					return true, nil, apierrors.NewServiceUnavailable("API unavailable")
				}
				return false, nil, nil
			})
			reader := NewKubernetesClusterReaderWithClients(fake.NewSimpleClientset(), dynamicClient, nil)
			if scenario == "no-client" {
				reader.dynamic = nil
			}
			items, complete := reader.readNamedCapabilities(context.Background())
			expectedComplete := scenario == "valid" || scenario == "absent-api" || scenario == "deleting"
			if complete != expectedComplete {
				t.Fatalf("scenario=%s complete=%v", scenario, complete)
			}
			if (len(items) == 1) != (scenario == "valid") {
				t.Fatalf("unexpected snapshot capability: %+v", items)
			}
		})
	}
}
