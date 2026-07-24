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
