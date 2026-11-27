package kube

import (
	"context"
	"encoding/json"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (a *DynamicManifestApplier) enrichSnapshotProgress(ctx context.Context, namespace string, backup *unstructured.Unstructured, progress *VolumeProgress) {
	var inventory protocol.SnapshotBackupInventory
	if json.Unmarshal([]byte(backup.GetAnnotations()[protocol.SnapshotInventoryAnnotation]), &inventory) != nil || inventory.Validate(inventory.Namespaces) != nil {
		return
	}
	progress.DiscoveryComplete = true
	progress.DiscoveryCount = len(inventory.Volumes)
	for index := range progress.Items {
		item := &progress.Items[index]
		if item.Kind != "DataUpload" {
			continue
		}
		item.TemporaryVolumeReady = item.Phase == "Prepared" || item.Phase == "InProgress" || item.Phase == "Completed" || item.StartedAt != ""
		item.SnapshotReady = item.TemporaryVolumeReady
		if item.SnapshotReady {
			continue
		}
		upload, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace(namespace).Get(ctx, item.Name, metav1.GetOptions{})
		if err != nil {
			item.PreparationMessage = "Unable to observe snapshot preparation: " + err.Error()
			continue
		}
		name, _, _ := unstructured.NestedString(upload.Object, "spec", "csiSnapshot", "volumeSnapshot")
		if name == "" {
			continue
		}
		snapshot, err := a.client.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}).Namespace(item.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			snapshot, err = a.client.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}).Namespace(namespace).Get(ctx, item.Name, metav1.GetOptions{})
		}
		if err != nil {
			item.PreparationMessage = "Unable to observe CSI snapshot: " + err.Error()
			continue
		}
		item.SnapshotReady, _, _ = unstructured.NestedBool(snapshot.Object, "status", "readyToUse")
		item.PreparationMessage, _, _ = unstructured.NestedString(snapshot.Object, "status", "error", "message")
	}
	for _, volume := range inventory.Volumes {
		found := false
		for _, item := range progress.Items {
			if item.Namespace == volume.Namespace && item.Volume == volume.Name {
				found = true
				break
			}
		}
		if !found {
			progress.Items = append(progress.Items, VolumeProgressItem{Kind: "DataUpload", Name: "pending-" + volume.UID, Namespace: volume.Namespace, Volume: volume.Name, Phase: "New"})
			progress.UnknownTotalCount++
			progress.AllTotalsKnown = false
		}
	}
}
