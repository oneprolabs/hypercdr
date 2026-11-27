package kube

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (r *KubernetesClusterReader) ReadSnapshotVolumePrerequisites(ctx context.Context, namespace string) ([]NamedCapability, error) {
	if namespace == "" || namespace == "*" {
		return nil, fmt.Errorf("snapshot inventory requires an explicit namespace")
	}
	claims, err := r.clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list snapshot PVC inventory: %w", err)
	}
	var classes []unstructured.Unstructured
	var classErr error
	if r.dynamic == nil {
		classErr = fmt.Errorf("snapshot API client unavailable")
	} else {
		list, err := r.dynamic.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}).List(ctx, metav1.ListOptions{})
		classErr = err
		if err == nil {
			classes = list.Items
		}
	}
	items := make([]NamedCapability, 0, len(claims.Items))
	for _, claim := range claims.Items {
		fields := map[string]string{"namespace": namespace, "uid": string(claim.UID), "status": "unknown", "reasonCode": "pvc-not-ready", "reason": "PVC is not bound or is being deleted", "verification": "read-only-prerequisites"}
		if claim.Spec.StorageClassName != nil {
			fields["storageClass"] = *claim.Spec.StorageClassName
		}
		item := NamedCapability{Type: "CSISnapshotVolumePrerequisite", Name: claim.Name, Fields: fields}
		if claim.UID != "" && claim.DeletionTimestamp == nil && claim.Status.Phase == corev1.ClaimBound && claim.Spec.VolumeName != "" {
			volume, err := r.clientset.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
			switch {
			case err != nil:
				fields["reasonCode"] = "pv-read-failed"
				fields["status"], fields["reason"] = "error", "Cannot read bound PV"
			case volume.DeletionTimestamp != nil || volume.Spec.ClaimRef == nil || volume.Spec.ClaimRef.UID != claim.UID || volume.Spec.ClaimRef.Namespace != namespace || volume.Spec.ClaimRef.Name != claim.Name:
				fields["reasonCode"] = "pv-identity-mismatch"
				fields["reason"] = "Bound PV identity does not match the current PVC"
			case volume.Spec.CSI == nil || volume.Spec.CSI.Driver == "":
				fields["reasonCode"] = "non-csi-volume"
				fields["status"], fields["reason"] = "unsupported", "Bound PV is not CSI-backed"
			default:
				item.Driver = volume.Spec.CSI.Driver
				switch {
				case apierrors.IsNotFound(classErr):
					fields["reasonCode"] = "snapshot-api-missing"
					fields["status"], fields["reason"] = "unsupported", "Install compatible snapshot APIs, controller and a driver-matching VolumeSnapshotClass"
				case classErr != nil:
					fields["reasonCode"] = "snapshot-class-read-failed"
					fields["status"], fields["reason"] = "error", "Cannot read snapshot classes; check API availability and Agent permissions"
				default:
					var matches []string
					selected := claim.Annotations["velero.io/csi-volumesnapshot-class"]
					for _, class := range classes {
						driver, _, _ := unstructured.NestedString(class.Object, "driver")
						if class.GetDeletionTimestamp() == nil && driver == item.Driver && (selected == "" || selected == class.GetName()) {
							matches = append(matches, class.GetName())
						}
					}
					sort.Strings(matches)
					switch len(matches) {
					case 0:
						fields["reasonCode"] = "snapshot-class-missing"
						fields["status"], fields["reason"] = "unsupported", "Configure a snapshot class matching the bound PV CSI driver and any PVC override"
					case 1:
						fields["reasonCode"] = "prerequisites-configured"
						fields["status"], fields["snapshotClass"], fields["reason"] = "configured", matches[0], "Read-only prerequisites match; snapshot creation and application consistency are not verified"
					default:
						fields["reasonCode"] = "snapshot-class-ambiguous"
						fields["reason"] = "Multiple matching snapshot classes; explicitly select one before backup"
					}
				}
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].Name < items[right].Name })
	return items, nil
}
