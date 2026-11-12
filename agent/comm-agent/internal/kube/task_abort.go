package kube

import (
	"context"
	"fmt"
	"slices"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (a *DynamicManifestApplier) StopBackupDeletionRequests(ctx context.Context, namespace string, backupNames []string) error {
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "deletebackuprequests"}).Namespace(namespace)
	requests, err := resource.List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, request := range requests.Items {
		backupName, _, _ := unstructured.NestedString(request.Object, "spec", "backupName")
		if backupName == "" || !slices.Contains(backupNames, backupName) {
			continue
		}
		uid := types.UID(request.GetUID())
		if err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			current, err := resource.Get(ctx, request.GetName(), metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			if current.GetUID() != uid {
				return false, fmt.Errorf("deletion request %q was replaced; retry force abort", request.GetName())
			}
			phase, _, _ := unstructured.NestedString(current.Object, "status", "phase")
			return phase != "InProgress", nil
		}); err != nil {
			return fmt.Errorf("Velero is still processing deletion of backup %q: %w", backupName, err)
		}
		if err := resource.Delete(ctx, request.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
