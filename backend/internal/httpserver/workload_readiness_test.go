package httpserver

import "testing"

func TestReadinessEventLevel(t *testing.T) {
	if readinessEventLevel(map[string]any{"readinessObservationError": "API unavailable"}) != "warning" {
		t.Fatal("observation errors need warnings")
	}
	if readinessEventLevel(map[string]any{"readiness": map[string]any{"Objects": []any{map[string]any{"State": "Starting", "Reason": "ImagePullBackOff"}}}}) != "warning" {
		t.Fatal("image backoff needs warning")
	}
	if readinessEventLevel(map[string]any{"readiness": map[string]any{"Objects": []any{map[string]any{"State": "Ready", "Reason": "ImagePullBackOff"}}}}) != "info" {
		t.Fatal("recovered objects must not retain warning level")
	}
}

func TestReadinessSnapshotPersistenceClearsObservationError(t *testing.T) {
	patch := backupTaskPayloadPatch(map[string]any{"readinessStage": "started", "readiness": map[string]any{"ReadyWorkloads": 1}, "readinessObservedAt": "2026-10-23T08:00:00Z"})
	if patch["readiness"] == nil || patch["readinessObservedAt"] == nil {
		t.Fatalf("snapshot missing: %#v", patch)
	}
	if value, exists := patch["readinessObservationError"]; !exists || value != nil {
		t.Fatalf("stale error not cleared: %#v", patch)
	}
}
