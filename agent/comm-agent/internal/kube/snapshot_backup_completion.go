package kube

import (
	"context"
	"encoding/json"
	"fmt"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (a *DynamicManifestApplier) ReadSnapshotBackupCompletion(ctx context.Context, namespace, name string) (map[string]any, error) {
	backup, err := a.client.Resource(consistencyBackups).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
	if backup.GetUID() == "" || backup.GetDeletionTimestamp() != nil || phase != "Completed" {
		return nil, fmt.Errorf("snapshot backup is not a completed immutable object")
	}
	var inventory protocol.SnapshotBackupInventory
	if err := json.Unmarshal([]byte(backup.GetAnnotations()[protocol.SnapshotInventoryAnnotation]), &inventory); err != nil {
		return nil, fmt.Errorf("snapshot capture inventory unavailable: %w", err)
	}
	namespaces, _, _ := unstructured.NestedStringSlice(backup.Object, "spec", "includedNamespaces")
	if err := inventory.Validate(namespaces); err != nil {
		return nil, err
	}
	fsb, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range fsb.Items {
		if item.GetLabels()["velero.io/backup-uid"] == string(backup.GetUID()) || item.GetLabels()["velero.io/backup-name"] == name {
			return nil, fmt.Errorf("snapshot backup contains file-level volume operations")
		}
		for _, owner := range item.GetOwnerReferences() {
			if owner.Kind == "Backup" && owner.UID == backup.GetUID() {
				return nil, fmt.Errorf("snapshot backup contains file-level volume operations")
			}
		}
	}
	uploads, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: "velero.io/backup-uid=" + string(backup.GetUID())})
	if err != nil {
		return nil, err
	}
	if len(uploads.Items) != len(inventory.Volumes) {
		return nil, fmt.Errorf("snapshot DataUpload count differs from captured PVC inventory")
	}
	var volumes []consistency.Volume
	var requirements []SnapshotVolumeRequirement
	for _, volume := range inventory.Volumes {
		volumes = append(volumes, consistency.Volume{Namespace: volume.Namespace, Name: volume.Name, UID: volume.UID})
		requirements = append(requirements, SnapshotVolumeRequirement{Namespace: volume.Namespace, Name: volume.Name, UID: volume.UID, Class: volume.SnapshotClass})
	}
	var snapshots []consistency.Snapshot
	if len(volumes) > 0 {
		snapshots, err = a.ReadConsistencySnapshotBarrier(ctx, namespace, name, string(backup.GetUID()), volumes, requirements)
		if err != nil {
			return nil, err
		}
		if err := consistency.VerifyDurableSnapshots(consistency.Record{Volumes: volumes, Snapshots: snapshots}); err != nil {
			return nil, err
		}
	}
	final, err := a.client.Resource(consistencyBackups).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if final.GetUID() != backup.GetUID() || final.GetResourceVersion() != backup.GetResourceVersion() {
		return nil, fmt.Errorf("snapshot backup changed during evidence collection")
	}
	return map[string]any{"kind": "Backup", "name": name, "namespace": namespace, "uid": string(backup.GetUID()), "phase": "Completed", "manifest": backup.Object,
		"requestedConsistency": "snapshot-crash-consistent", "achievedConsistency": "snapshot-crash-consistent", "snapshotEvidence": protocol.SnapshotBackupEvidence{Inventory: inventory, Snapshots: snapshots}}, nil
}
