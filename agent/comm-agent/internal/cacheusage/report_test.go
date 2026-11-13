package cacheusage

import "testing"

func TestFailedObservationsPreserveUsageOnlyForTheSamePath(t *testing.T) {
	used := int64(1024)
	previous := []Node{{Name: "node", Index: Cache{Path: "/old/index", UsedBytes: &used, ObservedAt: "previous"}, Drill: Cache{Path: "/old/drill", UsedBytes: &used, ObservedAt: "previous"}}}
	current := []Node{{Name: "node", Index: Cache{Path: "/old/index", Error: "permission denied"}, Drill: Cache{Path: "/new/drill", Error: "unavailable"}}}
	merged := PreservePreviousUsage(previous, current)
	if merged[0].Index.UsedBytes == nil || *merged[0].Index.UsedBytes != used || merged[0].Index.ObservedAt != "previous" {
		t.Fatal("last measured usage was lost")
	}
	if merged[0].Drill.UsedBytes != nil {
		t.Fatal("old directory usage was attributed to a new path")
	}
	current = []Node{{Name: "node", Index: Cache{Error: "node offline"}, Drill: Cache{Error: "node offline"}}}
	merged = PreservePreviousUsage(previous, current)
	if merged[0].Index.Path != "/old/index" || merged[0].Index.UsedBytes == nil || merged[0].Index.Error != "node offline" {
		t.Fatal("offline node lost its last observation")
	}
}

func TestMeasuredZeroReplacesOldUsage(t *testing.T) {
	used, zero := int64(1024), int64(0)
	merged := PreservePreviousUsage([]Node{{Name: "node", Index: Cache{Path: "/cache/index", UsedBytes: &used}}}, []Node{{Name: "node", Index: Cache{Path: "/cache/index", UsedBytes: &zero, ObservedAt: "new"}}})
	if merged[0].Index.UsedBytes == nil || *merged[0].Index.UsedBytes != 0 || merged[0].Index.ObservedAt != "new" {
		t.Fatal("valid zero usage did not replace old observation")
	}
}
