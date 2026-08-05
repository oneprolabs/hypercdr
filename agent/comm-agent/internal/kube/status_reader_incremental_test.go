package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestVolumeProgressFromItemReadsIncrementalBytes(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "data-upload-1"},
		"status": map[string]any{
			"phase":            "Completed",
			"incrementalBytes": int64(4096),
			"progress": map[string]any{
				"bytesDone":  int64(16384),
				"totalBytes": int64(16384),
			},
		},
	}}

	progress := volumeProgressFromItem("DataUpload", item)
	if !progress.IncrementalKnown || progress.IncrementalBytes != 4096 {
		t.Fatalf("expected incremental bytes 4096, got known=%v bytes=%d", progress.IncrementalKnown, progress.IncrementalBytes)
	}
}

func TestVolumeProgressFromItemClampsNegativeIncrementalBytes(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{"incrementalBytes": int64(-27)},
	}}

	progress := volumeProgressFromItem("DataUpload", item)
	if !progress.IncrementalKnown || progress.IncrementalBytes != 0 {
		t.Fatalf("expected known negative incremental bytes to be clamped to zero, got known=%v bytes=%d", progress.IncrementalKnown, progress.IncrementalBytes)
	}
}

func TestVolumeProgressFromItemTreatsOmittedCompletedIncrementalBytesAsZero(t *testing.T) {
	item := unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{"phase": "Completed"},
	}}

	progress := volumeProgressFromItem("PodVolumeBackup", item)
	if !progress.IncrementalKnown || progress.IncrementalBytes != 0 {
		t.Fatalf("expected omitted completed incremental bytes to mean known zero, got known=%v bytes=%d", progress.IncrementalKnown, progress.IncrementalBytes)
	}
}
