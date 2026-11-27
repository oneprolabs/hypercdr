package executor

import (
	"context"
	"fmt"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/velero"
)

type snapshotPreflightApplier struct {
	fakeManifestApplier
	err               error
	runtimeErr        error
	optimizationErr   error
	optimizationCalls int
}

func (applier *snapshotPreflightApplier) ConfigureBackupVolumes(context.Context, string, []string) error {
	applier.optimizationCalls++
	return applier.optimizationErr
}

func (applier *snapshotPreflightApplier) CheckSnapshotRuntime(context.Context, string) error {
	return applier.runtimeErr
}

func (applier *snapshotPreflightApplier) PrepareSnapshotBackup(context.Context, []string) (map[string]string, error) {
	return map[string]string{"velero.io/csi-volumesnapshot-class_test.csi": "verified"}, applier.err
}

func TestSnapshotSubmissionRequiresSuccessfulPreflight(t *testing.T) {
	enabled := true
	manifest := velero.BackupManifest{APIVersion: "velero.io/v1", Kind: "Backup", Metadata: velero.ManifestMetadata{Name: "isolated-snapshot", Namespace: "agent"}, Spec: velero.BackupManifestSpec{SnapshotMoveData: &enabled, IncludedNamespaces: []string{"isolated"}}}
	unchecked := &fakeManifestApplier{}
	if err := (KubernetesBackupExecutor{applier: unchecked}).SubmitBackup(context.Background(), manifest); err == nil || len(unchecked.manifests) != 0 {
		t.Fatal("unchecked CSI backup was submitted")
	}
	checked := &snapshotPreflightApplier{err: fmt.Errorf("CSI unavailable")}
	if err := (KubernetesBackupExecutor{applier: checked}).SubmitBackup(context.Background(), manifest); err == nil || len(checked.manifests) != 0 {
		t.Fatal("failed preflight still submitted backup")
	}
	checked.err = nil
	checked.runtimeErr = fmt.Errorf("EnableCSI is disabled")
	if err := (KubernetesBackupExecutor{applier: checked}).SubmitBackup(context.Background(), manifest); err == nil || len(checked.manifests) != 0 {
		t.Fatal("snapshot backup submitted without an enabled CSI runtime")
	}
	checked.runtimeErr = nil
	checked.optimizationErr = fmt.Errorf("optimization permission denied")
	if err := (KubernetesBackupExecutor{applier: checked}).SubmitBackup(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	metadata := checked.last(t)["metadata"].(map[string]any)
	if metadata["annotations"].(map[string]any)["velero.io/csi-volumesnapshot-class_test.csi"] != "verified" {
		t.Fatal("verified snapshot class annotation was not applied")
	}
	if manifest.Metadata.Annotations != nil {
		t.Fatal("submission mutated caller manifest")
	}
	if checked.optimizationCalls != 1 {
		t.Fatal("optional optimization did not run")
	}
	manifest.Metadata.Annotations = map[string]string{"velero.io/csi-volumesnapshot-class_test.csi": "unverified"}
	if err := (KubernetesBackupExecutor{applier: checked}).SubmitBackup(context.Background(), manifest); err == nil || len(checked.manifests) != 1 {
		t.Fatal("conflicting class selection was submitted")
	}
}
