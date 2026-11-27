package store

import (
	"reflect"
	"testing"
	"time"
)

func TestSnapshotNamespaceCapabilitiesPreserveIndependentFreshness(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	old := now.Add(-time.Hour).Format(time.RFC3339Nano)
	existing := []ClusterCapability{
		{Type: "CSISnapshotNamespaceInventory", Name: "other", Fields: map[string]string{"namespace": "other", "collectedAt": old}},
		{Type: "CSISnapshotVolumePrerequisite", Name: "stale", Fields: map[string]string{"namespace": "current"}},
		{Type: "VolumeSnapshotClass", Name: "old-class"},
	}
	incoming := []ClusterCapability{
		{Type: "CSISnapshotNamespaceInventory", Name: "current", Fields: map[string]string{"namespace": "current"}},
		{Type: "CSISnapshotVolumePrerequisite", Name: "new", Fields: map[string]string{"namespace": "current"}},
		{Type: "VolumeSnapshotClass", Name: "new-class"},
		{Type: "CSISnapshotVolumePrerequisite", Name: "out-of-scope", Fields: map[string]string{"namespace": "other"}},
	}
	merged := mergeSnapshotNamespaceCapabilities(existing, incoming, "current", now)
	if len(merged) != 4 || !reflect.DeepEqual(merged[0], existing[0]) || merged[1].Fields["collectedAt"] != now.Format(time.RFC3339Nano) || merged[2].Name != "new" || merged[3].Name != "new-class" {
		t.Fatalf("incorrect namespace merge: %+v", merged)
	}
	if incoming[0].Fields["collectedAt"] != "" {
		t.Fatal("merge mutated caller inventory")
	}
	cleared := mergeSnapshotNamespaceCapabilities(merged, nil, "current", now)
	if len(cleared) != 1 || cleared[0].Name != "other" {
		t.Fatal("legacy/missing namespace inventory retained stale current checks")
	}
}

func TestConsistencyWorkloadCapabilitiesRemainNamespaceScoped(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-time.Hour).Format(time.RFC3339Nano)
	existing := []ClusterCapability{
		{Type: "ConsistencyNamespaceInventory", Name: "other", Fields: map[string]string{"namespace": "other", "collectedAt": old}},
		{Type: "ConsistencyWorkload", Name: "writer", Fields: map[string]string{"namespace": "other", "uid": "other-uid", "collectedAt": old}},
		{Type: "ConsistencyWorkload", Name: "writer", Fields: map[string]string{"namespace": "current", "uid": "old-uid"}},
	}
	incoming := []ClusterCapability{
		{Type: "ConsistencyNamespaceInventory", Name: "current", Fields: map[string]string{"namespace": "current", "complete": "true"}},
		{Type: "ConsistencyWorkload", Name: "writer", Fields: map[string]string{"namespace": "current", "uid": "new-uid"}},
		{Type: "ConsistencyWorkload", Name: "injected", Fields: map[string]string{"namespace": "other"}},
	}
	merged := mergeSnapshotNamespaceCapabilities(existing, incoming, "current", now)
	if len(merged) != 4 || !reflect.DeepEqual(merged[:2], existing[:2]) || merged[3].Fields["uid"] != "new-uid" || merged[3].Fields["collectedAt"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("namespace identity or freshness was corrupted: %+v", merged)
	}
	if incoming[1].Fields["collectedAt"] != "" {
		t.Fatal("merge mutated the incoming report")
	}
	if cleared := mergeSnapshotNamespaceCapabilities(merged, nil, "current", now); !reflect.DeepEqual(cleared, existing[:2]) {
		t.Fatal("legacy report retained stale workload identities")
	}
}
