package wsclient

import (
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/kube"
)

func TestTaskLedgerPersistsPendingDrillCleanupAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	ledger, err := newTaskLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	task := map[string]any{"taskId": "task-1", "type": "drill"}
	object := taskLedgerObject{APIVersion: "velero.io/v1", Kind: "Restore", Namespace: "openshift-adp", Name: "restore-1"}
	if err := ledger.recordTask("task-1", "command-1", "", task, object); err != nil {
		t.Fatal(err)
	}
	if err := ledger.markCleanupPending("task-1", true); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newTaskLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending := reloaded.cleanupPendingRecords()
	if len(pending) != 1 || pending[0].TaskID != "task-1" {
		t.Fatalf("pending cleanup records = %#v, want task-1", pending)
	}
	if got := reloaded.recoverableRecords(); len(got) != 0 {
		t.Fatalf("cleanup-pending task must not also resume normal polling: %#v", got)
	}
	if err := reloaded.markCleanupPending("task-1", false); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.cleanupPendingRecords(); len(got) != 0 {
		t.Fatalf("completed cleanup remained pending: %#v", got)
	}
}

func TestIsTransientRestoreReconcileConflict(t *testing.T) {
	tests := []struct {
		name   string
		status kube.ManifestStatus
		want   bool
	}{
		{
			name: "Velero API interruption conflict",
			status: kube.ManifestStatus{
				Phase:  "Failed",
				Reason: "Restore from previous reconcile still in progress. The API Server may have been down.",
			},
			want: true,
		},
		{
			name: "ordinary restore failure",
			status: kube.ManifestStatus{
				Phase:   "Failed",
				Message: "pod volume restore failed",
			},
		},
		{
			name: "non-terminal phase with matching text",
			status: kube.ManifestStatus{
				Phase:  "InProgress",
				Reason: "previous reconcile still in progress",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientRestoreReconcileConflict(tc.status); got != tc.want {
				t.Fatalf("isTransientRestoreReconcileConflict() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRestoreResultReadFailed(t *testing.T) {
	if !restoreResultReadFailed(kube.RestoreResultSummary{Errors: []string{"failed to read Velero restore results: connection refused"}}) {
		t.Fatal("expected secondary restore-results read error to be recognized")
	}
	if restoreResultReadFailed(kube.RestoreResultSummary{Errors: []string{"failed to restore Deployment"}, ErrorCount: 1}) {
		t.Fatal("a real restore error must not be classified as a read error")
	}
	if restoreResultReadFailed(kube.RestoreResultSummary{Errors: []string{"failed to read Velero restore results: timeout", "another error"}}) {
		t.Fatal("multiple restore errors must not be hidden")
	}
}
