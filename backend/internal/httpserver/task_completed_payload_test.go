package httpserver

import (
	"testing"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestTaskCompletedPayloadPatchClearsStaleVolumeProgress(t *testing.T) {
	patch := taskCompletedPayloadPatch(protocol.TaskCompletedPayload{
		Velero: map[string]any{
			"restorePointSize": map[string]any{"totalBytes": float64(42)},
		},
	})

	value, exists := patch["volumeProgress"]
	if !exists {
		t.Fatal("volumeProgress clear marker is missing")
	}
	if value != nil {
		t.Fatalf("volumeProgress = %#v, want nil clear marker", value)
	}
}

func TestTaskCompletedPayloadPatchPreservesFinalVolumeProgress(t *testing.T) {
	final := map[string]any{"percent": float64(100), "completedCount": float64(1)}
	patch := taskCompletedPayloadPatch(protocol.TaskCompletedPayload{
		Velero: map[string]any{"volumeProgress": final},
	})

	got := mapFromAny(patch["volumeProgress"])
	if got["percent"] != float64(100) || got["completedCount"] != float64(1) {
		t.Fatalf("volumeProgress = %#v, want final progress", got)
	}
}
