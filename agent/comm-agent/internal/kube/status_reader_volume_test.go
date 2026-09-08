package kube

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestRestoreVolumeProgressObservesEmptyPhaseDuringGracePeriod(t *testing.T) {
	progress := restoreVolumeProgressForAge(t, restoreVolumeStartGrace-time.Minute, true)
	if progress.FailedCount != 0 || len(progress.Items) != 1 {
		t.Fatalf("progress = %#v, want one retryable item", progress)
	}
	if got := progress.Items[0]; got.Phase != "" || got.ErrorCode != "" {
		t.Fatalf("item = %#v, want empty retryable phase and error code", got)
	}
}

func TestRestoreVolumeProgressReportsMissingPVCOnlyAfterGracePeriod(t *testing.T) {
	progress := restoreVolumeProgressForAge(t, restoreVolumeStartGrace+time.Minute, true)
	got := progress.Items[0]
	if got.Phase != "FailedValidation" || got.ErrorCode != "RESTORE_VOLUME_DEPENDENCY_MISSING" {
		t.Fatalf("item = %#v, want missing dependency failure", got)
	}
	if !strings.Contains(got.Message, "claim-a") {
		t.Fatalf("message = %q, want PVC name", got.Message)
	}
}

func TestRestoreVolumeProgressReportsStartTimeoutAfterGracePeriod(t *testing.T) {
	progress := restoreVolumeProgressForAge(t, restoreVolumeStartGrace+time.Minute, false)
	got := progress.Items[0]
	if got.Phase != "FailedValidation" || got.ErrorCode != "RESTORE_VOLUME_START_TIMEOUT" {
		t.Fatalf("item = %#v, want start timeout", got)
	}
	if got.ElapsedSeconds < int64(restoreVolumeStartGrace/time.Second) {
		t.Fatalf("elapsedSeconds = %d, want at least grace period", got.ElapsedSeconds)
	}
}

func TestRestoreVolumeProgressUsesBackupSnapshotTotalWhileQueued(t *testing.T) {
	pvrGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumerestores"}
	pvbGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}
	dataDownloadGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datadownloads"}
	listKinds := map[schema.GroupVersionResource]string{
		pvrGVR:          "PodVolumeRestoreList",
		pvbGVR:          "PodVolumeBackupList",
		dataDownloadGVR: "DataDownloadList",
	}
	pvr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "velero.io/v1", "kind": "PodVolumeRestore",
		"metadata": map[string]any{
			"name": "restore-a-pvr", "namespace": "hypercdr-agent",
			"creationTimestamp": metav1.Now().Format(time.RFC3339),
			"labels":            map[string]any{"velero.io/restore-name": "restore-a"},
		},
		"spec": map[string]any{"snapshotID": "snapshot-a"},
	}}
	pvb := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "velero.io/v1", "kind": "PodVolumeBackup",
		"metadata": map[string]any{"name": "backup-a-pvb", "namespace": "hypercdr-agent"},
		"status": map[string]any{
			"snapshotID": "snapshot-a",
			"progress":   map[string]any{"bytesDone": int64(4096), "totalBytes": int64(4096)},
		},
	}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, pvr, pvb)
	progress, err := NewDynamicManifestApplierWithClient(client).GetRestoreVolumeProgress(context.Background(), "hypercdr-agent", "restore-a")
	if err != nil {
		t.Fatal(err)
	}
	if !progress.AllTotalsKnown || progress.TotalBytes != 4096 || !progress.Items[0].KnownTotal {
		t.Fatalf("progress = %#v, want queued restore total from matching backup snapshot", progress)
	}
}

func restoreVolumeProgressForAge(t *testing.T, age time.Duration, missingPVC bool) VolumeProgress {
	t.Helper()
	pvrGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumerestores"}
	dataDownloadGVR := schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datadownloads"}
	listKinds := map[schema.GroupVersionResource]string{
		pvrGVR: "PodVolumeRestoreList",
		{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}: "PodVolumeBackupList",
		dataDownloadGVR:                                     "DataDownloadList",
		{Version: "v1", Resource: "pods"}:                   "PodList",
		{Version: "v1", Resource: "persistentvolumeclaims"}: "PersistentVolumeClaimList",
	}
	pvr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "velero.io/v1",
		"kind":       "PodVolumeRestore",
		"metadata": map[string]any{
			"name":              "restore-a-pvr",
			"namespace":         "hypercdr-agent",
			"creationTimestamp": metav1.NewTime(time.Now().Add(-age)).Format(time.RFC3339),
			"labels":            map[string]any{"velero.io/restore-name": "restore-a"},
		},
		"spec": map[string]any{"pod": map[string]any{"namespace": "demo-drill", "name": "app-a"}, "volume": "data"},
	}}
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": "app-a", "namespace": "demo-drill"},
		"spec":     map[string]any{"volumes": []any{map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "claim-a"}}}},
	}}
	objects := []runtime.Object{pvr, pod}
	if !missingPVC {
		objects = append(objects, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "PersistentVolumeClaim",
			"metadata": map[string]any{"name": "claim-a", "namespace": "demo-drill"},
		}})
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
	applier := NewDynamicManifestApplierWithClient(client)
	progress, err := applier.GetRestoreVolumeProgress(context.Background(), "hypercdr-agent", "restore-a")
	if err != nil {
		t.Fatal(err)
	}
	return progress
}
