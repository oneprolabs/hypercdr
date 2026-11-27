package kube

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestConsistencyRestoreIdentityRejectsCollisionAndNeverUpdatesExistingRestore(t *testing.T) {
	for _, scenario := range []string{"absent", "same-task", "other-task", "other-command", "changed-spec"} {
		t.Run(scenario, func(t *testing.T) {
			object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "velero.io/v1", "kind": "Restore", "metadata": map[string]any{"name": "restore", "namespace": "agent", "labels": map[string]any{"hypercdr.io/task-id": "task", "hypercdr.io/command-id": "command"}}, "spec": map[string]any{"backupName": "backup"}}}
			manifest := Manifest(object.DeepCopy().Object)
			switch scenario {
			case "other-task":
				labels := object.GetLabels()
				labels["hypercdr.io/task-id"] = "other"
				object.SetLabels(labels)
			case "other-command":
				labels := object.GetLabels()
				labels["hypercdr.io/command-id"] = "other"
				object.SetLabels(labels)
			case "changed-spec":
				_ = unstructured.SetNestedField(object.Object, "other", "spec", "backupName")
			}
			var objects []runtime.Object
			if scenario != "absent" {
				objects = append(objects, object)
			}
			client := fake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
			applier := NewDynamicManifestApplierWithClient(client)
			found, err := applier.FindConsistencyRestore(context.Background(), manifest)
			if (err == nil) != (scenario == "absent" || scenario == "same-task") || found != (scenario == "same-task") {
				t.Fatalf("unexpected identity acceptance: found=%v err=%v", found, err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("identity lookup mutated a Restore")
				}
			}
			err = applier.CreateConsistencyRestore(context.Background(), manifest)
			if scenario == "absent" && err != nil {
				t.Fatal(err)
			}
			if scenario != "absent" && !apierrors.IsAlreadyExists(err) {
				t.Fatalf("existing Restore was not protected by atomic create: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "patch" || action.GetVerb() == "update" {
					t.Fatal("strict restore overwrote an existing object")
				}
			}
		})
	}
}
