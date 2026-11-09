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
	ktesting "k8s.io/client-go/testing"
)

func TestOnDemandStoragePreparationRequiresFreshAcknowledgement(t *testing.T) {
	for _, failure := range []string{"", "validate", "sync", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			image := "velero:test-on-demand-cache-v1"
			location := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "BackupStorageLocation", "metadata": map[string]any{"name": "store", "namespace": "agent"}}}
			deployment := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "velero", "namespace": "agent"}, "spec": map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]any{"hypercdr.io/storage-on-demand-supported": "v1", "hypercdr.io/storage-on-demand-image": image}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "velero", "image": image}}}}}}}
			cli := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{storageLocationResource: "BackupStorageLocationList", repositoryResource: "BackupRepositoryList"}, location, deployment)
			var operations []string
			cli.PrependReactor("get", "backupstoragelocations", func(action ktesting.Action) (bool, runtime.Object, error) {
				object, err := cli.Tracker().Get(storageLocationResource, "agent", "store")
				if err != nil {
					return true, nil, err
				}
				current := object.DeepCopyObject().(*unstructured.Unstructured)
				annotations := current.GetAnnotations()
				for _, operation := range []string{"validate", "sync"} {
					if annotations["hypercdr.io/"+operation+"-request"] != "" && failure != "timeout" {
						if operation == "sync" && annotations["hypercdr.io/sync-backup"] != "selected-backup" {
							t.Fatal("sync was not scoped to selected backup")
						}
						annotations["hypercdr.io/"+operation+"-completed"] = annotations["hypercdr.io/"+operation+"-request"]
						annotations["hypercdr.io/"+operation+"-result"] = "succeeded"
						if failure == operation {
							annotations["hypercdr.io/"+operation+"-result"] = "failed"
						}
						operations = append(operations, operation)
					}
				}
				current.SetAnnotations(annotations)
				return true, current, nil
			})
			runtimeManager := NewDynamicManifestApplierWithClient(cli)
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			err := runtimeManager.PrepareObjectStorage(ctx, "agent", "store", "selected-backup")
			if failure == "" && err != nil {
				t.Fatal(err)
			}
			if failure != "" && err == nil {
				t.Fatal("failed or unacknowledged preflight was accepted")
			}
			if failure == "validate" && strings.Contains(strings.Join(operations, ","), "sync") {
				t.Fatal("sync started after validation failed")
			}
			stored, err := cli.Resource(storageLocationResource).Namespace("agent").Get(t.Context(), "store", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			period, _, _ := unstructured.NestedString(stored.Object, "spec", "backupSyncPeriod")
			if period != "0s" {
				t.Fatal("periodic sync remained enabled")
			}
		})
	}
}

func TestOnDemandStorageRejectsDowngradedManagedLocation(t *testing.T) {
	location := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "BackupStorageLocation", "metadata": map[string]any{"name": "store", "namespace": "agent", "annotations": map[string]any{storageAccessPolicy: "on-demand-v1"}}}}
	cli := fake.NewSimpleDynamicClient(runtime.NewScheme(), location)
	applier := NewDynamicManifestApplierWithClient(cli)
	err := applier.PrepareObjectStorage(t.Context(), "agent", "store", "")
	if err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("managed storage silently accepted unsupported Velero: %v", err)
	}
	for _, action := range cli.Actions() {
		if action.GetVerb() == "patch" {
			t.Fatal("unsupported installation was modified")
		}
	}
	location.SetAnnotations(nil)
	if err := cli.Tracker().Update(storageLocationResource, location, "agent"); err != nil {
		t.Fatal(err)
	}
	if err := applier.PrepareObjectStorage(t.Context(), "agent", "store", ""); err != nil {
		t.Fatalf("legacy unmanaged storage compatibility failed: %v", err)
	}
}
