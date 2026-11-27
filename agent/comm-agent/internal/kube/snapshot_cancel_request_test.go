package kube

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestSnapshotCancellationRequestRequiresExactOwnership(t *testing.T) {
	for _, scenario := range []string{"valid", "foreign", "wrong-task", "wrong-command", "completed", "missing", "file-level"} {
		t.Run(scenario, func(t *testing.T) {
			backup := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "Backup", "metadata": map[string]any{"name": "backup", "namespace": "agent", "uid": "backup-uid", "resourceVersion": "1", "labels": map[string]any{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/task-id": "task", "hypercdr.io/command-id": "command"}}, "spec": map[string]any{"snapshotMoveData": true}, "status": map[string]any{"phase": "InProgress"}}}
			labels := backup.GetLabels()
			switch scenario {
			case "foreign":
				delete(labels, "hypercdr.io/managed-by")
			case "wrong-task":
				labels["hypercdr.io/task-id"] = "other"
			case "wrong-command":
				labels["hypercdr.io/command-id"] = "other"
			case "completed":
				_ = unstructured.SetNestedField(backup.Object, "Completed", "status", "phase")
			case "file-level":
				_ = unstructured.SetNestedField(backup.Object, false, "spec", "snapshotMoveData")
			}
			backup.SetLabels(labels)
			objects := []runtime.Object{backup}
			if scenario == "missing" {
				objects = nil
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			err := NewDynamicManifestApplierWithClient(client).RequestSnapshotBackupCancellation(context.Background(), "agent", "backup", "task", "command")
			if (err == nil) != (scenario == "valid" || scenario == "missing" || scenario == "completed") {
				t.Fatalf("unexpected cancellation admission: %v", err)
			}
			if scenario != "missing" {
				current, err := client.Resource(consistencyBackups).Namespace("agent").Get(context.Background(), "backup", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if (current.GetAnnotations()["hypercdr.io/snapshot-cancel-task-id"] == "task") != (scenario == "valid") {
					t.Fatal("cancellation changed a foreign or completed backup")
				}
			}
		})
	}
}
