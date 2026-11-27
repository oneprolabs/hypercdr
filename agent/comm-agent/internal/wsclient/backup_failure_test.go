package wsclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type failureResultStatsReader struct {
	backupSizeStatsReader
	err error
}

func (reader failureResultStatsReader) GetBackupResultSummary(context.Context, string, string, string) (kube.RestoreResultSummary, error) {
	return kube.RestoreResultSummary{Key: "backup-results", ErrorCount: 1, Errors: []string{"error to list DataUpload: the server was unable to return a response in the time allotted"}}, reader.err
}

func TestBackupFailureEnrichesActualResultWithoutChangingOutcome(t *testing.T) {
	client := &Client{statsReader: failureResultStatsReader{}}
	payload := map[string]any{"storageLocation": "storage"}
	status := kube.ManifestStatus{Phase: "PartiallyFailed", Errors: 1}
	message, details := client.enrichVeleroFailure(context.Background(), kube.AppliedObject{Kind: "Backup", Name: "backup", Namespace: "agent"}, payload, status, "velero backup failed")
	code, _ := backupFailureDetails("BACKUP_FAILED", message, map[string]any{"velero": details})
	if code != "SYNC_API_READ_FAILED" || details["backupResults"] == nil || status.Phase != "PartiallyFailed" {
		t.Fatalf("lost failure evidence: %s %s %+v", code, message, details)
	}
	client.statsReader = failureResultStatsReader{err: errors.New("storage unavailable")}
	message, details = client.enrichVeleroFailure(context.Background(), kube.AppliedObject{Kind: "Backup", Name: "backup"}, map[string]any{"storageLocation": "storage"}, status, "velero backup failed")
	if details["backupResultsReadError"] == nil || details["backupResults"] != nil || message == "velero backup failed" {
		t.Fatalf("diagnostic read failure was hidden: %s %+v", message, details)
	}
}

func TestBackupFailureEventSurvivesDisconnection(t *testing.T) {
	directory := t.TempDir()
	outbox, err := newEventOutbox(directory)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, upgradeErr := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if upgradeErr == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	client := &Client{outbox: outbox, conn: connection}
	_ = client.sendTaskFailedWithDetails(protocol.TaskDispatchPayload{TaskID: "task", CommandID: "command", Type: "backup"}, "BACKUP_FAILED", "S3 AccessDenied", nil)
	reloaded, err := newEventOutbox(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.items) != 1 {
		t.Fatalf("failure was not persisted: %d", len(reloaded.items))
	}
	for _, raw := range reloaded.items {
		var event protocol.Message[protocol.TaskFailedPayload]
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.Payload.ErrorCode != "SYNC_STORAGE_AUTH_FAILED" || event.Payload.TaskID != "task" || event.Payload.Details["originalMessage"] != "S3 AccessDenied" {
			t.Fatalf("lost structured error: %+v", event.Payload)
		}
	}
}

func TestBackupFailureClassification(t *testing.T) {
	cases := []struct{ message, want string }{
		{"S3: AccessDenied", "SYNC_STORAGE_AUTH_FAILED"},
		{"no space left on device", "SYNC_STORAGE_CAPACITY_EXCEEDED"},
		{"x509: certificate signed by unknown authority", "SYNC_TLS_FAILED"},
		{"volumesnapshots is forbidden", "SYNC_API_PERMISSION_DENIED"},
		{"repository not initialized in the provided storage", "SYNC_REPOSITORY_UNAVAILABLE"},
		{"CSI snapshot execution is unavailable", "SYNC_CSI_RUNTIME_UNAVAILABLE"},
		{"snapshot prerequisites failed: no matching class", "SYNC_CSI_PREREQUISITES_FAILED"},
		{"failed to create snapshot: driver error", "SYNC_CSI_SNAPSHOT_FAILED"},
		{"FailedAttachVolume: multi-attach error", "SYNC_VOLUME_MOUNT_FAILED"},
		{"FailedScheduling: insufficient cpu", "SYNC_WORKER_RESOURCE_UNAVAILABLE"},
		{"volume: input/output error", "SYNC_SOURCE_VOLUME_UNAVAILABLE"},
		{"dial tcp: connection refused", "SYNC_NETWORK_UNAVAILABLE"},
		{"error to list DataUpload: the server was unable to return a response in the time allotted", "SYNC_API_READ_FAILED"},
		{"unrecognized vendor error", "SYNC_BACKUP_FAILED"},
	}
	for _, scenario := range cases {
		t.Run(scenario.want, func(t *testing.T) {
			original := map[string]any{"retained": true}
			code, details := backupFailureDetails("BACKUP_FAILED", scenario.message, original)
			if code != scenario.want || details["originalMessage"] != scenario.message || details["originalErrorCode"] != "BACKUP_FAILED" || details["retained"] != true {
				t.Fatalf("unexpected result: %s %+v", code, details)
			}
			if original["originalMessage"] != nil {
				t.Fatal("caller details mutated")
			}
		})
	}
}

func TestBackupFailurePreservesEvidenceAndUnknownCause(t *testing.T) {
	for _, code := range []string{"SNAPSHOT_COMPLETION_UNPROVEN", "LOCAL_LEDGER_WRITE_FAILED", "SPECIFIC_VENDOR_FAILURE"} {
		message := "unknown error"
		if code != "SPECIFIC_VENDOR_FAILURE" {
			message = "connection refused"
		}
		got, _ := backupFailureDetails(code, message, nil)
		if got != code {
			t.Fatalf("lost specific failure: %s", got)
		}
	}
	for code, want := range map[string]string{"TASK_DEADLINE_EXCEEDED": "SYNC_DEADLINE_EXCEEDED", "Backup_STATUS_TIMEOUT": "SYNC_DEADLINE_EXCEEDED", "Backup_STATUS_READ_FAILED": "SYNC_API_READ_FAILED"} {
		got, _ := backupFailureDetails(code, "unknown error", nil)
		if got != want {
			t.Fatalf("%s: %s", code, got)
		}
	}
}

func TestBackupFailureUsesOnlyFailedVolumeEvidence(t *testing.T) {
	items := []any{
		map[string]any{"phase": "Completed", "message": "AccessDenied was resolved"},
		map[string]any{"phase": "Failed", "namespace": "app", "volume": "data", "name": "upload", "message": "FailedMount"},
	}
	volume := map[string]any{"items": items}
	details := map[string]any{"velero": map[string]any{"volumeProgress": volume}}
	code, _ := backupFailureDetails("VOLUME_BACKUP_FAILED", "volume failed", details)
	if code != "SYNC_VOLUME_MOUNT_FAILED" {
		t.Fatal(code)
	}
	if got := backupVolumeFailureMessage(volume); got != "app/data (upload): FailedMount" {
		t.Fatal(got)
	}
}
