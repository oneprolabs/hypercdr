package kube

import (
	"context"
	"fmt"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (a *DynamicManifestApplier) ReadConsistencySnapshotBarrier(ctx context.Context, agentNamespace, backupName, backupUID string, volumes []consistency.Volume, expectedClasses ...[]SnapshotVolumeRequirement) ([]consistency.Snapshot, error) {
	if agentNamespace == "" || backupName == "" || backupUID == "" || len(volumes) == 0 {
		return nil, fmt.Errorf("snapshot barrier requires explicit backup and volume identities")
	}
	classes := map[string]SnapshotVolumeRequirement{}
	if len(expectedClasses) > 1 {
		return nil, fmt.Errorf("snapshot barrier accepts one explicit class inventory")
	}
	if len(expectedClasses) == 1 {
		if len(expectedClasses[0]) != len(volumes) {
			return nil, fmt.Errorf("snapshot class inventory does not cover the volume group")
		}
		for _, requirement := range expectedClasses[0] {
			if requirement.UID == "" || requirement.Class == "" || classes[requirement.UID].UID != "" {
				return nil, fmt.Errorf("invalid snapshot class inventory")
			}
			classes[requirement.UID] = requirement
		}
	}
	backup, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}).Namespace(agentNamespace).Get(ctx, backupName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(backup.GetUID()) != backupUID {
		return nil, fmt.Errorf("snapshot barrier backup identity changed")
	}
	moveData, _, _ := unstructured.NestedBool(backup.Object, "spec", "snapshotMoveData")
	snapshotsEnabled, _, _ := unstructured.NestedBool(backup.Object, "spec", "snapshotVolumes")
	fsb, _, _ := unstructured.NestedBool(backup.Object, "spec", "defaultVolumesToFsBackup")
	if !moveData || !snapshotsEnabled || fsb {
		return nil, fmt.Errorf("backup is not configured for strict CSI snapshot data movement")
	}
	uploads, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}).Namespace(agentNamespace).List(ctx, metav1.ListOptions{LabelSelector: "velero.io/backup-uid=" + backupUID})
	if err != nil {
		return nil, err
	}
	var result []consistency.Snapshot
	seen := map[string]bool{}
	for _, volume := range volumes {
		if volume.Namespace == "" || volume.Name == "" || volume.UID == "" || seen[volume.UID] {
			return nil, fmt.Errorf("invalid or duplicate snapshot barrier PVC inventory")
		}
		seen[volume.UID] = true
		pvc, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(volume.Namespace).Get(ctx, volume.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if string(pvc.GetUID()) != volume.UID {
			return nil, fmt.Errorf("snapshot barrier PVC %s/%s identity changed", volume.Namespace, volume.Name)
		}
		var matched *unstructured.Unstructured
		for index := range uploads.Items {
			upload := &uploads.Items[index]
			if upload.GetLabels()["velero.io/pvc-uid"] != volume.UID {
				continue
			}
			if matched != nil {
				return nil, fmt.Errorf("multiple DataUploads for required PVC %s/%s", volume.Namespace, volume.Name)
			}
			matched = upload
		}
		if matched == nil {
			return nil, fmt.Errorf("DataUpload missing for required PVC %s/%s", volume.Namespace, volume.Name)
		}
		if err := verifyConsistencyUploadIdentity(matched, backupName, backupUID, volume); err != nil {
			return nil, err
		}
		expectedClass := ""
		if len(expectedClasses) == 1 {
			requirement := classes[volume.UID]
			actualClass, _, _ := unstructured.NestedString(matched.Object, "spec", "csiSnapshot", "snapshotClass")
			if requirement.Namespace != volume.Namespace || requirement.Name != volume.Name || actualClass != requirement.Class {
				return nil, fmt.Errorf("DataUpload does not use the explicitly requested snapshot class for %s/%s", volume.Namespace, volume.Name)
			}
			expectedClass = requirement.Class
		}
		phase, _, _ := unstructured.NestedString(matched.Object, "status", "phase")
		if phase == "Completed" {
			repositoryID, _, _ := unstructured.NestedString(matched.Object, "status", "snapshotID")
			if repositoryID == "" {
				return nil, fmt.Errorf("completed DataUpload has no durable repository snapshot ID")
			}
			result = append(result, consistency.Snapshot{VolumeUID: volume.UID, Handle: repositoryID, Secured: true, Source: "kopia", DataUploadUID: string(matched.GetUID())})
			continue
		}
		snapshotName, _, _ := unstructured.NestedString(matched.Object, "spec", "csiSnapshot", "volumeSnapshot")
		if snapshotName == "" {
			return nil, fmt.Errorf("DataUpload has no CSI snapshot reference")
		}
		snapshotName = matched.GetName()
		snapshot, err := a.client.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}).Namespace(agentNamespace).Get(ctx, snapshotName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("stable snapshot evidence unavailable for %s/%s: %w", volume.Namespace, volume.Name, err)
		}
		ready, _, _ := unstructured.NestedBool(snapshot.Object, "status", "readyToUse")
		contentName, _, _ := unstructured.NestedString(snapshot.Object, "status", "boundVolumeSnapshotContentName")
		sourceContent, _, _ := unstructured.NestedString(snapshot.Object, "spec", "source", "volumeSnapshotContentName")
		actualClass, _, _ := unstructured.NestedString(snapshot.Object, "spec", "volumeSnapshotClassName")
		if expectedClass != "" && actualClass != expectedClass {
			return nil, fmt.Errorf("secured VolumeSnapshot class differs from the requested class")
		}
		if !ready || contentName != snapshotName || sourceContent != contentName || snapshot.GetUID() == "" || snapshot.GetDeletionTimestamp() != nil {
			return nil, fmt.Errorf("snapshot is not ready or does not identify required source PVC %s/%s", volume.Namespace, volume.Name)
		}
		content, err := a.client.Resource(schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotcontents"}).Get(ctx, contentName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		contentReady, _, _ := unstructured.NestedBool(content.Object, "status", "readyToUse")
		handle, _, _ := unstructured.NestedString(content.Object, "status", "snapshotHandle")
		refUID, _, _ := unstructured.NestedString(content.Object, "spec", "volumeSnapshotRef", "uid")
		refNamespace, _, _ := unstructured.NestedString(content.Object, "spec", "volumeSnapshotRef", "namespace")
		refName, _, _ := unstructured.NestedString(content.Object, "spec", "volumeSnapshotRef", "name")
		driver, _, _ := unstructured.NestedString(content.Object, "spec", "driver")
		uploadDriver, _, _ := unstructured.NestedString(matched.Object, "spec", "csiSnapshot", "driver")
		sourceHandle, _, _ := unstructured.NestedString(content.Object, "spec", "source", "snapshotHandle")
		contentClass, _, _ := unstructured.NestedString(content.Object, "spec", "volumeSnapshotClassName")
		if expectedClass != "" && contentClass != expectedClass {
			return nil, fmt.Errorf("secured VolumeSnapshotContent class differs from the requested class")
		}
		if !contentReady || handle == "" || handle != sourceHandle || refUID != string(snapshot.GetUID()) || refNamespace != agentNamespace || refName != snapshotName || driver == "" || driver != uploadDriver || content.GetDeletionTimestamp() != nil {
			return nil, fmt.Errorf("snapshot content identity, driver or readiness mismatch for %s/%s", volume.Namespace, volume.Name)
		}
		temporary, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(agentNamespace).Get(ctx, matched.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		bound, _, _ := unstructured.NestedString(temporary.Object, "status", "phase")
		sourceKind, _, _ := unstructured.NestedString(temporary.Object, "spec", "dataSource", "kind")
		sourceGroup, _, _ := unstructured.NestedString(temporary.Object, "spec", "dataSource", "apiGroup")
		sourceName, _, _ := unstructured.NestedString(temporary.Object, "spec", "dataSource", "name")
		owned := false
		for _, owner := range temporary.GetOwnerReferences() {
			if owner.Kind == "DataUpload" && owner.Name == matched.GetName() && owner.UID == matched.GetUID() && owner.UID != "" {
				owned = true
			}
		}
		if !owned || (bound != "Bound" && bound != "Pending") || sourceKind != "VolumeSnapshot" || sourceGroup != "snapshot.storage.k8s.io" || sourceName != snapshotName || temporary.GetDeletionTimestamp() != nil {
			return nil, fmt.Errorf("temporary snapshot volume is not secured for %s/%s", volume.Namespace, volume.Name)
		}
		result = append(result, consistency.Snapshot{VolumeUID: volume.UID, Handle: handle, Secured: true, Source: "csi", DataUploadUID: string(matched.GetUID())})
	}
	return result, nil
}

func verifyConsistencyUploadIdentity(upload *unstructured.Unstructured, backupName, backupUID string, volume consistency.Volume) error {
	owned := false
	for _, owner := range upload.GetOwnerReferences() {
		if owner.Kind == "Backup" && owner.Name == backupName && string(owner.UID) == backupUID && owner.Controller != nil && *owner.Controller {
			owned = true
		}
	}
	sourceNamespace, _, _ := unstructured.NestedString(upload.Object, "spec", "sourceNamespace")
	sourcePVC, _, _ := unstructured.NestedString(upload.Object, "spec", "sourcePVC")
	snapshotType, _, _ := unstructured.NestedString(upload.Object, "spec", "snapshotType")
	dataMover, _, _ := unstructured.NestedString(upload.Object, "spec", "datamover")
	phase, _, _ := unstructured.NestedString(upload.Object, "status", "phase")
	cancelled, _, _ := unstructured.NestedBool(upload.Object, "spec", "cancel")
	if !owned || upload.GetUID() == "" || upload.GetLabels()["velero.io/backup-uid"] != backupUID || sourceNamespace != volume.Namespace || sourcePVC != volume.Name || snapshotType != "CSI" || (dataMover != "" && dataMover != "velero") || cancelled || upload.GetDeletionTimestamp() != nil {
		return fmt.Errorf("DataUpload identity or CSI source mismatch for %s/%s", volume.Namespace, volume.Name)
	}
	if phase != "Accepted" && phase != "Prepared" && phase != "InProgress" && phase != "Completed" {
		return fmt.Errorf("snapshot mover has not secured source for %s/%s: %s", volume.Namespace, volume.Name, phase)
	}
	return nil
}
