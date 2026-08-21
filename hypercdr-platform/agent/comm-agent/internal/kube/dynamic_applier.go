package kube

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type DynamicManifestApplier struct {
	client dynamic.Interface
}

func NewDynamicManifestApplierWithClient(client dynamic.Interface) *DynamicManifestApplier {
	return &DynamicManifestApplier{client: client}
}

func NewDynamicManifestApplier(kubeconfigPath string) (*DynamicManifestApplier, error) {
	cfg, err := BuildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &DynamicManifestApplier{client: client}, nil
}

func (a *DynamicManifestApplier) ApplyManifest(ctx context.Context, manifest Manifest) (AppliedObject, error) {
	object, err := ObjectFromManifest(manifest)
	if err != nil {
		return AppliedObject{}, err
	}
	gvr, namespaced, err := resourceForObject(object)
	if err != nil {
		return AppliedObject{}, err
	}
	unstructuredObject := &unstructured.Unstructured{Object: map[string]any(manifest)}

	var resource dynamic.ResourceInterface
	if namespaced {
		if object.Namespace == "" {
			return AppliedObject{}, fmt.Errorf("%s %q requires metadata.namespace", object.Kind, object.Name)
		}
		resource = a.client.Resource(gvr).Namespace(object.Namespace)
	} else {
		resource = a.client.Resource(gvr)
	}

	existing, err := resource.Get(ctx, object.Name, v1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := resource.Create(ctx, unstructuredObject, v1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				existing, getErr := resource.Get(ctx, object.Name, v1.GetOptions{})
				if getErr != nil {
					return AppliedObject{}, getErr
				}
				unstructuredObject.SetResourceVersion(existing.GetResourceVersion())
				if _, updateErr := resource.Update(ctx, unstructuredObject, v1.UpdateOptions{}); updateErr != nil {
					return AppliedObject{}, updateErr
				}
				return object, nil
			}
			return AppliedObject{}, err
		}
		return object, nil
	}
	if err != nil {
		return AppliedObject{}, err
	}
	unstructuredObject.SetResourceVersion(existing.GetResourceVersion())
	if _, err := resource.Update(ctx, unstructuredObject, v1.UpdateOptions{}); err != nil {
		return AppliedObject{}, err
	}
	return object, nil
}

func (a *DynamicManifestApplier) DeleteObject(ctx context.Context, object AppliedObject) error {
	gvr, namespaced, err := resourceForObject(object)
	if err != nil {
		return err
	}
	var resource dynamic.ResourceInterface
	if namespaced {
		if object.Namespace == "" {
			return fmt.Errorf("%s %q requires metadata.namespace", object.Kind, object.Name)
		}
		resource = a.client.Resource(gvr).Namespace(object.Namespace)
	} else {
		resource = a.client.Resource(gvr)
	}
	err = resource.Delete(ctx, object.Name, v1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (a *DynamicManifestApplier) ReplaceNamespaceAndWait(ctx context.Context, namespace string) error {
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	resource := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"})
	var oldUID string
	oldNamespace, err := resource.Get(ctx, namespace, v1.GetOptions{})
	if err == nil {
		oldUID = string(oldNamespace.GetUID())
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to inspect target namespace %q: %w", namespace, err)
	}
	err = resource.Delete(ctx, namespace, v1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete target namespace %q: %w", namespace, err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Minute)
	defer timeout.Stop()
	for {
		_, err := resource.Get(ctx, namespace, v1.GetOptions{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for namespace %q to be deleted", namespace)
		case <-ticker.C:
		}
	}

	created, err := resource.Create(ctx, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": namespace},
	}}, v1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to recreate target namespace %q: %w", namespace, err)
	}
	newUID := string(created.GetUID())
	if newUID == "" {
		return fmt.Errorf("recreated target namespace %q has no UID", namespace)
	}
	if oldUID != "" && oldUID == newUID {
		return fmt.Errorf("recreated target namespace %q retained stale UID %q", namespace, newUID)
	}

	// Require two consecutive healthy observations. This prevents a restore from
	// being submitted against a namespace that is still terminating or has only
	// just become visible through the API server.
	stableObservations := 0
	for {
		current, getErr := resource.Get(ctx, namespace, v1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to verify recreated target namespace %q: %w", namespace, getErr)
		}
		currentUID := string(current.GetUID())
		phase, _, _ := unstructured.NestedString(current.Object, "status", "phase")
		healthy := current.GetDeletionTimestamp() == nil && phase == "Active"
		if newUID != "" && currentUID != newUID {
			return fmt.Errorf("recreated target namespace %q changed UID from %q to %q", namespace, newUID, currentUID)
		}
		if oldUID != "" && currentUID == oldUID {
			return fmt.Errorf("recreated target namespace %q still has stale UID %q", namespace, oldUID)
		}
		if healthy {
			stableObservations++
			if stableObservations >= 2 {
				return nil
			}
		} else {
			stableObservations = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for recreated namespace %q to become Active", namespace)
		case <-ticker.C:
		}
	}
}

func (a *DynamicManifestApplier) WaitForVeleroBackup(ctx context.Context, namespace string, name string, timeoutDuration time.Duration) error {
	if namespace == "" {
		return fmt.Errorf("velero namespace is required")
	}
	if name == "" {
		return fmt.Errorf("velero backup name is required")
	}
	if timeoutDuration <= 0 {
		timeoutDuration = 3 * time.Minute
	}
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}).Namespace(namespace)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(timeoutDuration)
	defer timeout.Stop()
	for {
		backup, err := resource.Get(ctx, name, v1.GetOptions{})
		if err == nil {
			phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
			switch phase {
			case "Completed", "PartiallyFailed":
				return nil
			case "Failed", "FailedValidation":
				return fmt.Errorf("Velero backup %q in namespace %q is %s", name, namespace, phase)
			}
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for Velero backup %q in namespace %q to be synced", name, namespace)
		case <-ticker.C:
		}
	}
}

func (a *DynamicManifestApplier) WaitForResourceModifier(ctx context.Context, namespace string, name string, timeoutDuration time.Duration) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("resource modifier namespace and name are required")
	}
	if timeoutDuration <= 0 {
		timeoutDuration = 15 * time.Second
	}
	resource := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace(namespace)
	timeout := time.NewTimer(timeoutDuration)
	defer timeout.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	observed := false
	var observedAt time.Time
	for {
		configMap, err := resource.Get(ctx, name, v1.GetOptions{})
		if err == nil && configMap.GetResourceVersion() != "" {
			if !observed {
				observed = true
				observedAt = time.Now()
			}
			// The API read confirms persistence. A short grace period gives the
			// Velero controller informer time to consume the same add/update event.
			if time.Since(observedAt) >= 2*time.Second {
				return nil
			}
		} else if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for resource modifier ConfigMap %s/%s to propagate", namespace, name)
		case <-ticker.C:
		}
	}
}

func (a *DynamicManifestApplier) WaitForVeleroBackupDeleted(ctx context.Context, namespace string, name string, timeoutDuration time.Duration) error {
	if namespace == "" {
		return fmt.Errorf("velero namespace is required")
	}
	if name == "" {
		return fmt.Errorf("velero backup name is required")
	}
	if timeoutDuration <= 0 {
		timeoutDuration = 10 * time.Minute
	}
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}).Namespace(namespace)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(timeoutDuration)
	defer timeout.Stop()
	for {
		_, err := resource.Get(ctx, name, v1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for Velero backup %q in namespace %q to be deleted", name, namespace)
		case <-ticker.C:
		}
	}
}

func (a *DynamicManifestApplier) CleanupStaleRestoreState(ctx context.Context, agentNamespace string, sourceNamespace string, targetNamespace string, currentRestoreName string) error {
	if agentNamespace == "" {
		return fmt.Errorf("agent namespace is required")
	}
	if sourceNamespace == "" {
		return fmt.Errorf("source namespace is required")
	}
	restoreNames, err := a.deleteMatchingRestores(ctx, agentNamespace, sourceNamespace, targetNamespace, currentRestoreName)
	if err != nil {
		return err
	}
	if err := a.deleteMatchingPodVolumeRestores(ctx, agentNamespace, restoreNames); err != nil {
		return err
	}
	return a.waitForRestoreStateCleanup(ctx, agentNamespace, restoreNames)
}

func (a *DynamicManifestApplier) deleteMatchingRestores(ctx context.Context, agentNamespace string, sourceNamespace string, targetNamespace string, currentRestoreName string) (map[string]struct{}, error) {
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}).Namespace(agentNamespace)
	list, err := resource.List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	restoreNames := map[string]struct{}{}
	for _, item := range list.Items {
		name := item.GetName()
		if name == "" || name == currentRestoreName {
			continue
		}
		if !matchesRestoreCleanupTarget(item, sourceNamespace, targetNamespace) {
			continue
		}
		restoreNames[name] = struct{}{}
		if err := resource.Delete(ctx, name, v1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	return restoreNames, nil
}

func (a *DynamicManifestApplier) deleteMatchingPodVolumeRestores(ctx context.Context, agentNamespace string, restoreNames map[string]struct{}) error {
	if len(restoreNames) == 0 {
		return nil
	}
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumerestores"}).Namespace(agentNamespace)
	list, err := resource.List(ctx, v1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, item := range list.Items {
		if !matchesPodVolumeRestoreCleanupTarget(item, restoreNames) {
			continue
		}
		if err := resource.Delete(ctx, item.GetName(), v1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (a *DynamicManifestApplier) waitForRestoreStateCleanup(ctx context.Context, agentNamespace string, restoreNames map[string]struct{}) error {
	if len(restoreNames) == 0 {
		return nil
	}
	restoreResource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}).Namespace(agentNamespace)
	pvrResource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumerestores"}).Namespace(agentNamespace)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(2 * time.Minute)
	defer timeout.Stop()
	for {
		pending, err := restoreCleanupPending(ctx, restoreResource, pvrResource, restoreNames)
		if err != nil {
			return err
		}
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for stale restore state to be deleted")
		case <-ticker.C:
		}
	}
}

func restoreCleanupPending(ctx context.Context, restoreResource dynamic.ResourceInterface, pvrResource dynamic.ResourceInterface, restoreNames map[string]struct{}) (bool, error) {
	for name := range restoreNames {
		_, err := restoreResource.Get(ctx, name, v1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	pvrList, err := pvrResource.List(ctx, v1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, item := range pvrList.Items {
		if matchesPodVolumeRestoreCleanupTarget(item, restoreNames) {
			return true, nil
		}
	}
	return false, nil
}

func matchesRestoreCleanupTarget(item unstructured.Unstructured, sourceNamespace string, targetNamespace string) bool {
	labels := item.GetLabels()
	if labels["hypercdr.io/source-namespace"] == sourceNamespace {
		labelTargetNamespace := labels["hypercdr.io/target-namespace"]
		if labelTargetNamespace == "" {
			labelTargetNamespace = sourceNamespace
		}
		if targetNamespace == "" || labelTargetNamespace == targetNamespace {
			return true
		}
	}
	namePrefix := "hcdr-restore-" + sourceNamespace + "-"
	if strings.HasPrefix(item.GetName(), namePrefix) && restoreTargetsNamespace(item, sourceNamespace, targetNamespace) {
		return true
	}
	spec, _ := item.Object["spec"].(map[string]any)
	if namespaceIncluded(spec, sourceNamespace) && restoreSpecTargetsNamespace(spec, sourceNamespace, targetNamespace) {
		return true
	}
	return false
}

func namespaceIncluded(spec map[string]any, sourceNamespace string) bool {
	if spec == nil {
		return false
	}
	namespaces, _ := spec["includedNamespaces"].([]any)
	for _, namespace := range namespaces {
		if value, _ := namespace.(string); value == sourceNamespace {
			return true
		}
	}
	return false
}

func restoreTargetsNamespace(item unstructured.Unstructured, sourceNamespace string, targetNamespace string) bool {
	spec, _ := item.Object["spec"].(map[string]any)
	return restoreSpecTargetsNamespace(spec, sourceNamespace, targetNamespace)
}

func restoreSpecTargetsNamespace(spec map[string]any, sourceNamespace string, targetNamespace string) bool {
	if targetNamespace == "" {
		return true
	}
	mapping, _ := spec["namespaceMapping"].(map[string]any)
	if mapping == nil {
		return targetNamespace == sourceNamespace
	}
	mappedTarget, _ := mapping[sourceNamespace].(string)
	if mappedTarget == "" {
		mappedTarget = sourceNamespace
	}
	return mappedTarget == targetNamespace
}

func matchesPodVolumeRestoreCleanupTarget(item unstructured.Unstructured, restoreNames map[string]struct{}) bool {
	labels := item.GetLabels()
	if restoreName := labels["velero.io/restore-name"]; restoreName != "" {
		_, ok := restoreNames[restoreName]
		return ok
	}
	name := item.GetName()
	for restoreName := range restoreNames {
		if strings.HasPrefix(name, restoreName+"-") {
			return true
		}
	}
	return false
}

func resourceForObject(object AppliedObject) (schema.GroupVersionResource, bool, error) {
	switch object.APIVersion + "/" + object.Kind {
	case "v1/Secret":
		return schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, true, nil
	case "v1/ConfigMap":
		return schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true, nil
	case "velero.io/v1/Backup":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}, true, nil
	case "velero.io/v1/Schedule":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "schedules"}, true, nil
	case "velero.io/v1/Restore":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "restores"}, true, nil
	case "velero.io/v1/DeleteBackupRequest":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "deletebackuprequests"}, true, nil
	case "velero.io/v1/PodVolumeBackup":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}, true, nil
	case "velero.io/v1/PodVolumeRestore":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumerestores"}, true, nil
	case "velero.io/v1/BackupRepository":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backuprepositories"}, true, nil
	case "velero.io/v1/BackupStorageLocation":
		return schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backupstoragelocations"}, true, nil
	default:
		return schema.GroupVersionResource{}, false, fmt.Errorf("unsupported manifest kind %s/%s", object.APIVersion, object.Kind)
	}
}

func (a *DynamicManifestApplier) DeleteVeleroBackupArtifacts(ctx context.Context, agentNamespace string, backupNames []string) (map[string][]string, error) {
	deleted := map[string][]string{}
	backupSet := map[string]struct{}{}
	for _, name := range backupNames {
		name = strings.TrimSpace(name)
		if name != "" {
			backupSet[name] = struct{}{}
		}
	}
	if agentNamespace == "" || len(backupSet) == 0 {
		return deleted, nil
	}
	for _, target := range []struct {
		key      string
		resource string
		match    func(unstructured.Unstructured) bool
	}{
		{key: "restores", resource: "restores", match: func(item unstructured.Unstructured) bool {
			return restoreMatchesAnyBackup(item, backupSet)
		}},
		{key: "podVolumeBackups", resource: "podvolumebackups", match: func(item unstructured.Unstructured) bool {
			return itemLabelInSet(item, "velero.io/backup-name", backupSet)
		}},
		{key: "backups", resource: "backups", match: func(item unstructured.Unstructured) bool {
			_, ok := backupSet[item.GetName()]
			return ok
		}},
	} {
		names, err := a.deleteMatchingVeleroObjects(ctx, agentNamespace, target.resource, target.match)
		if err != nil {
			return deleted, err
		}
		deleted[target.key] = names
	}
	restoreSet := map[string]struct{}{}
	for _, name := range deleted["restores"] {
		restoreSet[name] = struct{}{}
	}
	if len(restoreSet) > 0 {
		names, err := a.deleteMatchingVeleroObjects(ctx, agentNamespace, "podvolumerestores", func(item unstructured.Unstructured) bool {
			return matchesAnyRestoreName(item, restoreSet)
		})
		if err != nil {
			return deleted, err
		}
		deleted["podVolumeRestores"] = names
	}
	return deleted, nil
}

func (a *DynamicManifestApplier) DeleteVeleroBackupArtifactsByNamePrefix(ctx context.Context, agentNamespace string, backupNamePrefix string) (map[string][]string, error) {
	backupNamePrefix = strings.TrimSpace(backupNamePrefix)
	if agentNamespace == "" || backupNamePrefix == "" {
		return map[string][]string{}, nil
	}
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}).Namespace(agentNamespace)
	list, err := resource.List(ctx, v1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	backupNames := []string{}
	for _, item := range list.Items {
		name := item.GetName()
		if name != "" && strings.HasPrefix(name, backupNamePrefix) {
			backupNames = append(backupNames, name)
		}
	}
	return a.DeleteVeleroBackupArtifacts(ctx, agentNamespace, backupNames)
}

func (a *DynamicManifestApplier) DeleteBackupRepositories(ctx context.Context, agentNamespace string, storageLocation string, namespaces []string) ([]string, error) {
	deleted := []string{}
	for _, ns := range uniqueNonEmptyStrings(namespaces) {
		name := backupRepositoryName(ns, storageLocation)
		if name == "" {
			continue
		}
		err := a.DeleteObject(ctx, AppliedObject{
			APIVersion: "velero.io/v1",
			Kind:       "BackupRepository",
			Namespace:  agentNamespace,
			Name:       name,
		})
		if err != nil {
			return deleted, err
		}
		deleted = append(deleted, name)
	}
	return deleted, nil
}

func (a *DynamicManifestApplier) deleteMatchingVeleroObjects(ctx context.Context, agentNamespace string, resourceName string, match func(unstructured.Unstructured) bool) ([]string, error) {
	resource := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: resourceName}).Namespace(agentNamespace)
	list, err := resource.List(ctx, v1.ListOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	deleted := []string{}
	for _, item := range list.Items {
		if !match(item) {
			continue
		}
		name := item.GetName()
		if name == "" {
			continue
		}
		if err := resource.Delete(ctx, name, v1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return deleted, err
		}
		deleted = append(deleted, name)
	}
	slices.Sort(deleted)
	return deleted, nil
}

func itemLabelInSet(item unstructured.Unstructured, label string, values map[string]struct{}) bool {
	value := item.GetLabels()[label]
	if value == "" {
		return false
	}
	_, ok := values[value]
	return ok
}

func matchesAnyRestoreName(item unstructured.Unstructured, restoreNames map[string]struct{}) bool {
	value := item.GetLabels()["velero.io/restore-name"]
	if value != "" {
		_, ok := restoreNames[value]
		return ok
	}
	return false
}

func restoreMatchesAnyBackup(item unstructured.Unstructured, backupNames map[string]struct{}) bool {
	backupName, _, _ := unstructured.NestedString(item.Object, "spec", "backupName")
	if backupName != "" {
		_, ok := backupNames[backupName]
		return ok
	}
	return itemLabelInSet(item, "velero.io/backup-name", backupNames)
}

func backupRepositoryName(namespace string, storageLocation string) string {
	namespace = strings.TrimSpace(namespace)
	storageLocation = strings.TrimSpace(storageLocation)
	if namespace == "" || storageLocation == "" {
		return ""
	}
	return namespace + "-" + storageLocation + "-kopia"
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}
