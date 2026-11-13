package wsclient

import (
	"errors"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestRestoreSubmissionRecovery(t *testing.T) {
	for _, scenario := range []string{"missing", "exists", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			ledger, err := newTaskLedger(directory)
			if err != nil {
				t.Fatal(err)
			}
			task := protocol.TaskDispatchPayload{TaskID: "restore-task", CommandID: "command", Type: "restore"}
			object := taskLedgerObject{APIVersion: "velero.io/v1", Kind: "Restore", Namespace: "hypercdr-agent", Name: "restore-test"}
			if err := ledger.recordTask(task.TaskID, task.CommandID, "", task, object); err != nil {
				t.Fatal(err)
			}
			ledger, err = newTaskLedger(directory)
			if err != nil {
				t.Fatal(err)
			}
			if !ledger.recoverableRecords()[0].SubmissionPending {
				t.Fatal("pending submission lost across restart")
			}
			var statusErr error
			if scenario == "missing" {
				statusErr = apierrors.NewNotFound(schema.GroupResource{Group: "velero.io", Resource: "restores"}, object.Name)
			}
			if scenario == "unavailable" {
				statusErr = errors.New("API server unavailable")
			}
			client := &Client{ledger: ledger, statusReader: &sequenceManifestStatusReader{results: []manifestStatusResult{{err: statusErr}}}}
			resume, reconcileErr := client.reconcileRestoreSubmission(task, kube.AppliedObject{APIVersion: object.APIVersion, Kind: object.Kind, Namespace: object.Namespace, Name: object.Name})
			if resume != (scenario == "missing") || (reconcileErr != nil) != (scenario == "unavailable") {
				t.Fatalf("resume=%v err=%v", resume, reconcileErr)
			}
			ledger, err = newTaskLedger(directory)
			if err != nil {
				t.Fatal(err)
			}
			if ledger.recoverableRecords()[0].SubmissionPending != (scenario != "exists") {
				t.Fatal("incorrect submission state")
			}
		})
	}
}
