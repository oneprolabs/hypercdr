package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCacheUsagePersistencePreservesFailureAndRejectsOlderReports(t *testing.T) {
	store := newTestStore(t)
	token, err := store.CreateAgentToken(DefaultTenantID, "", "cache-usage", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _, err := store.RegisterCluster(RegisterClusterInput{Token: token.Token, ClusterName: "cache-usage", AgentVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	used := int64(1234)
	write := func(report cacheUsageReport) {
		t.Helper()
		raw, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveClusterCacheUsage(cluster.ID, payload); err != nil {
			t.Fatal(err)
		}
	}
	initial := cacheUsageReport{Phase: "succeeded", ObservedAt: now, Nodes: []nodeCacheUsage{{Name: "worker", Index: nodeCacheMeasurement{Path: "/cache/index", UsedBytes: &used, ObservedAt: now.Format(time.RFC3339Nano)}}}}
	write(initial)
	write(cacheUsageReport{Phase: "failed", ObservedAt: now.Add(time.Second), Error: "offline"})
	write(initial)
	var raw []byte
	if err := store.db.QueryRow(`select metadata->'cacheUsage' from clusters where id = $1`, cluster.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved cacheUsageReport
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Phase != "failed" || len(saved.Nodes) != 1 || saved.Nodes[0].Index.UsedBytes == nil || *saved.Nodes[0].Index.UsedBytes != used || saved.Nodes[0].Index.ObservedAt != initial.Nodes[0].Index.ObservedAt {
		t.Fatalf("measurement not retained: %s", raw)
	}
}

func TestMergeCacheUsagePreservesOnlyMatchingPath(t *testing.T) {
	used := int64(1024)
	previous := nodeCacheMeasurement{Path: "/cache/index", UsedBytes: &used, ObservedAt: "2026-11-13T00:00:00Z"}
	for _, test := range []struct {
		name     string
		path     string
		preserve bool
	}{
		{name: "same directory", path: "/cache/index", preserve: true},
		{name: "offline unknown directory", preserve: true},
		{name: "changed directory", path: "/cache/new"},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := mergeCacheUsage(previous, nodeCacheMeasurement{Path: test.path, Error: "node unavailable"})
			if (current.UsedBytes != nil) != test.preserve {
				t.Fatalf("unexpected preserved measurement: %+v", current)
			}
			if test.preserve && (current.ObservedAt != previous.ObservedAt || *current.UsedBytes != used) {
				t.Fatal("previous measurement changed")
			}
		})
	}
	zero := int64(0)
	current := mergeCacheUsage(previous, nodeCacheMeasurement{Path: previous.Path, UsedBytes: &zero})
	if current.UsedBytes == nil || *current.UsedBytes != 0 {
		t.Fatal("valid zero was replaced")
	}
}

func TestValidateCacheUsageRejectsInvalidMeasurements(t *testing.T) {
	now := time.Now().UTC()
	used := int64(10)
	valid := func() cacheUsageReport {
		return cacheUsageReport{Phase: "succeeded", ObservedAt: now, Nodes: []nodeCacheUsage{{Name: "worker", Index: nodeCacheMeasurement{Path: "/cache/index", UsedBytes: &used, ObservedAt: now.Format(time.RFC3339Nano)}}}}
	}
	if err := validateCacheUsage(valid()); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*cacheUsageReport){
		func(report *cacheUsageReport) { report.Nodes[0].Index.Path = "/" },
		func(report *cacheUsageReport) { report.Nodes[0].Index.ObservedAt = "invalid" },
		func(report *cacheUsageReport) { report.Nodes = append(report.Nodes, report.Nodes[0]) },
		func(report *cacheUsageReport) { report.Phase = "unknown" },
		func(report *cacheUsageReport) { report.Nodes[0].Index.MaxBytes = -1 },
	} {
		report := valid()
		mutate(&report)
		if validateCacheUsage(report) == nil {
			t.Fatalf("invalid report accepted: %+v", report)
		}
	}
}
