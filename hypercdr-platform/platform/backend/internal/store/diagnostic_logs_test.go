package store

import (
	"testing"
	"time"
)

func TestDiagnosticLogTenantFilteringAndRedaction(t *testing.T) {
	s := NewMemoryStore()
	_, _ = s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: "tenant-a", Scope: "tenant", Level: "error", Component: "platform-api", Message: "A failed", Details: map[string]any{"password": "bad", "nested": map[string]any{"secretAccessKey": "hidden"}}})
	_, _ = s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: "tenant-b", Scope: "tenant", Level: "info", Component: "task", Message: "B completed"})
	items, err := s.ListDiagnosticLogs(DiagnosticLogFilter{TenantID: "tenant-a", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TenantID != "tenant-a" {
		t.Fatalf("unexpected tenant result: %#v", items)
	}
	if items[0].Details["password"] != "[REDACTED]" {
		t.Fatalf("password was not redacted: %#v", items[0].Details)
	}
	nested := items[0].Details["nested"].(map[string]any)
	if nested["secretAccessKey"] != "[REDACTED]" {
		t.Fatalf("nested secret was not redacted: %#v", nested)
	}
}

func TestTaskEventCreatesCorrelatedDiagnosticLog(t *testing.T) {
	s := NewMemoryStore()
	task, err := s.CreateTask(TaskInput{ClusterID: "cluster-a", Type: "backup", Status: "running", CommandID: "command-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddTaskEvent(TaskEventInput{TaskID: task.ID, Level: "error", Reason: "BACKUP_FAILED", Message: "backup failed"}); err != nil {
		t.Fatal(err)
	}
	items, _ := s.ListDiagnosticLogs(DiagnosticLogFilter{TaskID: task.ID, From: time.Now().Add(-time.Minute)})
	if len(items) != 1 || items[0].ClusterID != "cluster-a" || items[0].CommandID != "command-a" || items[0].ErrorCode != "BACKUP_FAILED" {
		t.Fatalf("unexpected correlated log: %#v", items)
	}
}

func TestDiagnosticLogSourceFiltering(t *testing.T) {
	s := NewMemoryStore()
	_, _ = s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: "tenant-a", Component: "platform-api", Message: "platform"})
	_, _ = s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: "tenant-a", Component: "velero", Message: "cluster"})
	platform, _ := s.ListDiagnosticLogs(DiagnosticLogFilter{TenantID: "tenant-a", Source: "platform"})
	cluster, _ := s.ListDiagnosticLogs(DiagnosticLogFilter{TenantID: "tenant-a", Source: "cluster"})
	if len(platform) != 1 || platform[0].Component != "platform-api" || len(cluster) != 1 || cluster[0].Component != "velero" {
		t.Fatalf("source filtering failed: platform=%#v cluster=%#v", platform, cluster)
	}
}

func TestDiagnosticLogRetentionPurgesExpiredEventsAndAdjustsCoverage(t *testing.T) {
	s := NewMemoryStore()
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	old, _ := s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: "tenant-a", Component: "comm-agent", Message: "old", EventAt: now.AddDate(0, -7, 0)})
	_, _ = s.CreateDiagnosticLog(DiagnosticLogInput{TenantID: "tenant-a", Component: "comm-agent", Message: "current", EventAt: now.Add(-time.Hour)})
	_, _ = s.UpsertClusterLogCoverage(ClusterLogCoverageInput{ClusterID: "cluster-a", TenantID: "tenant-a", Component: "comm-agent", CoveredFrom: old.EventAt, CoveredTo: now, CollectedAt: now})
	cutoff := now.Add(-180 * 24 * time.Hour)
	removed, err := s.PurgeDiagnosticLogs(cutoff)
	if err != nil || removed != 1 {
		t.Fatalf("purge removed=%d err=%v", removed, err)
	}
	items, _ := s.ListDiagnosticLogs(DiagnosticLogFilter{TenantID: "tenant-a"})
	if len(items) != 1 || items[0].Message != "current" {
		t.Fatalf("unexpected retained logs: %#v", items)
	}
	coverage, ok, _ := s.GetClusterLogCoverage("cluster-a", "comm-agent")
	if !ok || !coverage.CoveredFrom.Equal(cutoff) {
		t.Fatalf("coverage was not aligned to retention cutoff: %#v", coverage)
	}
}

func TestClusterLogCoverageDoesNotMergeAcrossOfflineGap(t *testing.T) {
	s := NewMemoryStore()
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	_, _ = s.UpsertClusterLogCoverage(ClusterLogCoverageInput{ClusterID: "cluster-a", TenantID: "tenant-a", Component: "velero", CoveredFrom: now.Add(-72 * time.Hour), CoveredTo: now.Add(-48 * time.Hour), CollectedAt: now.Add(-48 * time.Hour)})
	coverage, _ := s.UpsertClusterLogCoverage(ClusterLogCoverageInput{ClusterID: "cluster-a", TenantID: "tenant-a", Component: "velero", CoveredFrom: now.Add(-24 * time.Hour), CoveredTo: now, CollectedAt: now})
	if !coverage.CoveredFrom.Equal(now.Add(-24 * time.Hour)) {
		t.Fatalf("offline gap was incorrectly marked covered: %#v", coverage)
	}
}

func TestTruncatedClusterLogCoverageKeepsShortArchiveCycle(t *testing.T) {
	s := NewMemoryStore()
	now := time.Date(2026, 7, 27, 3, 0, 0, 0, time.UTC)
	_, _ = s.UpsertClusterLogCoverage(ClusterLogCoverageInput{ClusterID: "cluster-a", TenantID: "tenant-a", Component: "velero", CoveredFrom: now.Add(-time.Hour), CoveredTo: now, CollectedAt: now, Truncated: true})
	coverage, _ := s.UpsertClusterLogCoverage(ClusterLogCoverageInput{ClusterID: "cluster-a", TenantID: "tenant-a", Component: "velero", CoveredFrom: now.Add(-5 * time.Minute), CoveredTo: now.Add(15 * time.Minute), CollectedAt: now.Add(15 * time.Minute), Truncated: false})
	if !coverage.Truncated {
		t.Fatal("high-volume component lost its shortened archive-cycle marker")
	}
}
