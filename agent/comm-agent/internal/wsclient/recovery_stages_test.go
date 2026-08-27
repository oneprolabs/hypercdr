package wsclient

import (
	"errors"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestRestoreSubmitErrorCodeIdentifiesStaleCleanupTimeout(t *testing.T) {
	got := restoreSubmitErrorCode(errors.New("timed out waiting for stale restore state to be deleted"))
	if got != "RESTORE_STALE_STATE_CLEANUP_TIMEOUT" {
		t.Fatalf("restoreSubmitErrorCode() = %q", got)
	}
	if got := restoreSubmitErrorCode(errors.New("admission rejected restore")); got != "RESTORE_SUBMIT_FAILED" {
		t.Fatalf("generic restore error code = %q", got)
	}
}

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

func TestWithRecoveryStagesClassifiesWorkloadVolumeMountFailureAsReadiness(t *testing.T) {
	task := protocol.TaskDispatchPayload{Type: "drill"}
	payload := withRecoveryStages(task, map[string]any{}, 0, "failed", "RESTORE_WORKLOAD_VOLUME_MOUNT_FAILED", "mount failed")
	stages := payload["recoveryStages"].([]map[string]any)
	for _, stage := range stages {
		if stage["id"] == "restoring_data" && stage["status"] != "succeeded" {
			t.Fatalf("restoring_data status = %v, want succeeded", stage["status"])
		}
		if stage["id"] == "waiting_for_workloads" && stage["status"] != "failed" {
			t.Fatalf("waiting_for_workloads status = %v, want failed", stage["status"])
		}
	}
}

func TestWithRecoveryStagesClassifiesVolumeDataPathFailures(t *testing.T) {
	for _, code := range []string{"RESTORE_VOLUME_FILESYSTEM_READ_ONLY", "RESTORE_VOLUME_DATA_PATH_FAILED", "RESTORE_VOLUME_PROGRESS_STALLED", "RESTORE_VELERO_STALLED"} {
		payload := withRecoveryStages(protocol.TaskDispatchPayload{Type: "drill"}, nil, 74, "failed", code, "restore failed")
		stages := payload["recoveryStages"].([]map[string]any)
		want := map[string]string{"preparing_restore": "succeeded", "restoring_resources": "succeeded", "restoring_data": "failed"}
		for _, stage := range stages {
			id := stage["id"].(string)
			if expected, ok := want[id]; ok && stage["status"] != expected {
				t.Errorf("code %s stage %s = %v, want %s", code, id, stage["status"], expected)
			}
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

func TestRestoreVolumeFailureDetailsPreservesStartTimeout(t *testing.T) {
	code, message := restoreVolumeFailureDetails(map[string]any{"items": []map[string]any{{
		"errorCode": "RESTORE_VOLUME_START_TIMEOUT",
		"message":   "pod volume restore pvr-a did not start within 10m0s",
	}}})
	if code != "RESTORE_VOLUME_START_TIMEOUT" || message != "pod volume restore pvr-a did not start within 10m0s" {
		t.Fatalf("got (%q, %q)", code, message)
	}
}
