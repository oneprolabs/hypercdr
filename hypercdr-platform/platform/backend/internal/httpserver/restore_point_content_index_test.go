package httpserver

import (
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func TestRestorePointContentIndexRoundTrip(t *testing.T) {
	indexedAt := time.Date(2026, 8, 3, 8, 0, 0, 0, time.UTC)
	point := store.RestorePoint{Metadata: map[string]any{"contentIndex": restorePointIndex{
		Status:    "ready",
		IndexedAt: indexedAt,
		Resources: []protocol.BackupResourceSummary{{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "web", Group: "apps", Resource: "deployments"}},
	}}}
	index, ok := restorePointContentIndex(point)
	if !ok || index.Status != "ready" || !index.IndexedAt.Equal(indexedAt) || len(index.Resources) != 1 || index.Resources[0].Name != "web" {
		t.Fatalf("index=%+v ok=%v", index, ok)
	}
}

func TestRestorePointContentIndexRejectsMissingOrInvalidIndex(t *testing.T) {
	for _, point := range []store.RestorePoint{
		{},
		{Metadata: map[string]any{"contentIndex": map[string]any{"resources": []any{}}}},
	} {
		if index, ok := restorePointContentIndex(point); ok {
			t.Fatalf("unexpected index: %+v", index)
		}
	}
}

func TestNormalizeBackupResourceSummariesRepairsLegacyVeleroPaths(t *testing.T) {
	resources := normalizeBackupResourceSummaries([]protocol.BackupResourceSummary{
		{APIVersion: "actions.kio.kasten.io/v1alpha1", Kind: "BackupAction", Namespace: "demo", Name: "manual", Group: "backupactions.actions.kio.kasten.io", Resource: "namespaces"},
		{APIVersion: "v1", Kind: "Pod", Namespace: "demo", Name: "web", Group: "pods", Resource: "namespaces"},
		{APIVersion: "actions.kio.kasten.io/v1alpha1", Kind: "BackupAction", Namespace: "demo", Name: "manual", Group: "actions.kio.kasten.io", Resource: "backupactions"},
	})
	if len(resources) != 2 {
		t.Fatalf("resources=%+v", resources)
	}
	if resources[0].Group != "actions.kio.kasten.io" || resources[0].Resource != "backupactions" {
		t.Fatalf("custom resource=%+v", resources[0])
	}
	if resources[1].Group != "" || resources[1].Resource != "pods" {
		t.Fatalf("core resource=%+v", resources[1])
	}
}
