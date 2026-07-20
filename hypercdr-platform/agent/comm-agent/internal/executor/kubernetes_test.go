package executor

import (
	"context"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestKubernetesBackupExecutorAppliesVeleroBackup(t *testing.T) {
	applier := &fakeManifestApplier{}
	exec := NewBackupExecutor(Config{
		Mode:           ModeKubernetes,
		AgentNamespace: "hypercdr-agent",
		Applier:        applier,
	})
	task := protocol.TaskDispatchPayload{
		TaskID:    "task-backup",
		CommandID: "command-backup",
		Type:      "backup",
		Deadline:  time.Now().Add(time.Minute),
		Backup: &protocol.BackupCommand{
			SourceNamespace: "default",
			LabelSelector:   protocol.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
			StorageRepo:     "default",
		},
	}

	manifest, err := exec.BuildBackupManifest(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.SubmitBackup(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	applied := applier.last(t)
	if applied["kind"] != "Backup" {
		t.Fatalf("expected Backup manifest, got %#v", applied["kind"])
	}
	spec := applied["spec"].(map[string]any)
	if spec["storageLocation"] != "default" {
		t.Fatalf("unexpected backup spec: %#v", spec)
	}
	if spec["defaultVolumesToFsBackup"] != true || spec["snapshotVolumes"] != false {
		t.Fatalf("expected filesystem backup enabled and snapshots disabled, got %#v", spec)
	}
}

func TestKubernetesRestoreExecutorAppliesVeleroRestore(t *testing.T) {
	applier := &fakeManifestApplier{}
	exec := NewRestoreExecutor(Config{
		Mode:           ModeKubernetes,
		AgentNamespace: "hypercdr-agent",
		Applier:        applier,
	})
	task := protocol.TaskDispatchPayload{
		TaskID:    "task-restore",
		CommandID: "command-restore",
		Type:      "restore",
		Deadline:  time.Now().Add(time.Minute),
		Restore: &protocol.RestoreCommand{
			VeleroBackupName: "backup-a",
			SourceNamespace:  "default",
			TargetNamespace:  "default-restore",
			ConflictPolicy:   "update",
		},
	}

	manifest, err := exec.BuildRestoreManifest(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.SubmitRestore(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	if len(applier.manifests) != 2 {
		t.Fatalf("expected ConfigMap and Restore manifests, got %d", len(applier.manifests))
	}
	modifier := applier.manifests[0]
	if modifier["kind"] != "ConfigMap" {
		t.Fatalf("expected ConfigMap manifest first, got %#v", modifier["kind"])
	}
	data := modifier["data"].(map[string]any)
	if data["resource-modifiers.yaml"] == "" {
		t.Fatalf("expected resource modifier config, got %#v", data)
	}

	applied := applier.last(t)
	if applied["kind"] != "Restore" {
		t.Fatalf("expected Restore manifest, got %#v", applied["kind"])
	}
	spec := applied["spec"].(map[string]any)
	if spec["backupName"] != "backup-a" {
		t.Fatalf("unexpected restore spec: %#v", spec)
	}
	if spec["resourceModifier"] == nil {
		t.Fatalf("expected restore resourceModifier, got %#v", spec)
	}
	if applier.waitedBackupNamespace != "hypercdr-agent" || applier.waitedBackupName != "backup-a" {
		t.Fatalf("restore should wait for backup before submit, namespace=%q name=%q", applier.waitedBackupNamespace, applier.waitedBackupName)
	}
}

func TestKubernetesRestoreExecutorReplacesNamespaceBeforeRestore(t *testing.T) {
	applier := &fakeNamespaceReplacingApplier{}
	exec := NewRestoreExecutor(Config{
		Mode:           ModeKubernetes,
		AgentNamespace: "hypercdr-agent",
		Applier:        applier,
	})
	task := protocol.TaskDispatchPayload{
		TaskID:    "task-restore",
		CommandID: "command-restore",
		Type:      "restore",
		Deadline:  time.Now().Add(time.Minute),
		Restore: &protocol.RestoreCommand{
			VeleroBackupName: "backup-a",
			SourceNamespace:  "default",
			TargetNamespace:  "default",
			RestoreMode:      "full",
			ConflictPolicy:   "replace",
		},
	}

	manifest, err := exec.BuildRestoreManifest(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.SubmitRestore(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}

	if applier.deletedNamespace != "default" {
		t.Fatalf("deleted namespace = %q, want default", applier.deletedNamespace)
	}
	if applier.cleanedAgentNamespace != "hypercdr-agent" || applier.cleanedSourceNamespace != "default" || applier.cleanedTargetNamespace != "default" {
		t.Fatalf("unexpected cleanup call: agent=%q source=%q target=%q", applier.cleanedAgentNamespace, applier.cleanedSourceNamespace, applier.cleanedTargetNamespace)
	}
	if applier.cleanedCurrentRestore == "" {
		t.Fatalf("cleanup should receive current restore name")
	}
	if applier.replacedNamespace != "default" {
		t.Fatalf("replaced namespace = %q, want default", applier.replacedNamespace)
	}
	if len(applier.manifests) != 2 {
		t.Fatalf("expected ConfigMap and Restore manifests, got %d", len(applier.manifests))
	}
	if applier.manifests[0]["kind"] != "ConfigMap" || applier.manifests[1]["kind"] != "Restore" {
		t.Fatalf("unexpected restore manifest order: %#v, %#v", applier.manifests[0]["kind"], applier.manifests[1]["kind"])
	}
}

func TestKubernetesRestoreExecutorRejectsDataOnlyRestore(t *testing.T) {
	applier := &fakeNamespaceReplacingApplier{}
	exec := NewRestoreExecutor(Config{
		Mode:           ModeKubernetes,
		AgentNamespace: "hypercdr-agent",
		Applier:        applier,
	})
	task := protocol.TaskDispatchPayload{
		TaskID:    "task-restore",
		CommandID: "command-restore",
		Type:      "restore",
		Deadline:  time.Now().Add(time.Minute),
		Restore: &protocol.RestoreCommand{
			VeleroBackupName: "backup-a",
			SourceNamespace:  "default",
			TargetNamespace:  "default",
			RestoreMode:      "dataOnly",
			ConflictPolicy:   "overwrite",
		},
	}

	manifest, err := exec.BuildRestoreManifest(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.SubmitRestore(context.Background(), manifest); err == nil {
		t.Fatal("expected data-only restore to be rejected")
	}
	if applier.deletedNamespace != "" || len(applier.manifests) != 0 {
		t.Fatalf("data-only restore should not change the cluster, deleted=%q manifests=%d", applier.deletedNamespace, len(applier.manifests))
	}
}

func TestKubernetesRestoreExecutorFailsWhenBackupIsNotSynced(t *testing.T) {
	applier := &fakeManifestApplier{waitBackupErr: context.DeadlineExceeded}
	exec := NewRestoreExecutor(Config{
		Mode:           ModeKubernetes,
		AgentNamespace: "hypercdr-agent",
		Applier:        applier,
	})
	task := protocol.TaskDispatchPayload{
		TaskID:    "task-restore",
		CommandID: "command-restore",
		Type:      "drill",
		Deadline:  time.Now().Add(time.Minute),
		Restore: &protocol.RestoreCommand{
			VeleroBackupName: "backup-a",
			SourceNamespace:  "default",
			TargetNamespace:  "default-drill",
			ConflictPolicy:   "update",
		},
	}

	manifest, err := exec.BuildRestoreManifest(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.SubmitRestore(context.Background(), manifest); err == nil {
		t.Fatal("expected restore submit to fail when backup is not synced")
	}
	if len(applier.manifests) != 0 {
		t.Fatalf("restore should not create manifests before backup is synced, got %d", len(applier.manifests))
	}
}

func TestKubernetesStorageExecutorAppliesBackupStorageLocation(t *testing.T) {
	applier := &fakeManifestApplier{}
	exec := NewStorageExecutor(Config{
		Mode:           ModeKubernetes,
		AgentNamespace: "hypercdr-agent",
		Applier:        applier,
	})
	task := protocol.TaskDispatchPayload{
		TaskID:    "task-storage",
		CommandID: "command-storage",
		Type:      "storage-sync",
		Deadline:  time.Now().Add(time.Minute),
		StorageSync: &protocol.StorageSyncCommand{
			RepositoryID: "repo-a",
			Name:         "default",
			Type:         "S3",
			Endpoint:     "https://s3.example.com",
			Bucket:       "hypercdr",
			Region:       "us-east-1",
			TLSEnabled:   true,
			SecretRef:    "velero-cloud-credentials",
			Credentials: &protocol.S3Credentials{
				AccessKey: "minio",
				SecretKey: "minio-secret",
			},
			Config: map[string]any{"prefix": "clusters/a"},
		},
	}

	manifests, err := exec.BuildStorageManifests(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.SubmitStorage(context.Background(), manifests); err != nil {
		t.Fatal(err)
	}

	if len(applier.manifests) != 2 {
		t.Fatalf("expected Secret and BackupStorageLocation manifests, got %d", len(applier.manifests))
	}
	secret := applier.manifests[0]
	if secret["kind"] != "Secret" {
		t.Fatalf("expected Secret manifest first, got %#v", secret["kind"])
	}
	stringData := secret["stringData"].(map[string]any)
	if stringData["cloud"] == "" {
		t.Fatalf("expected velero cloud credentials, got %#v", stringData)
	}

	applied := applier.last(t)
	if applied["kind"] != "BackupStorageLocation" {
		t.Fatalf("expected BackupStorageLocation manifest, got %#v", applied["kind"])
	}
	spec := applied["spec"].(map[string]any)
	if spec["provider"] != "aws" {
		t.Fatalf("unexpected storage spec: %#v", spec)
	}
	objectStorage := spec["objectStorage"].(map[string]any)
	if objectStorage["bucket"] != "hypercdr" || objectStorage["prefix"] != "clusters/a" {
		t.Fatalf("unexpected object storage spec: %#v", objectStorage)
	}
	credential := spec["credential"].(map[string]any)
	if credential["name"] != "velero-cloud-credentials" || credential["key"] != "cloud" {
		t.Fatalf("unexpected credential ref: %#v", credential)
	}
}

type fakeManifestApplier struct {
	manifests             []kube.Manifest
	waitedBackupNamespace string
	waitedBackupName      string
	waitedBackupTimeout   time.Duration
	waitBackupErr         error
}

func (a *fakeManifestApplier) ApplyManifest(ctx context.Context, manifest kube.Manifest) (kube.AppliedObject, error) {
	a.manifests = append(a.manifests, manifest)
	return kube.ObjectFromManifest(manifest)
}

func (a *fakeManifestApplier) WaitForVeleroBackup(ctx context.Context, namespace string, name string, timeout time.Duration) error {
	a.waitedBackupNamespace = namespace
	a.waitedBackupName = name
	a.waitedBackupTimeout = timeout
	return a.waitBackupErr
}

func (a *fakeManifestApplier) last(t *testing.T) kube.Manifest {
	t.Helper()
	if len(a.manifests) == 0 {
		t.Fatal("expected applied manifest")
	}
	return a.manifests[len(a.manifests)-1]
}

type fakeNamespaceReplacingApplier struct {
	fakeManifestApplier
	deletedNamespace       string
	replacedNamespace      string
	cleanedAgentNamespace  string
	cleanedSourceNamespace string
	cleanedTargetNamespace string
	cleanedCurrentRestore  string
}

func (a *fakeNamespaceReplacingApplier) CleanupStaleRestoreState(ctx context.Context, agentNamespace string, sourceNamespace string, targetNamespace string, currentRestoreName string) error {
	a.cleanedAgentNamespace = agentNamespace
	a.cleanedSourceNamespace = sourceNamespace
	a.cleanedTargetNamespace = targetNamespace
	a.cleanedCurrentRestore = currentRestoreName
	return nil
}

func (a *fakeNamespaceReplacingApplier) ReplaceNamespaceAndWait(ctx context.Context, namespace string) error {
	a.deletedNamespace = namespace
	a.replacedNamespace = namespace
	return nil
}
