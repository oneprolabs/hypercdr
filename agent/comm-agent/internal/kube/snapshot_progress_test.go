package kube

import (
	"context"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestSnapshotProgressDiscoveryAndPreparedEvidence(t *testing.T) {
	backup := &unstructured.Unstructured{}
	backup.SetAnnotations(map[string]string{protocol.SnapshotInventoryAnnotation: `{"namespaces":["app","db"],"volumes":[{"namespace":"app","name":"data","uid":"one","snapshotClass":"csi"},{"namespace":"db","name":"data","uid":"two","snapshotClass":"csi"}]}`})
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	progress := VolumeProgress{Items: []VolumeProgressItem{{Kind: "DataUpload", Name: "upload", Namespace: "app", Volume: "data", Phase: "Prepared"}}}
	NewDynamicManifestApplierWithClient(client).enrichSnapshotProgress(context.Background(), "agent", backup, &progress)
	if !progress.DiscoveryComplete || progress.DiscoveryCount != 2 || len(progress.Items) != 2 {
		t.Fatalf("inventory lost: %+v", progress)
	}
	if !progress.Items[0].SnapshotReady || !progress.Items[0].TemporaryVolumeReady || progress.Items[1].SnapshotReady {
		t.Fatalf("fabricated preparation: %+v", progress.Items)
	}
	if len(client.Actions()) != 0 {
		t.Fatal("prepared evidence should not depend on already-cleaned snapshots")
	}
}

func TestSnapshotProgressInvalidInventoryDoesNotCompleteDiscovery(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	progress := VolumeProgress{}
	NewDynamicManifestApplierWithClient(client).enrichSnapshotProgress(context.Background(), "agent", &unstructured.Unstructured{}, &progress)
	if progress.DiscoveryComplete {
		t.Fatal("invalid inventory accepted")
	}
}

func TestSnapshotProgressReadsReadinessWithoutClaimingMount(t *testing.T) {
	for _, ready := range []bool{false, true} {
		backup := &unstructured.Unstructured{}
		backup.SetAnnotations(map[string]string{protocol.SnapshotInventoryAnnotation: `{"namespaces":["app"],"volumes":[{"namespace":"app","name":"data","uid":"one","snapshotClass":"csi"}]}`})
		upload := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v2alpha1", "kind": "DataUpload", "metadata": map[string]any{"name": "upload", "namespace": "agent"}, "spec": map[string]any{"csiSnapshot": map[string]any{"volumeSnapshot": "snapshot"}}}}
		snapshot := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot", "metadata": map[string]any{"name": "snapshot", "namespace": "app"}, "status": map[string]any{"readyToUse": ready}}}
		if ready {
			snapshot.SetNamespace("agent")
			snapshot.SetName("upload")
		}
		client := fake.NewSimpleDynamicClient(runtime.NewScheme(), upload, snapshot)
		progress := VolumeProgress{Items: []VolumeProgressItem{{Kind: "DataUpload", Name: "upload", Namespace: "app", Volume: "data", Phase: "Accepted"}}}
		NewDynamicManifestApplierWithClient(client).enrichSnapshotProgress(context.Background(), "agent", backup, &progress)
		if progress.Items[0].SnapshotReady != ready || progress.Items[0].TemporaryVolumeReady {
			t.Fatalf("incorrect evidence: %+v", progress.Items[0])
		}
		for _, action := range client.Actions() {
			if action.GetVerb() != "get" {
				t.Fatal("progress collection mutated the cluster")
			}
		}
	}
}
