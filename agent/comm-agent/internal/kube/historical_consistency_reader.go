package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hypercdr-platform/agent/comm-agent/internal/consistency"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var consistencyBackups = schema.GroupVersionResource{Group: "velero.io", Version: "v1", Resource: "backups"}

type consistencyTerminalError struct{ error }
type historicalConsistencyBackupReader struct {
	Applier   *DynamicManifestApplier
	Backup    *unstructured.Unstructured
	backupUID string
}

func (driver *historicalConsistencyBackupReader) readBackup(ctx context.Context, record consistency.Record) (*unstructured.Unstructured, error) {
	backup, err := driver.Applier.client.Resource(consistencyBackups).Namespace(driver.Backup.GetNamespace()).Get(ctx, driver.Backup.GetName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(backup.GetUID()) != driver.backupUID || backup.GetAnnotations()["hypercdr.io/consistency-run"] != record.ID || backup.GetDeletionTimestamp() != nil {
		return nil, &consistencyTerminalError{fmt.Errorf("captured backup identity changed or backup is deleting")}
	}
	phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
	switch phase {
	case "Failed", "FailedValidation", "PartiallyFailed", "Deleting", "Cancelled", "Canceling":
		return nil, &consistencyTerminalError{fmt.Errorf("captured backup reached unsuccessful phase %s", phase)}
	}
	if err := consistency.VerifyQuiescenceProof(record); err != nil {
		return nil, &consistencyTerminalError{err}
	}
	var persisted consistency.QuiescenceProof
	if err := json.Unmarshal([]byte(backup.GetAnnotations()["hypercdr.io/consistency-quiescence-proof"]), &persisted); err != nil {
		return nil, &consistencyTerminalError{fmt.Errorf("captured backup application-quiescence proof changed or is missing")}
	}
	persistedJSON, persistedErr := json.Marshal(&persisted)
	recordJSON, recordErr := json.Marshal(record.QuiescenceProof)
	if persistedErr != nil || recordErr != nil || !bytes.Equal(persistedJSON, recordJSON) {
		return nil, &consistencyTerminalError{fmt.Errorf("captured backup application-quiescence proof changed or is missing")}
	}
	return backup, nil
}
