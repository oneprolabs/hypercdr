package kube

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type SnapshotVolumeRequirement struct {
	Namespace string
	Name      string
	UID       string
	Class     string
}

func (a *DynamicManifestApplier) CheckSnapshotPrerequisites(ctx context.Context, volumes []SnapshotVolumeRequirement) error {
	if len(volumes) == 0 {
		return fmt.Errorf("snapshot capture requires an explicit nonempty PVC inventory")
	}
	classes, err := a.client.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("CSI snapshot classes unavailable: %w", err)
	}
	seen := map[string]bool{}
	for _, volume := range volumes {
		key := volume.Namespace + "/" + volume.Name
		if volume.Namespace == "" || volume.Name == "" || volume.UID == "" || volume.Class == "" || seen[key] {
			return fmt.Errorf("invalid or duplicate snapshot PVC requirement %q; namespace, name, UID and snapshot class are required", key)
		}
		seen[key] = true
		pvc, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(volume.Namespace).Get(ctx, volume.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read snapshot PVC %s: %w", key, err)
		}
		phase, _, _ := unstructured.NestedString(pvc.Object, "status", "phase")
		if string(pvc.GetUID()) != volume.UID || phase != "Bound" {
			return fmt.Errorf("snapshot PVC %s identity changed or is not Bound", key)
		}
		if selected := pvc.GetAnnotations()["velero.io/csi-volumesnapshot-class"]; selected != "" && selected != volume.Class {
			return fmt.Errorf("PVC %s overrides the explicitly requested snapshot class", key)
		}
		pvName, _, _ := unstructured.NestedString(pvc.Object, "spec", "volumeName")
		if pvName == "" {
			return fmt.Errorf("snapshot PVC %s has no bound volume", key)
		}
		pv, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}).Get(ctx, pvName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read snapshot volume %s: %w", pvName, err)
		}
		driver, _, _ := unstructured.NestedString(pv.Object, "spec", "csi", "driver")
		claimUID, _, _ := unstructured.NestedString(pv.Object, "spec", "claimRef", "uid")
		if driver == "" || claimUID != volume.UID {
			return fmt.Errorf("volume %s is not CSI-backed or has a different PVC owner", pvName)
		}
		matched := false
		for _, class := range classes.Items {
			classDriver, _, _ := unstructured.NestedString(class.Object, "driver")
			if class.GetName() == volume.Class && classDriver == driver && class.GetDeletionTimestamp() == nil {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("snapshot class %s does not match CSI driver %s for PVC %s", volume.Class, driver, key)
		}
	}
	return nil
}

func (a *DynamicManifestApplier) ResolveConsistencySnapshotClassAnnotations(ctx context.Context, volumes []SnapshotVolumeRequirement) (map[string]string, error) {
	if err := a.CheckSnapshotPrerequisites(ctx, volumes); err != nil {
		return nil, err
	}
	annotations := map[string]string{}
	classes := map[string]string{}
	for _, volume := range volumes {
		driver := classes[volume.Class]
		if driver == "" {
			class, err := a.client.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}).Get(ctx, volume.Class, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			driver, _, _ = unstructured.NestedString(class.Object, "driver")
			if driver == "" || class.GetDeletionTimestamp() != nil {
				return nil, fmt.Errorf("snapshot class %s changed during preparation", volume.Class)
			}
			classes[volume.Class] = driver
		}
		key := "velero.io/csi-volumesnapshot-class_" + driver
		if previous := annotations[key]; previous != "" && previous != volume.Class {
			return nil, fmt.Errorf("one consistency group cannot select multiple snapshot classes for CSI driver %s", driver)
		}
		annotations[key] = volume.Class
	}
	return annotations, nil
}
