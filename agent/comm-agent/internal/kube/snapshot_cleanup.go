package kube

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var cleanupSnapshots = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}

func (a *DynamicManifestApplier) RequestSnapshotBackupCancellation(ctx context.Context, namespace, name, taskID, commandID string) error {
	if namespace == "" || name == "" || taskID == "" || commandID == "" {
		return fmt.Errorf("snapshot cancellation requires complete task and backup identity")
	}
	backups := a.client.Resource(consistencyBackups).Namespace(namespace)
	backup, err := backups.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	labels := backup.GetLabels()
	moveData, _, _ := unstructured.NestedBool(backup.Object, "spec", "snapshotMoveData")
	if backup.GetUID() == "" || !moveData || labels["hypercdr.io/managed-by"] != "hypercdr" || labels["hypercdr.io/task-id"] != taskID || labels["hypercdr.io/command-id"] != commandID {
		return fmt.Errorf("snapshot cancellation backup identity does not match the failed task")
	}
	phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
	if snapshotCleanupTerminal(phase) {
		return nil
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": string(backup.GetUID()), "resourceVersion": backup.GetResourceVersion(), "annotations": map[string]string{"hypercdr.io/snapshot-cancel-task-id": taskID}}})
	if err != nil {
		return err
	}
	_, err = backups.Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func snapshotCleanupTerminal(phase string) bool {
	switch phase {
	case "Completed", "Failed", "PartiallyFailed", "FailedValidation", "Canceled":
		return true
	default:
		return false
	}
}

func ownedTemporarySnapshot(snapshot, backup *unstructured.Unstructured) bool {
	annotations := snapshot.GetAnnotations()
	return backup.GetUID() != "" && snapshot.GetUID() != "" &&
		backup.GetLabels()["hypercdr.io/managed-by"] == "hypercdr" &&
		backup.GetLabels()["hypercdr.io/task-id"] != "" &&
		annotations["hypercdr.io/snapshot-backup-uid"] == string(backup.GetUID()) &&
		annotations["hypercdr.io/snapshot-task-id"] == backup.GetLabels()["hypercdr.io/task-id"]
}

func (a *DynamicManifestApplier) CleanupTerminalBackupSnapshots(ctx context.Context, agentNamespace string) error {
	if agentNamespace == "" || agentNamespace == "*" {
		return fmt.Errorf("snapshot cleanup requires an explicit agent namespace")
	}
	backups := a.client.Resource(consistencyBackups).Namespace(agentNamespace)
	items, err := backups.List(ctx, metav1.ListOptions{LabelSelector: "hypercdr.io/managed-by=hypercdr"})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for index := range items.Items {
		backup := &items.Items[index]
		phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
		moveData, _, _ := unstructured.NestedBool(backup.Object, "spec", "snapshotMoveData")
		terminal := snapshotCleanupTerminal(phase)
		cancelRequested := backup.GetAnnotations()["hypercdr.io/snapshot-cancel-task-id"] != "" && backup.GetAnnotations()["hypercdr.io/snapshot-cancel-task-id"] == backup.GetLabels()["hypercdr.io/task-id"]
		if (!terminal && !cancelRequested) || !moveData || backup.GetUID() == "" || backup.GetLabels()["hypercdr.io/task-id"] == "" {
			continue
		}
		uploads, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace(agentNamespace).List(ctx, metav1.ListOptions{LabelSelector: "velero.io/backup-uid=" + string(backup.GetUID())})
		if err != nil {
			return err
		}
		stopped := true
		for _, upload := range uploads.Items {
			uploadPhase, _, _ := unstructured.NestedString(upload.Object, "status", "phase")
			if uploadPhase != "Completed" && uploadPhase != "Failed" && uploadPhase != "Canceled" {
				stopped = false
				if phase != "Completed" {
					owned := false
					for _, owner := range upload.GetOwnerReferences() {
						if owner.Kind == "Backup" && owner.Name == backup.GetName() && owner.UID == backup.GetUID() {
							owned = true
						}
					}
					requested, _, _ := unstructured.NestedBool(upload.Object, "spec", "cancel")
					if owned && upload.GetUID() != "" && !requested {
						patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": string(upload.GetUID()), "resourceVersion": upload.GetResourceVersion()}, "spec": map[string]any{"cancel": true}})
						if err != nil {
							return err
						}
						if _, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace(agentNamespace).Patch(ctx, upload.GetName(), types.MergePatchType, patch, metav1.PatchOptions{}); err != nil && !apierrors.IsNotFound(err) {
							return fmt.Errorf("cancel terminal backup's remaining upload: %w", err)
						}
					}
				}
			}
		}
		if !stopped || !terminal {
			continue
		}
		namespaces, _, _ := unstructured.NestedStringSlice(backup.Object, "spec", "includedNamespaces")
		seen := map[string]bool{}
		for _, namespace := range append(namespaces, agentNamespace) {
			if namespace == "" || namespace == "*" || seen[namespace] {
				continue
			}
			seen[namespace] = true
			resource := a.client.Resource(cleanupSnapshots).Namespace(namespace)
			snapshots, err := resource.List(ctx, metav1.ListOptions{})
			if apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return err
			}
			var claims *unstructured.UnstructuredList
			for _, snapshot := range snapshots.Items {
				if snapshot.GetDeletionTimestamp() != nil || !ownedTemporarySnapshot(&snapshot, backup) {
					continue
				}
				if claims == nil {
					claims, err = a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
					if err != nil {
						return err
					}
				}
				referenced := false
				for _, claim := range claims.Items {
					for _, field := range []string{"dataSource", "dataSourceRef"} {
						kind, _, _ := unstructured.NestedString(claim.Object, "spec", field, "kind")
						name, _, _ := unstructured.NestedString(claim.Object, "spec", field, "name")
						if kind == "VolumeSnapshot" && name == snapshot.GetName() {
							referenced = true
						}
					}
				}
				if referenced {
					continue
				}
				current, err := backups.Get(ctx, backup.GetName(), metav1.GetOptions{})
				if err != nil {
					return err
				}
				if current.GetUID() != backup.GetUID() || current.GetResourceVersion() != backup.GetResourceVersion() {
					return fmt.Errorf("snapshot cleanup backup identity changed")
				}
				approved := snapshot.DeepCopy()
				annotations := approved.GetAnnotations()
				annotations["hypercdr.io/snapshot-cleanup-approved-uid"] = string(snapshot.GetUID())
				annotations["hypercdr.io/snapshot-backup-name"] = backup.GetName()
				annotations["hypercdr.io/snapshot-backup-namespace"] = agentNamespace
				approved.SetAnnotations(annotations)
				labels := approved.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels["hypercdr.io/managed-by"] = "hypercdr"
				approved.SetLabels(labels)
				updated, err := resource.Update(ctx, approved, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("record snapshot cleanup approval: %w", err)
				}
				snapshot = *updated
				uid, version := snapshot.GetUID(), snapshot.GetResourceVersion()
				if err := resource.Delete(ctx, snapshot.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}); err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("cleanup owned snapshot %s/%s: %w", namespace, snapshot.GetName(), err)
				}
			}
		}
	}
	return a.cleanupApprovedOrphanSnapshots(ctx, agentNamespace)
}

func (a *DynamicManifestApplier) cleanupApprovedOrphanSnapshots(ctx context.Context, agentNamespace string) error {
	snapshots, err := a.client.Resource(cleanupSnapshots).List(ctx, metav1.ListOptions{LabelSelector: "hypercdr.io/managed-by=hypercdr"})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots.Items {
		annotations := snapshot.GetAnnotations()
		backupUID := annotations["hypercdr.io/snapshot-backup-uid"]
		backupName := annotations["hypercdr.io/snapshot-backup-name"]
		if snapshot.GetUID() == "" || snapshot.GetDeletionTimestamp() != nil ||
			annotations["hypercdr.io/snapshot-cleanup-approved-uid"] != string(snapshot.GetUID()) ||
			annotations["hypercdr.io/snapshot-task-id"] == "" || backupUID == "" || backupName == "" ||
			annotations["hypercdr.io/snapshot-backup-namespace"] != agentNamespace {
			continue
		}
		_, err := a.client.Resource(consistencyBackups).Namespace(agentNamespace).Get(ctx, backupName, metav1.GetOptions{})
		if err == nil {
			continue
		}
		if !apierrors.IsNotFound(err) {
			return err
		}
		uploads, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace(agentNamespace).List(ctx, metav1.ListOptions{LabelSelector: "velero.io/backup-uid=" + backupUID})
		if err != nil {
			return err
		}
		stopped := true
		for _, upload := range uploads.Items {
			phase, _, _ := unstructured.NestedString(upload.Object, "status", "phase")
			if phase != "Completed" && phase != "Failed" && phase != "Canceled" {
				stopped = false
			}
		}
		if !stopped {
			continue
		}
		claims, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(snapshot.GetNamespace()).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		referenced := false
		for _, claim := range claims.Items {
			for _, field := range []string{"dataSource", "dataSourceRef"} {
				kind, _, _ := unstructured.NestedString(claim.Object, "spec", field, "kind")
				name, _, _ := unstructured.NestedString(claim.Object, "spec", field, "name")
				if kind == "VolumeSnapshot" && name == snapshot.GetName() {
					referenced = true
				}
			}
		}
		if referenced {
			continue
		}
		uid, version := snapshot.GetUID(), snapshot.GetResourceVersion()
		if err := a.client.Resource(cleanupSnapshots).Namespace(snapshot.GetNamespace()).Delete(ctx, snapshot.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("cleanup approved orphan snapshot %s/%s: %w", snapshot.GetNamespace(), snapshot.GetName(), err)
		}
	}
	return nil
}
