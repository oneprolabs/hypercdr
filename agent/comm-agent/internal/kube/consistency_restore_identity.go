package kube

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var consistencyRestores = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}

func consistencyRestoreObject(manifest Manifest) (*unstructured.Unstructured, error) {
	object := &unstructured.Unstructured{Object: map[string]any(manifest)}
	if object.GetKind() != "Restore" || object.GetAPIVersion() != "velero.io/v1" || object.GetName() == "" || object.GetNamespace() == "" || object.GetLabels()["hypercdr.io/task-id"] == "" || object.GetLabels()["hypercdr.io/command-id"] == "" {
		return nil, fmt.Errorf("strict restore requires exact task and command identities")
	}
	return object, nil
}

func (applier *DynamicManifestApplier) FindConsistencyRestore(ctx context.Context, manifest Manifest) (bool, error) {
	object, err := consistencyRestoreObject(manifest)
	if err != nil {
		return false, err
	}
	current, err := applier.client.Resource(consistencyRestores).Namespace(object.GetNamespace()).Get(ctx, object.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current.GetDeletionTimestamp() != nil || current.GetLabels()["hypercdr.io/task-id"] != object.GetLabels()["hypercdr.io/task-id"] || current.GetLabels()["hypercdr.io/command-id"] != object.GetLabels()["hypercdr.io/command-id"] {
		return false, fmt.Errorf("strict restore name belongs to another task or is deleting")
	}
	spec, _, _ := unstructured.NestedMap(object.Object, "spec")
	currentSpec, _, _ := unstructured.NestedMap(current.Object, "spec")
	for key, value := range spec {
		if !reflect.DeepEqual(value, currentSpec[key]) {
			return false, fmt.Errorf("strict restore replay changed spec field %s", key)
		}
	}
	return true, nil
}

func (applier *DynamicManifestApplier) CreateConsistencyRestore(ctx context.Context, manifest Manifest) error {
	object, err := consistencyRestoreObject(manifest)
	if err != nil {
		return err
	}
	_, err = applier.client.Resource(consistencyRestores).Namespace(object.GetNamespace()).Create(ctx, object, metav1.CreateOptions{})
	return err
}
