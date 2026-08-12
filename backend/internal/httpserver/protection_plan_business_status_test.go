package httpserver

import "testing"

func TestProtectionPlanBusinessStatus(t *testing.T) {
	tests := map[string]string{
		"pending_activation": "configuring", "activating_storage": "configuring", "activating_schedule": "configuring",
		"active": "ready", "active_with_warning": "ready_with_warning",
		"storage_failed": "configuration_failed", "schedule_failed": "configuration_failed",
		"cleanup_running": "cleaning", "cleanup_failed": "cleanup_failed",
	}
	for input, want := range tests {
		if got := protectionPlanBusinessStatus(input); got != want {
			t.Errorf("status %q = %q, want %q", input, got, want)
		}
	}
}
