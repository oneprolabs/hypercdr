package httpserver

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestPersistedDRSupportGate(t *testing.T) {
	repo := newTestStore(t)
	clusterID := testClusterID(t, repo, "support-gate")
	router := &Router{store: repo, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, status := range []string{"supported", "unsupported", "unknown", ""} {
		_, _, err := repo.ApplyInventory(store.InventoryInput{
			ClusterID:   clusterID,
			CollectedAt: time.Now().UTC(),
			Apps:        []store.Application{{Namespace: "sample", Name: "sample", ResourceSummary: map[string]any{"drSupport": map[string]any{"status": status}}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		apps, err := repo.ListApplications(clusterID)
		if err != nil || len(apps) != 1 {
			t.Fatalf("applications: %#v, %v", apps, err)
		}
		blocked, _ := router.applicationDRSupportBlock(apps[0].ID, "protected")
		if blocked != (status != "supported") {
			t.Errorf("status %q: blocked=%v", status, blocked)
		}
		if blocked, _ := router.applicationDRSupportBlock(apps[0].ID, "unprotected"); blocked {
			t.Errorf("Move Back blocked for status %q", status)
		}
	}
	if blocked, _ := router.applicationDRSupportBlock(store.NewPublicID(), "protected"); !blocked {
		t.Error("missing application accepted")
	}
}
