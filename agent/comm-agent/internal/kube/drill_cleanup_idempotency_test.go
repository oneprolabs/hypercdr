package kube

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestDrillCleanupAlreadyDeletedResourcesSucceed(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	applier := NewDynamicManifestApplierWithClient(client)
	for attempt := 0; attempt < 2; attempt++ {
		if err := applier.DeleteNamespaceAndWait(context.Background(), "manually-deleted-drill"); err != nil {
			t.Fatalf("absent namespace should count as cleaned: %v", err)
		}
		if err := applier.DeleteObject(context.Background(), AppliedObject{APIVersion: "velero.io/v1", Kind: "Restore", Namespace: "velero", Name: "already-deleted-restore"}); err != nil {
			t.Fatalf("absent restore should count as cleaned: %v", err)
		}
	}
}
