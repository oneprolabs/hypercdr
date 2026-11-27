package kube

import (
	"context"
	"fmt"
	"reflect"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (applier *DynamicManifestApplier) ReadConsistencyBackupCompletion(ctx context.Context, namespace, name string, record consistency.Record, requirements []SnapshotVolumeRequirement) (*unstructured.Unstructured, error) {
	if applier == nil || applier.client == nil || namespace == "" || name == "" || record.Phase != consistency.Completed || !record.Resumed || !record.LocksReleased || record.Failure != "" {
		return nil, fmt.Errorf("backup completion requires an identified, completed and safely resumed consistency run")
	}
	if err := consistency.VerifyQuiescenceProof(record); err != nil {
		return nil, err
	}
	if err := consistency.VerifyDurableSnapshots(record); err != nil {
		return nil, err
	}
	backup, err := applier.client.Resource(consistencyBackups).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if backup.GetUID() == "" {
		return nil, fmt.Errorf("completed backup has no immutable Kubernetes identity")
	}
	driver := historicalConsistencyBackupReader{Applier: applier, Backup: backup, backupUID: string(backup.GetUID())}
	verifiedBackup, err := driver.readBackup(ctx, record)
	if err != nil {
		return nil, err
	}
	phase, _, _ := unstructured.NestedString(verifiedBackup.Object, "status", "phase")
	if phase != "Completed" {
		return nil, fmt.Errorf("consistency backup is not completed: %s", phase)
	}
	snapshots, err := applier.ReadConsistencySnapshotBarrier(ctx, namespace, name, string(backup.GetUID()), record.Volumes, requirements)
	if err != nil {
		return nil, err
	}
	if len(snapshots) != len(record.Snapshots) {
		return nil, fmt.Errorf("completed backup does not cover the journal snapshot inventory")
	}
	expected := make(map[string]consistency.Snapshot, len(record.Snapshots))
	for _, snapshot := range record.Snapshots {
		expected[snapshot.VolumeUID] = snapshot
	}
	for _, snapshot := range snapshots {
		if !reflect.DeepEqual(expected[snapshot.VolumeUID], snapshot) {
			return nil, fmt.Errorf("completed backup durable snapshot differs from the consistency journal")
		}
	}
	finalBackup, err := driver.readBackup(ctx, record)
	if err != nil {
		return nil, err
	}
	finalPhase, _, _ := unstructured.NestedString(finalBackup.Object, "status", "phase")
	if finalPhase != "Completed" || finalBackup.GetResourceVersion() != verifiedBackup.GetResourceVersion() {
		return nil, fmt.Errorf("backup changed while verifying completion evidence")
	}
	return finalBackup, nil
}
