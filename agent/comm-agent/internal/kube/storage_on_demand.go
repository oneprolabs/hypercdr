package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const storageAccessPolicy = "hypercdr.io/storage-access"

var storageLocationResource = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"}
var repositoryResource = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backuprepositories"}

type ObjectStoragePreparer interface {
	PrepareObjectStorage(context.Context, string, string, string) error
	ConfigureOnDemandStorage(context.Context, string) error
	MaintainObjectStorage(context.Context, string, string) error
}

func (a *DynamicManifestApplier) supportsOnDemandStorage(ctx context.Context, namespace string) (bool, error) {
	deployment, err := a.client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(namespace).Get(ctx, "velero", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	annotations, _, _ := unstructured.NestedStringMap(deployment.Object, "spec", "template", "metadata", "annotations")
	containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	for _, value := range containers {
		container, ok := value.(map[string]any)
		if ok && container["name"] == "velero" {
			return annotations["hypercdr.io/storage-on-demand-supported"] == "v1" && annotations["hypercdr.io/storage-on-demand-image"] == container["image"], nil
		}
	}
	return false, nil
}

func (a *DynamicManifestApplier) ConfigureOnDemandStorage(ctx context.Context, namespace string) error {
	supported, err := a.supportsOnDemandStorage(ctx, namespace)
	if err != nil || !supported {
		return err
	}
	locations, err := a.client.Resource(storageLocationResource).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, location := range locations.Items {
		if location.GetLabels()["hypercdr.io/repository-id"] == "" && location.GetAnnotations()[storageAccessPolicy] != "on-demand-v1" {
			continue
		}
		if err := a.configureOnDemandLocation(ctx, namespace, location.GetName()); err != nil {
			return err
		}
	}
	return nil
}

func (a *DynamicManifestApplier) configureOnDemandLocation(ctx context.Context, namespace, name string) error {
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{storageAccessPolicy: "on-demand-v1"}}, "spec": map[string]any{"validationFrequency": "0s", "backupSyncPeriod": "0s"}})
	_, err := a.client.Resource(storageLocationResource).Namespace(namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (a *DynamicManifestApplier) requestStorageOperation(ctx context.Context, namespace string, resource schema.GroupVersionResource, name, operation string, extra map[string]string) error {
	requested := time.Now().UTC().Format(time.RFC3339Nano)
	annotations := map[string]string{"hypercdr.io/" + operation + "-request": requested}
	for key, value := range extra {
		annotations[key] = value
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": annotations}})
	objects := a.client.Resource(resource).Namespace(namespace)
	if _, err := objects.Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return err
	}
	return wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		object, err := objects.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		annotations := object.GetAnnotations()
		if annotations["hypercdr.io/"+operation+"-completed"] != requested {
			return false, nil
		}
		if annotations["hypercdr.io/"+operation+"-result"] != "succeeded" {
			return false, fmt.Errorf("object storage %s failed for %s; check Velero task logs", operation, name)
		}
		return true, nil
	})
}

func (a *DynamicManifestApplier) PrepareObjectStorage(ctx context.Context, namespace, name, backup string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	value, _ := a.storageRequests.LoadOrStore(namespace+"/"+name, make(chan struct{}, 1))
	lock := value.(chan struct{})
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-ctx.Done():
		return ctx.Err()
	}
	location, err := a.client.Resource(storageLocationResource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	supported, err := a.supportsOnDemandStorage(ctx, namespace)
	if err != nil {
		return err
	}
	if !supported {
		if location.GetAnnotations()[storageAccessPolicy] == "on-demand-v1" {
			return fmt.Errorf("installed Velero does not support on-demand storage access")
		}
		return nil
	}
	if err := a.configureOnDemandLocation(ctx, namespace, name); err != nil {
		return err
	}
	if err := a.requestStorageOperation(ctx, namespace, storageLocationResource, name, "validate", nil); err != nil {
		return err
	}
	if backup != "" {
		if err := a.requestStorageOperation(ctx, namespace, storageLocationResource, name, "sync", map[string]string{"hypercdr.io/sync-backup": backup}); err != nil {
			return err
		}
	}
	repositories, err := a.client.Resource(repositoryResource).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, repo := range repositories.Items {
		location, _, _ := unstructured.NestedString(repo.Object, "spec", "backupStorageLocation")
		phase, _, _ := unstructured.NestedString(repo.Object, "status", "phase")
		if location == name && phase != "Ready" {
			patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{"hypercdr.io/open-request": time.Now().UTC().Format(time.RFC3339Nano)}}})
			if _, err := a.client.Resource(repositoryResource).Namespace(namespace).Patch(ctx, repo.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *DynamicManifestApplier) MaintainObjectStorage(ctx context.Context, namespace, name string) error {
	value, _ := a.storageRequests.LoadOrStore("maintain/"+namespace+"/"+name, make(chan struct{}, 1))
	lock := value.(chan struct{})
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-ctx.Done():
		return ctx.Err()
	}
	location, err := a.client.Resource(storageLocationResource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || location.GetAnnotations()[storageAccessPolicy] != "on-demand-v1" {
		return err
	}
	supported, err := a.supportsOnDemandStorage(ctx, namespace)
	if err != nil {
		return err
	}
	if !supported {
		return fmt.Errorf("installed Velero does not support on-demand maintenance")
	}
	backups, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, backup := range backups.Items {
		storage, _, _ := unstructured.NestedString(backup.Object, "spec", "storageLocation")
		phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
		if storage == name && phase != "Completed" && phase != "PartiallyFailed" && phase != "Failed" && phase != "FailedValidation" {
			return nil
		}
	}
	restores, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, restore := range restores.Items {
		phase, _, _ := unstructured.NestedString(restore.Object, "status", "phase")
		if phase != "Completed" && phase != "PartiallyFailed" && phase != "Failed" && phase != "FailedValidation" {
			return nil
		}
	}
	repositories, err := a.client.Resource(repositoryResource).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, repo := range repositories.Items {
		storage, _, _ := unstructured.NestedString(repo.Object, "spec", "backupStorageLocation")
		if storage != name {
			continue
		}
		frequency, _, _ := unstructured.NestedString(repo.Object, "spec", "maintenanceFrequency")
		period, err := time.ParseDuration(frequency)
		if err != nil || period <= 0 {
			period = time.Hour
		}
		last, _, _ := unstructured.NestedString(repo.Object, "status", "lastMaintenanceTime")
		when, _ := time.Parse(time.RFC3339Nano, last)
		if !when.IsZero() && time.Since(when) < period {
			continue
		}
		if err := a.requestStorageOperation(ctx, namespace, repositoryResource, repo.GetName(), "maintain", nil); err != nil {
			return err
		}
	}
	return nil
}
