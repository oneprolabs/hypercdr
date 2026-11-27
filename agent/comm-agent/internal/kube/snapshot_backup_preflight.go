package kube

import (
	"context"
	"encoding/json"
	"fmt"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (a *DynamicManifestApplier) PrepareSnapshotBackup(ctx context.Context, namespaces []string) (map[string]string, error) {
	if len(namespaces) == 0 {
		return nil, fmt.Errorf("snapshot backup requires explicit namespaces")
	}
	var volumes []SnapshotVolumeRequirement
	seen := map[string]bool{}
	for _, namespace := range namespaces {
		if namespace == "" || namespace == "*" || seen[namespace] {
			return nil, fmt.Errorf("invalid or duplicate snapshot namespace")
		}
		seen[namespace] = true
		pods, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "pods"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("read snapshot Pod annotations: %w", err)
		}
		for _, pod := range pods.Items {
			if pod.GetAnnotations()["backup.velero.io/backup-volumes"] != "" {
				return nil, fmt.Errorf("Pod %s/%s explicitly requests file-level backup; refusing mixed snapshot/file capture", namespace, pod.GetName())
			}
		}
		claims, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("read snapshot PVC inventory: %w", err)
		}
		if len(claims.Items) == 0 {
			continue
		}
		classes, err := a.client.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("read snapshot classes: %w", err)
		}
		for _, claim := range claims.Items {
			if claim.GetDeletionTimestamp() != nil {
				return nil, fmt.Errorf("snapshot PVC %s/%s is being deleted", namespace, claim.GetName())
			}
			pvName, _, _ := unstructured.NestedString(claim.Object, "spec", "volumeName")
			if pvName == "" {
				return nil, fmt.Errorf("snapshot PVC %s/%s is not bound", namespace, claim.GetName())
			}
			volume, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}).Get(ctx, pvName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("read snapshot PV: %w", err)
			}
			if volume.GetDeletionTimestamp() != nil {
				return nil, fmt.Errorf("snapshot PV %s is being deleted", pvName)
			}
			claimNamespace, _, _ := unstructured.NestedString(volume.Object, "spec", "claimRef", "namespace")
			claimName, _, _ := unstructured.NestedString(volume.Object, "spec", "claimRef", "name")
			if claimNamespace != namespace || claimName != claim.GetName() {
				return nil, fmt.Errorf("snapshot PV %s claim identity changed", pvName)
			}
			driver, _, _ := unstructured.NestedString(volume.Object, "spec", "csi", "driver")
			if driver == "" {
				return nil, fmt.Errorf("snapshot PVC %s/%s is not CSI-backed", namespace, claim.GetName())
			}
			selected := claim.GetAnnotations()["velero.io/csi-volumesnapshot-class"]
			var matches []string
			for _, class := range classes.Items {
				classDriver, _, _ := unstructured.NestedString(class.Object, "driver")
				if class.GetDeletionTimestamp() == nil && classDriver == driver && (selected == "" || selected == class.GetName()) {
					matches = append(matches, class.GetName())
				}
			}
			if len(matches) != 1 {
				return nil, fmt.Errorf("snapshot PVC %s/%s requires one explicit driver-matching snapshot class; found %d", namespace, claim.GetName(), len(matches))
			}
			volumes = append(volumes, SnapshotVolumeRequirement{Namespace: namespace, Name: claim.GetName(), UID: string(claim.GetUID()), Class: matches[0]})
		}
	}
	inventory := protocol.SnapshotBackupInventory{Namespaces: append([]string(nil), namespaces...), Volumes: []protocol.ConsistencyVolume{}}
	for _, volume := range volumes {
		inventory.Volumes = append(inventory.Volumes, protocol.ConsistencyVolume{Namespace: volume.Namespace, Name: volume.Name, UID: volume.UID, SnapshotClass: volume.Class})
	}
	annotations := map[string]string{}
	if len(volumes) > 0 {
		var err error
		annotations, err = a.ResolveConsistencySnapshotClassAnnotations(ctx, volumes)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		return nil, err
	}
	annotations[protocol.SnapshotInventoryAnnotation] = string(encoded)
	return annotations, nil
}
