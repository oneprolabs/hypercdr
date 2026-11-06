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

func TestVolumeProgressIncludesSourceAndStartTimestamp(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "pvb-a"},
		"spec":     map[string]any{"volume": "data", "pod": map[string]any{"name": "app-0", "namespace": "demo"}},
		"status":   map[string]any{"phase": "InProgress", "startTimestamp": "2026-11-05T02:00:00Z"},
	}}
	progress := volumeProgressFromItem("PodVolumeBackup", item)
	if progress.Volume != "data" || progress.Pod != "app-0" || progress.Namespace != "demo" || progress.StartedAt != "2026-11-05T02:00:00Z" {
		t.Fatalf("unexpected source and start metadata: %#v", progress)
	}
	item.Object["spec"] = map[string]any{"sourcePVC": "claim-a", "sourceNamespace": "demo"}
	progress = volumeProgressFromItem("DataUpload", item)
	if progress.Volume != "claim-a" || progress.Namespace != "demo" {
		t.Fatalf("unexpected upload source: %#v", progress)
	}
}

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

func TestRestoreVolumeProgressKeepsQueuedVolumeAfterGracePeriod(t *testing.T) {
	progress := restoreVolumeProgressForAge(t, restoreVolumeStartGrace+time.Minute, false)
	got := progress.Items[0]
	if got.Phase != "" || got.ErrorCode != "" {
		t.Fatalf("item = %#v, want queued volume to remain retryable", got)
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

func TestBackupVolumeProgressCompletenessChangesWhenAnotherVolumeAppears(t *testing.T) {
	backupResource := schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}
	uploadResource := schema.GroupVersionResource{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}
	listKinds := map[schema.GroupVersionResource]string{backupResource: "PodVolumeBackupList", uploadResource: "DataUploadList"}
	volume := func(name string, totalBytes int64) runtime.Object {
		status := map[string]any{"phase": "New"}
		if totalBytes > 0 {
			status = map[string]any{"phase": "Completed", "progress": map[string]any{"bytesDone": totalBytes, "totalBytes": totalBytes}}
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v1", "kind": "PodVolumeBackup",
			"metadata": map[string]any{"name": name, "namespace": "hypercdr-agent", "labels": map[string]any{"velero.io/backup-name": "backup-progress"}},
			"status":   status,
		}}
	}
	for _, sample := range []struct {
		name       string
		volumes    []runtime.Object
		allKnown   bool
		totalBytes int64
	}{
		{"first volume discovered", []runtime.Object{volume("first", 0)}, false, 0},
		{"first volume complete", []runtime.Object{volume("first", 852587767)}, true, 852587767},
		{"second volume queued", []runtime.Object{volume("first", 852587767), volume("second", 0)}, false, 852587767},
		{"both volumes complete", []runtime.Object{volume("first", 852587767), volume("second", 73608033)}, true, 926195800},
	} {
		t.Run(sample.name, func(t *testing.T) {
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, sample.volumes...)
			progress, err := NewDynamicManifestApplierWithClient(client).GetBackupVolumeProgress(context.Background(), "hypercdr-agent", "backup-progress")
			if err != nil {
				t.Fatal(err)
			}
			if progress.AllTotalsKnown != sample.allKnown || progress.TotalBytes != sample.totalBytes || len(progress.Items) != len(sample.volumes) {
				t.Fatalf("progress = %#v", progress)
			}
			t.Logf("items=%d allTotalsKnown=%v totalBytes=%d unknownVolumes=%d", len(progress.Items), progress.AllTotalsKnown, progress.TotalBytes, progress.UnknownTotalCount)
		})
	}
}

func TestBackupVolumeDiscoveryRequiresMatchingIdentityAndCount(t *testing.T) {
	for _, sample := range []struct {
		marker   string
		complete bool
	}{{"uid-a:1", true}, {"old-uid:1", false}, {"uid-a:2", false}, {"", false}} {
		backup := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v1", "kind": "Backup",
			"metadata": map[string]any{"name": "backup-a", "namespace": "velero", "uid": "uid-a", "annotations": map[string]any{"hypercdr.io/pod-volume-discovery": sample.marker}},
		}}
		volume := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "velero.io/v1", "kind": "PodVolumeBackup",
			"metadata": map[string]any{"name": "pvb-a", "namespace": "velero", "labels": map[string]any{"velero.io/backup-name": "backup-a"}},
		}}
		kinds := map[schema.GroupVersionResource]string{
			{Group: "velero.io", Version: "v1", Resource: "podvolumebackups"}:  "PodVolumeBackupList",
			{Group: "velero.io", Version: "v2alpha1", Resource: "datauploads"}: "DataUploadList",
		}
		client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), kinds, backup, volume)
		progress, err := NewDynamicManifestApplierWithClient(client).GetBackupVolumeProgress(context.Background(), "velero", "backup-a")
		if err != nil || progress.DiscoveryComplete != sample.complete {
			t.Fatalf("marker %q: progress=%#v err=%v", sample.marker, progress, err)
		}
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
