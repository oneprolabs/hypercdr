package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (applier *DynamicManifestApplier) verifyConsistencyRestoreBackupMetadata(ctx context.Context, namespace string, evidence protocol.ConsistencyRestoreEvidence) error {
	command := protocol.RestoreCommand{VeleroBackupName: evidence.Backup.VeleroBackupName, SourceNamespace: evidence.Backup.SourceNamespace, SourceNamespaces: evidence.Backup.SourceNamespaces, ConsistencyEvidence: &evidence}
	if err := protocol.ValidateConsistencyRestoreEvidence(command); err != nil {
		return err
	}
	if applier == nil || applier.client == nil || namespace == "" {
		return fmt.Errorf("destination backup reader is unavailable")
	}
	backup, err := applier.client.Resource(consistencyBackups).Namespace(namespace).Get(ctx, command.VeleroBackupName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if backup.GetUID() == "" || backup.GetResourceVersion() == "" || backup.GetLabels()["hypercdr.io/task-id"] != evidence.TaskID || backup.GetLabels()["hypercdr.io/command-id"] != evidence.CommandID {
		return fmt.Errorf("destination backup does not match the accepted source task")
	}
	if evidence.Backup.SourceClusterID != "" && backup.GetLabels()["hypercdr.io/source-cluster-id"] != evidence.Backup.SourceClusterID {
		return fmt.Errorf("destination backup source cluster differs")
	}
	driver := historicalConsistencyBackupReader{Applier: applier, Backup: backup, backupUID: string(backup.GetUID())}
	verified, err := driver.readBackup(ctx, evidence.Record)
	if err != nil {
		return err
	}
	phase, _, _ := unstructured.NestedString(verified.Object, "status", "phase")
	move, _, _ := unstructured.NestedBool(verified.Object, "spec", "snapshotMoveData")
	snapshot, _, _ := unstructured.NestedBool(verified.Object, "spec", "snapshotVolumes")
	fsb, _, _ := unstructured.NestedBool(verified.Object, "spec", "defaultVolumesToFsBackup")
	if phase != "Completed" || !move || !snapshot || fsb || verified.GetAnnotations()["hypercdr.io/consistency-requested"] != "application-consistent" {
		return fmt.Errorf("destination backup is not a completed coordinated CSI backup")
	}
	var workloads []protocol.ConsistencyWorkloadState
	if err := json.Unmarshal([]byte(verified.GetAnnotations()["hypercdr.io/consistency-original-workloads"]), &workloads); err != nil || !reflect.DeepEqual(workloads, evidence.Record.Workloads) {
		return fmt.Errorf("destination backup original workload state differs")
	}
	selected, _, err := unstructured.NestedStringSlice(verified.Object, "spec", "includedNamespaces")
	if err != nil {
		return err
	}
	command.SourceNamespaces, command.SourceNamespace = selected, ""
	if err := protocol.ValidateConsistencyRestoreEvidence(command); err != nil {
		return err
	}
	for _, volume := range evidence.Backup.ConsistencyGroup.Volumes {
		matched := false
		for key, class := range verified.GetAnnotations() {
			if strings.HasPrefix(key, "velero.io/csi-volumesnapshot-class_") && class == volume.SnapshotClass {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("destination backup snapshot class evidence is missing")
		}
	}
	final, err := driver.readBackup(ctx, evidence.Record)
	if err != nil {
		return err
	}
	if final.GetResourceVersion() != backup.GetResourceVersion() {
		return fmt.Errorf("destination backup changed during verification")
	}
	return nil
}
