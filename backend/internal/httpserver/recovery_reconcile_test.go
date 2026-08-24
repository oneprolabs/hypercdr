package httpserver

import (
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestRecoveryTaskTimedOut(t *testing.T) {
	now := time.Date(2026, 8, 24, 7, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		task store.Task
		want bool
	}{
		{"stalled drill", store.Task{Type: "drill", Status: "running", Payload: map[string]any{"lastStatusAt": now.Add(-15 * time.Minute).Format(time.RFC3339Nano)}}, true},
		{"active restore", store.Task{Type: "restore", Status: "running", Payload: map[string]any{"lastStatusAt": now.Add(-14 * time.Minute).Format(time.RFC3339Nano)}}, false},
		{"legacy task", store.Task{Type: "drill", Status: "running", Payload: map[string]any{}}, false},
		{"terminal drill", store.Task{Type: "drill", Status: "failed", Payload: map[string]any{"lastStatusAt": now.Add(-time.Hour).Format(time.RFC3339Nano)}}, false},
		{"unrelated backup", store.Task{Type: "backup", Status: "running", Payload: map[string]any{"lastStatusAt": now.Add(-time.Hour).Format(time.RFC3339Nano)}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recoveryTaskTimedOut(test.task, now); got != test.want {
				t.Fatalf("recoveryTaskTimedOut() = %v, want %v", got, test.want)
			}
		})
	}
}
