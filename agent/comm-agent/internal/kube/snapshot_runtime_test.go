package kube

import (
	"context"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestRestoreBackupRequiresDataMoverRuntimeButNotSnapshotStorage(t *testing.T) {
	for _, scenario := range []string{"file-level", "disabled", "enabled"} {
		t.Run(scenario, func(t *testing.T) {
			backup := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "metadata": map[string]any{"name": "backup", "namespace": "agent"}, "spec": map[string]any{"snapshotMoveData": scenario != "file-level"}, "status": map[string]any{"phase": "Completed"}}}
			args := []any{"server"}
			if scenario == "enabled" {
				args = append(args, "--features=EnableCSI")
			}
			deployment := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "velero", "namespace": "agent", "generation": int64(1)}, "spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "velero", "args": args}}}}}, "status": map[string]any{"availableReplicas": int64(1), "updatedReplicas": int64(1), "observedGeneration": int64(1)}}}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList"}, backup, deployment)
			err := NewDynamicManifestApplierWithClient(client).WaitForVeleroBackup(context.Background(), "agent", "backup", time.Second)
			if (err != nil) != (scenario == "disabled") {
				t.Fatalf("unexpected restore admission: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatal("restore admission mutated the target")
				}
				if action.GetResource().Group == "snapshot.storage.k8s.io" {
					t.Fatal("data-mover restore must not require snapshot storage APIs")
				}
			}
		})
	}
}

func TestEnableVeleroCSIPreservesFlags(t *testing.T) {
	for _, args := range [][]string{{"server"}, {"server", "--features="}, {"server", "--features=Other"}, {"server", "--features", "Other"}, {"server", "--features=EnableCSI"}} {
		result := enableVeleroCSI(append([]string(nil), args...))
		if !reflect.DeepEqual(result, enableVeleroCSI(append([]string(nil), result...))) {
			t.Fatalf("CSI configuration is not idempotent: %v", result)
		}
		if result[0] != "server" {
			t.Fatal("server command was discarded")
		}
	}
}

func TestSnapshotRuntimeRequiresEnabledReadyVelero(t *testing.T) {
	for _, scenario := range []string{"enabled", "split-flags", "disabled", "not-ready", "stale-rollout", "partial-rollout", "missing", "wrong-container", "deleting"} {
		t.Run(scenario, func(t *testing.T) {
			deployment := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "velero", "namespace": "agent", "generation": int64(2)}, "spec": map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "velero", "args": []any{"server", "--features=EnableCSI,Other"}}}}}}, "status": map[string]any{"availableReplicas": int64(1), "updatedReplicas": int64(1), "observedGeneration": int64(2)}}}
			containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
			container := containers[0].(map[string]any)
			switch scenario {
			case "split-flags":
				container["args"] = []any{"server", "--features", "Other,EnableCSI"}
			case "disabled":
				container["args"] = []any{"server", "--features=Other"}
			case "wrong-container":
				container["name"] = "unrelated"
			case "not-ready":
				_ = unstructured.SetNestedField(deployment.Object, int64(0), "status", "availableReplicas")
			case "stale-rollout":
				_ = unstructured.SetNestedField(deployment.Object, int64(1), "status", "observedGeneration")
			case "partial-rollout":
				_ = unstructured.SetNestedField(deployment.Object, int64(2), "spec", "replicas")
			case "deleting":
				stamp := metav1.Now()
				deployment.SetDeletionTimestamp(&stamp)
			}
			_ = unstructured.SetNestedSlice(deployment.Object, containers, "spec", "template", "spec", "containers")
			objects := []runtime.Object{deployment}
			if scenario == "missing" {
				objects = nil
			}
			client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList"}, objects...)
			err := NewDynamicManifestApplierWithClient(client).CheckSnapshotRuntime(context.Background(), "agent")
			if (err == nil) != (scenario == "enabled" || scenario == "split-flags") {
				t.Fatalf("unexpected runtime admission: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "list" {
					t.Fatal("runtime admission mutated cluster configuration")
				}
			}
		})
	}
}
