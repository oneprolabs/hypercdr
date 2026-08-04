package wsclient

import (
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestWithRecoveryStagesMarksReadinessFailureWithoutRegressingRestore(t *testing.T) {
	task := protocol.TaskDispatchPayload{Type: "drill"}
	payload := withRecoveryStages(task, map[string]any{}, 0, "failed", "RESTORE_WORKLOAD_IMAGE_PULL_FAILED", "image pull failed")
	stages, ok := payload["recoveryStages"].([]map[string]any)
	if !ok || len(stages) != 6 {
		t.Fatalf("recoveryStages = %#v, want six stage snapshots", payload["recoveryStages"])
	}
	want := map[string]string{
		"preparing_restore": "succeeded", "restoring_resources": "succeeded",
		"restoring_data": "succeeded", "waiting_for_workloads": "failed",
		"application_validation": "pending", "finalizing_drill": "pending",
	}
	for _, stage := range stages {
		id, _ := stage["id"].(string)
		if got := stage["status"]; got != want[id] {
			t.Errorf("stage %s status = %v, want %s", id, got, want[id])
		}
	}
}

func TestWithRecoveryStagesMarksSuccessfulRecovery(t *testing.T) {
	task := protocol.TaskDispatchPayload{Type: "restore"}
	payload := withRecoveryStages(task, nil, 100, "succeeded", "", "complete")
	stages := payload["recoveryStages"].([]map[string]any)
	for _, stage := range stages {
		want := "succeeded"
		if stage["id"] == "application_validation" {
			want = "skipped"
		}
		if stage["status"] != want {
			t.Errorf("stage %v status = %v, want %s", stage["id"], stage["status"], want)
		}
	}
}

func TestWithRecoveryStagesMarksPersistentDataNotApplicableFromCatalog(t *testing.T) {
	task := protocol.TaskDispatchPayload{Type: "drill", Restore: &protocol.RestoreCommand{ContentCatalogLoaded: true, PersistentDataExpected: false, WaitForWorkloads: true}}
	payload := withRecoveryStages(task, nil, 100, "succeeded", "", "complete")
	stages := payload["recoveryStages"].([]map[string]any)
	for _, stage := range stages {
		if stage["id"] == "restoring_data" && stage["status"] != "not_applicable" {
			t.Fatalf("restoring_data status = %v, want not_applicable", stage["status"])
		}
	}
}

func TestWithRecoveryStagesLeavesBackupPayloadUnchanged(t *testing.T) {
	task := protocol.TaskDispatchPayload{Type: "backup"}
	payload := map[string]any{"kind": "Backup"}
	if got := withRecoveryStages(task, payload, 20, "running", "", ""); got["recoveryStages"] != nil {
		t.Fatalf("backup unexpectedly received recovery stages: %#v", got)
	}
}
