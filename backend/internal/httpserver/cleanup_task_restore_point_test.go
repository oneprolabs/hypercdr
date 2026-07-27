package httpserver

import (
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestEnrichCleanupTaskRestorePointTimesFillsMissingValue(t *testing.T) {
	repo := store.NewMemoryStore()
	taskCreatedAt := time.Date(2026, time.July, 22, 18, 39, 29, 0, time.UTC)
	point, err := repo.CreateRestorePoint(store.RestorePointInput{
		SourceClusterID:  "cluster-1",
		VeleroBackupName: "backup-1",
		TaskCreatedAt:    taskCreatedAt,
	})
	if err != nil {
		t.Fatalf("CreateRestorePoint() error = %v", err)
	}

	router := &Router{store: repo}
	items := router.enrichCleanupTaskRestorePointTimes([]store.Task{{
		Type: "retention-cleanup",
		Payload: map[string]any{
			"restorePoints": []any{map[string]any{"id": point.ID}},
		},
	}})

	points := items[0].Payload["restorePoints"].([]any)
	got, ok := points[0].(map[string]any)["taskCreatedAt"].(time.Time)
	if !ok || !got.Equal(taskCreatedAt) {
		t.Fatalf("taskCreatedAt = %#v, want %v", got, taskCreatedAt)
	}
}
