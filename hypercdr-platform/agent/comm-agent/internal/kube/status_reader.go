package kube

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type ManifestStatusReader interface {
	GetManifestStatus(ctx context.Context, object AppliedObject) (ManifestStatus, error)
}

type VolumeProgressReader interface {
	GetBackupVolumeProgress(ctx context.Context, namespace string, backupName string) (VolumeProgress, error)
	GetRestoreVolumeProgress(ctx context.Context, namespace string, restoreName string) (VolumeProgress, error)
}

type BackupObjectStatsReader interface {
	GetBackupObjectStats(ctx context.Context, namespace string, storageLocation string, backupName string) (BackupObjectStats, error)
	GetBackupVolumeInfoStats(ctx context.Context, namespace string, storageLocation string, backupName string) (BackupVolumeInfoStats, error)
	GetPlanObjectStorageStats(ctx context.Context, namespace string, storageLocation string, backupNamePrefix string, repositoryNamespaces []string) (PlanObjectStorageStats, error)
	GetRestoreResultSummary(ctx context.Context, namespace string, storageLocation string, restoreName string) (RestoreResultSummary, error)
}

type VeleroBackupReader interface {
	ListVeleroBackups(ctx context.Context, namespace string, limit int) ([]VeleroBackupSummary, error)
}

type VeleroScheduleReader interface {
	ListVeleroSchedules(ctx context.Context, namespace string, limit int) ([]VeleroScheduleSummary, error)
}

type ManifestStatus struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Phase      string
	Message    string
	Reason     string
	Errors     int64
	Warnings   int64
	ItemsDone  int64
	ItemsTotal int64
	Raw        map[string]any
}

type VolumeProgress struct {
	Items             []VolumeProgressItem
	BytesDone         int64
	TotalBytes        int64
	KnownTotal        bool
	AllTotalsKnown    bool
	KnownTotalCount   int
	UnknownTotalCount int
	RunningCount      int
	FailedCount       int
	Completed         int
}

type BackupObjectStats struct {
	MetadataPackageBytes int64
	ObjectCount          int
	Prefix               string
}

type BackupVolumeInfoStats struct {
	VolumeBytes int64
	VolumeCount int
	Key         string
	Accurate    bool
}

type PlanObjectStorageStats struct {
	MetadataBytes     int64
	KopiaBytes        int64
	TotalBytes        int64
	BackupObjectCount int
	KopiaObjectCount  int
	BackupNamePrefix  string
	BackupPrefix      string
	KopiaPrefixes     []string
	KopiaNamespaces   []string
}

type RestoreResultSummary struct {
	Key          string
	ErrorCount   int
	WarningCount int
	Errors       []string
	Warnings     []string
}

type VolumeProgressItem struct {
	Kind       string
	Name       string
	Phase      string
	BytesDone  int64
	TotalBytes int64
	KnownTotal bool
	Message    string
}

type VeleroBackupSummary struct {
	Name               string
	Namespace          string
	Phase              string
	ResourceVersion    string
	StorageLocation    string
	IncludedNamespaces []string
	Labels             map[string]string
	CreatedAt          time.Time
	StartedAt          time.Time
	CompletedAt        time.Time
	ItemsTotal         int64
	ItemsDone          int64
	Errors             int64
	Warnings           int64
}

type VeleroScheduleSummary struct {
	Name            string
	Namespace       string
	ResourceVersion string
	Labels          map[string]string
	CreatedAt       time.Time
}

type RestoreReadinessReader interface {
	GetNamespaceReadiness(ctx context.Context, namespace string) (NamespaceReadiness, error)
}

type NamespaceReadiness struct {
	Namespace       string
	NamespacePhase  string
	Ready           bool
	Message         string
	PodCount        int
	ReadyPodCount   int
	WorkloadCount   int
	ReadyWorkloads  int
	ServiceCount    int
	UnreadyPods     []string
	UnreadyWorkload []string
	FailureCode     string
	FailureMessage  string
}

func (a *DynamicManifestApplier) GetManifestStatus(ctx context.Context, object AppliedObject) (ManifestStatus, error) {
	if object.APIVersion == "" || object.Kind == "" || object.Name == "" {
		return ManifestStatus{}, fmt.Errorf("object apiVersion, kind, and name are required")
	}
	gvr, namespaced, err := resourceForObject(object)
	if err != nil {
		return ManifestStatus{}, err
	}
	var resource dynamic.ResourceInterface
	if namespaced {
		if object.Namespace == "" {
			return ManifestStatus{}, fmt.Errorf("%s %q requires namespace", object.Kind, object.Name)
		}
		resource = a.client.Resource(gvr).Namespace(object.Namespace)
	} else {
		resource = a.client.Resource(gvr)
	}
	current, err := resource.Get(ctx, object.Name, metav1.GetOptions{})
	if err != nil {
		return ManifestStatus{}, err
	}
	raw, _ := current.Object["status"].(map[string]any)
	status := ManifestStatus{
		APIVersion: object.APIVersion,
		Kind:       object.Kind,
		Namespace:  object.Namespace,
		Name:       object.Name,
		Raw:        raw,
	}
	if raw == nil {
		status.Raw = map[string]any{}
		return status, nil
	}
	status.Phase = stringField(raw, "phase")
	status.Message = firstStringField(raw, "message", "failureReason", "validationErrors")
	status.Reason = firstStringField(raw, "reason", "failureReason")
	status.Errors = intField(raw, "errors")
	status.Warnings = intField(raw, "warnings")
	status.ItemsDone = int64FieldPath(raw, "progress", "itemsBackedUp")
	if status.ItemsDone == 0 {
		status.ItemsDone = int64FieldPath(raw, "progress", "itemsRestored")
	}
	status.ItemsTotal = int64FieldPath(raw, "progress", "totalItems")
	return status, nil
}

func (a *DynamicManifestApplier) GetBackupVolumeProgress(ctx context.Context, namespace string, backupName string) (VolumeProgress, error) {
	return a.getVolumeProgress(ctx, namespace, backupName, []volumeProgressResource{
		{GVR: schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}, Kind: "PodVolumeBackup", Label: "velero.io/backup-name"},
		{GVR: schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}, Kind: "DataUpload", Label: "velero.io/backup-name"},
	})
}

func (a *DynamicManifestApplier) ListVeleroBackups(ctx context.Context, namespace string, limit int) ([]VeleroBackupSummary, error) {
	if namespace == "" {
		namespace = "velero"
	}
	list, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	items := list.Items
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	result := make([]VeleroBackupSummary, 0, len(items))
	for _, item := range items {
		phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
		storageLocation, _, _ := unstructured.NestedString(item.Object, "spec", "storageLocation")
		includedNamespaces, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "includedNamespaces")
		startedAt, _ := nestedTime(item.Object, "status", "startTimestamp")
		completedAt, _ := nestedTime(item.Object, "status", "completionTimestamp")
		totalItems, _, _ := unstructured.NestedInt64(item.Object, "status", "progress", "totalItems")
		itemsDone, _, _ := unstructured.NestedInt64(item.Object, "status", "progress", "itemsBackedUp")
		errors, _, _ := unstructured.NestedInt64(item.Object, "status", "errors")
		warnings, _, _ := unstructured.NestedInt64(item.Object, "status", "warnings")
		result = append(result, VeleroBackupSummary{
			Name:               item.GetName(),
			Namespace:          item.GetNamespace(),
			Phase:              phase,
			ResourceVersion:    item.GetResourceVersion(),
			StorageLocation:    storageLocation,
			IncludedNamespaces: includedNamespaces,
			Labels:             item.GetLabels(),
			CreatedAt:          item.GetCreationTimestamp().Time,
			StartedAt:          startedAt,
			CompletedAt:        completedAt,
			ItemsTotal:         totalItems,
			ItemsDone:          itemsDone,
			Errors:             errors,
			Warnings:           warnings,
		})
	}
	return result, nil
}

func (a *DynamicManifestApplier) ListVeleroSchedules(ctx context.Context, namespace string, limit int) ([]VeleroScheduleSummary, error) {
	if namespace == "" {
		namespace = "velero"
	}
	list, err := a.client.Resource(schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "schedules"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	items := list.Items
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	result := make([]VeleroScheduleSummary, 0, len(items))
	for _, item := range items {
		result = append(result, VeleroScheduleSummary{
			Name:            item.GetName(),
			Namespace:       item.GetNamespace(),
			ResourceVersion: item.GetResourceVersion(),
			Labels:          item.GetLabels(),
			CreatedAt:       item.GetCreationTimestamp().Time,
		})
	}
	return result, nil
}

func (a *DynamicManifestApplier) GetRestoreVolumeProgress(ctx context.Context, namespace string, restoreName string) (VolumeProgress, error) {
	return a.getVolumeProgress(ctx, namespace, restoreName, []volumeProgressResource{
		{GVR: schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumerestores"}, Kind: "PodVolumeRestore", Label: "velero.io/restore-name"},
		{GVR: schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datadownloads"}, Kind: "DataDownload", Label: "velero.io/restore-name"},
	})
}

type volumeProgressResource struct {
	GVR   schema.GroupVersionResource
	Kind  string
	Label string
}

func (a *DynamicManifestApplier) getVolumeProgress(ctx context.Context, namespace string, ownerName string, resources []volumeProgressResource) (VolumeProgress, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(ownerName) == "" {
		return VolumeProgress{}, nil
	}
	result := VolumeProgress{}
	for _, resource := range resources {
		items, err := a.client.Resource(resource.GVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for _, item := range items.Items {
			if !volumeProgressBelongsTo(item, resource.Label, ownerName) {
				continue
			}
			progress := volumeProgressFromItem(resource.Kind, item)
			if resource.Kind == "PodVolumeRestore" && progress.Phase == "" && time.Since(item.GetCreationTimestamp().Time) >= 30*time.Second {
				if message := a.podVolumeRestoreDependencyFailure(ctx, item); message != "" {
					progress.Phase = "FailedValidation"
					progress.Message = message
				}
			}
			if resource.Kind == "PodVolumeRestore" && progress.Phase == "" && time.Since(item.GetCreationTimestamp().Time) >= 2*time.Minute {
				progress.Phase = "FailedValidation"
				progress.Message = "volume data restoration did not start within 2 minutes; verify that the restored PVC exists, is bound, and can be mounted by a target node"
			}
			result.Items = append(result.Items, progress)
			result.BytesDone += progress.BytesDone
			if progress.KnownTotal {
				result.TotalBytes += progress.TotalBytes
				result.KnownTotal = true
				result.KnownTotalCount++
			} else {
				result.UnknownTotalCount++
			}
			switch progress.Phase {
			case "Completed":
				result.Completed++
			case "Failed", "FailedValidation", "PartiallyFailed", "Canceled":
				result.FailedCount++
			case "InProgress", "Accepted", "Prepared", "New":
				result.RunningCount++
			}
		}
	}
	result.AllTotalsKnown = len(result.Items) > 0 && result.UnknownTotalCount == 0
	return result, nil
}

func (a *DynamicManifestApplier) podVolumeRestoreDependencyFailure(ctx context.Context, item unstructured.Unstructured) string {
	podNamespace, _, _ := unstructured.NestedString(item.Object, "spec", "pod", "namespace")
	podName, _, _ := unstructured.NestedString(item.Object, "spec", "pod", "name")
	volumeName, _, _ := unstructured.NestedString(item.Object, "spec", "volume")
	if podNamespace == "" || podName == "" || volumeName == "" {
		return ""
	}
	pod, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "pods"}).Namespace(podNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return ""
	}
	volumes, _, _ := unstructured.NestedSlice(pod.Object, "spec", "volumes")
	for _, raw := range volumes {
		volume, _ := raw.(map[string]any)
		if stringField(volume, "name") != volumeName {
			continue
		}
		claimName, _, _ := unstructured.NestedString(volume, "persistentVolumeClaim", "claimName")
		if claimName == "" {
			return ""
		}
		_, err = a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}).Namespace(podNamespace).Get(ctx, claimName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return fmt.Sprintf("volume restore cannot start because persistent volume claim %s/%s does not exist", podNamespace, claimName)
		}
		return ""
	}
	return ""
}

func volumeProgressBelongsTo(item unstructured.Unstructured, label string, ownerName string) bool {
	labels := item.GetLabels()
	if labels[label] == ownerName {
		return true
	}
	return strings.HasPrefix(item.GetName(), ownerName+"-")
}

func volumeProgressFromItem(kind string, item unstructured.Unstructured) VolumeProgressItem {
	bytesDone, _, _ := unstructured.NestedInt64(item.Object, "status", "progress", "bytesDone")
	totalBytes, ok, _ := unstructured.NestedInt64(item.Object, "status", "progress", "totalBytes")
	phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
	message, _, _ := unstructured.NestedString(item.Object, "status", "message")
	return VolumeProgressItem{
		Kind:       kind,
		Name:       item.GetName(),
		Phase:      phase,
		BytesDone:  bytesDone,
		TotalBytes: totalBytes,
		KnownTotal: ok && totalBytes > 0,
		Message:    message,
	}
}

func nestedTime(object map[string]any, fields ...string) (time.Time, bool) {
	value, ok, _ := unstructured.NestedString(object, fields...)
	if !ok || value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func (a *DynamicManifestApplier) GetNamespaceReadiness(ctx context.Context, namespace string) (NamespaceReadiness, error) {
	if strings.TrimSpace(namespace) == "" {
		return NamespaceReadiness{}, fmt.Errorf("namespace is required")
	}
	result := NamespaceReadiness{Namespace: namespace}
	ns, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return result, err
	}
	result.NamespacePhase, _, _ = unstructured.NestedString(ns.Object, "status", "phase")
	if result.NamespacePhase != "Active" {
		result.Message = fmt.Sprintf("namespace %q is not active", namespace)
		return result, nil
	}

	if err := a.readPodReadiness(ctx, namespace, &result); err != nil {
		return result, err
	}
	if err := a.readWorkloadReadiness(ctx, namespace, &result); err != nil {
		return result, err
	}
	services, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "services"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return result, err
	}
	result.ServiceCount = len(services.Items)

	switch {
	case result.PodCount == 0 && result.WorkloadCount == 0:
		result.Message = fmt.Sprintf("namespace %q has no restored pods or workloads yet", namespace)
	case len(result.UnreadyPods) > 0:
		result.Message = "waiting for restored pods to become ready: " + strings.Join(result.UnreadyPods, ", ")
	case len(result.UnreadyWorkload) > 0:
		result.Message = "waiting for restored workloads to become ready: " + strings.Join(result.UnreadyWorkload, ", ")
	default:
		result.Ready = true
		result.Message = fmt.Sprintf("restored namespace %q is ready", namespace)
	}
	return result, nil
}

func (a *DynamicManifestApplier) readPodReadiness(ctx context.Context, namespace string, result *NamespaceReadiness) error {
	pods, err := a.client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "pods"}).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
		if phase == "Succeeded" {
			continue
		}
		name := pod.GetName()
		result.PodCount++
		if code, message := podTerminalReadinessFailure(pod.Object); code != "" {
			result.FailureCode = code
			result.FailureMessage = message
			result.UnreadyPods = append(result.UnreadyPods, name)
			continue
		}
		if isPodReady(pod.Object) {
			result.ReadyPodCount++
			continue
		}
		result.UnreadyPods = append(result.UnreadyPods, name)
	}
	return nil
}

func podTerminalReadinessFailure(object map[string]any) (string, string) {
	podName, _, _ := unstructured.NestedString(object, "metadata", "name")
	for _, statusPath := range [][]string{{"status", "initContainerStatuses"}, {"status", "containerStatuses"}} {
		statuses, _, _ := unstructured.NestedSlice(object, statusPath...)
		for _, raw := range statuses {
			status, _ := raw.(map[string]any)
			waiting, _, _ := unstructured.NestedMap(status, "state", "waiting")
			reason := stringField(waiting, "reason")
			switch reason {
			case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError":
				containerName := stringField(status, "name")
				image := stringField(status, "image")
				detail := stringField(waiting, "message")
				message := fmt.Sprintf("restored pod %s container %s cannot start: %s", podName, containerName, reason)
				if image != "" {
					message += fmt.Sprintf(" (image %s)", image)
				}
				if detail != "" {
					message += ": " + detail
				}
				return "RESTORE_WORKLOAD_IMAGE_PULL_FAILED", message
			case "CrashLoopBackOff":
				if intField(status, "restartCount") < 3 {
					continue
				}
				containerName := stringField(status, "name")
				terminated, _, _ := unstructured.NestedMap(status, "lastState", "terminated")
				exitCode := intField(terminated, "exitCode")
				detail := firstStringField(terminated, "message", "reason")
				message := fmt.Sprintf("restored pod %s container %s is crash looping after %d restarts", podName, containerName, intField(status, "restartCount"))
				if exitCode != 0 {
					message += fmt.Sprintf(" (last exit code %d)", exitCode)
				}
				if detail != "" {
					message += ": " + detail
				}
				return "RESTORE_WORKLOAD_CRASH_LOOP", message
			}
		}
	}
	return "", ""
}

func (a *DynamicManifestApplier) readWorkloadReadiness(ctx context.Context, namespace string, result *NamespaceReadiness) error {
	workloads := []struct {
		gvr  schema.GroupVersionResource
		kind string
	}{
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "deployment"},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, "statefulset"},
		{schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, "daemonset"},
	}
	for _, workload := range workloads {
		items, err := a.client.Resource(workload.gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, item := range items.Items {
			desired := int64FieldPath(item.Object, "spec", "replicas")
			if workload.kind == "daemonset" {
				desired = int64FieldPath(item.Object, "status", "desiredNumberScheduled")
			}
			if desired == 0 {
				continue
			}
			result.WorkloadCount++
			if isWorkloadReady(workload.kind, item.Object, desired) {
				result.ReadyWorkloads++
				continue
			}
			result.UnreadyWorkload = append(result.UnreadyWorkload, workload.kind+"/"+item.GetName())
		}
	}
	return nil
}

func isPodReady(object map[string]any) bool {
	conditions, _, _ := unstructured.NestedSlice(object, "status", "conditions")
	for _, item := range conditions {
		condition, _ := item.(map[string]any)
		if stringField(condition, "type") == "Ready" && stringField(condition, "status") == "True" {
			return true
		}
	}
	return false
}

func isWorkloadReady(kind string, object map[string]any, desired int64) bool {
	switch kind {
	case "daemonset":
		return int64FieldPath(object, "status", "numberReady") >= desired &&
			int64FieldPath(object, "status", "updatedNumberScheduled") >= desired
	default:
		return int64FieldPath(object, "status", "readyReplicas") >= desired &&
			int64FieldPath(object, "status", "availableReplicas") >= desired
	}
}

func stringField(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(values, key); value != "" {
			return value
		}
	}
	return ""
}

func intField(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func int64FieldPath(values map[string]any, fields ...string) int64 {
	value, _, _ := unstructured.NestedInt64(values, fields...)
	return value
}
