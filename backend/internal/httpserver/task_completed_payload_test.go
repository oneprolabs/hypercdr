package httpserver

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
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

func TestTerminalPayloadPatchesClearRunningSizeProgress(t *testing.T) {
	completed := taskCompletedPayloadPatch(protocol.TaskCompletedPayload{})
	if value, exists := completed["sizeProgressV2"]; !exists || value != nil {
		t.Fatalf("completed sizeProgressV2 = %#v, want explicit nil clear marker", value)
	}
	failed := taskFailurePayloadPatch(nil)
	if value, exists := failed["sizeProgressV2"]; !exists || value != nil {
		t.Fatalf("failed sizeProgressV2 = %#v, want explicit nil clear marker", value)
	}
}

func TestCreateRestorePointFromBackupPersistsTypedSizeMetricsV2(t *testing.T) {
	repo := store.NewMemoryStore()
	router := &Router{
		store:             repo,
		logger:            slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})),
		hub:               newSessionHub(),
		contentIndexing:   map[string]struct{}{},
		contentIndexSlots: make(chan struct{}, 2),
	}
	plan, err := repo.CreateProtectionPlan(store.ProtectionPlanInput{
		SourceClusterID: "cluster-1",
		AppID:           "app-1",
		Status:          "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	metrics := &protocol.SizeMetricsV2{
		SchemaVersion:     2,
		Operation:         "backup",
		MeasurementStatus: "complete",
		Logical:           protocol.SizeValueV2{TotalBytes: 1024, Known: true},
		NewData:           protocol.SizeValueV2{TotalBytes: 0, Known: true},
		AllVolumesKnown:   true,
		Reuse:             protocol.SizeReuseV2{Ratio: 1, Status: "known"},
		MeasuredAt:        time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC),
	}
	patch := taskCompletedPayloadPatch(protocol.TaskCompletedPayload{SizeMetricsV2: metrics})
	point, err := router.createRestorePointFromBackup(store.Task{
		ID:               "task-1",
		ClusterID:        "cluster-1",
		AppID:            "app-1",
		ProtectionPlanID: plan.ID,
		Payload:          patch,
		CreatedAt:        time.Now().UTC(),
	}, map[string]any{"kind": "Backup", "name": "backup-1"})
	if err != nil {
		t.Fatal(err)
	}
	if point.SizeMetricsV2["schemaVersion"] != float64(2) {
		t.Fatalf("sizeMetricsV2 = %#v, want typed completion metrics converted and persisted", point.SizeMetricsV2)
	}
	newData := mapFromAny(point.SizeMetricsV2["newData"])
	if known, ok := newData["known"].(bool); !ok || !known {
		t.Fatalf("newData = %#v, want known zero preserved", newData)
	}
	if got := int64FromAny(newData["totalBytes"]); got != 0 {
		t.Fatalf("newData.totalBytes = %d, want 0", got)
	}
}
