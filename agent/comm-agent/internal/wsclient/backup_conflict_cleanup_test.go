package wsclient

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/internal/velero"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type staleBackupApplier struct {
	backups  []kube.VeleroBackupSummary
	canceled []string
	deleted  []kube.AppliedObject
}

func (a *staleBackupApplier) ApplyManifest(context.Context, kube.Manifest) (kube.AppliedObject, error) {
	return kube.AppliedObject{}, nil
}

func (a *staleBackupApplier) ListVeleroBackups(context.Context, string, int) ([]kube.VeleroBackupSummary, error) {
	return a.backups, nil
}

func (a *staleBackupApplier) CancelBackupVolumeOperations(_ context.Context, namespace, backupName string) error {
	a.canceled = append(a.canceled, namespace+"/"+backupName)
	return nil
}

func (a *staleBackupApplier) DeleteObject(_ context.Context, object kube.AppliedObject) error {
	a.deleted = append(a.deleted, object)
	return nil
}

func TestFindConflictingActiveBackupCleansPlatformAcknowledgedTerminalTask(t *testing.T) {
	ledger, err := newTaskLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const taskID = "terminal-task"
	if err := ledger.recordTask(taskID, "old-command", "plan-1", protocol.TaskDispatchPayload{TaskID: taskID, Type: "backup"}, taskLedgerObject{APIVersion: "velero.io/v1", Kind: "Backup", Namespace: "openshift-adp", Name: "old-backup"}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.markTerminalAcked(taskID); err != nil {
		t.Fatal(err)
	}
	applier := &staleBackupApplier{backups: []kube.VeleroBackupSummary{{
		Name: "old-backup", Namespace: "openshift-adp", Phase: "InProgress", CreatedAt: time.Now().Add(-time.Hour),
		Labels: map[string]string{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/plan-id": "plan-1", "hypercdr.io/task-id": taskID},
	}}}
	client := &Client{ledger: ledger, applier: applier, backupReader: applier, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	task := protocol.TaskDispatchPayload{TaskID: "new-task", Backup: &protocol.BackupCommand{PlanID: "plan-1"}}
	manifest := velero.BackupManifest{}
	manifest.Metadata.Name = "new-backup"

	_, conflict, err := client.findConflictingActiveBackup(context.Background(), task, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if conflict {
		t.Fatal("terminal acknowledged stale backup must not block a new backup")
	}
	if len(applier.canceled) != 1 || len(applier.deleted) != 1 || applier.deleted[0].Name != "old-backup" {
		t.Fatalf("expected cancellation and deletion, canceled=%v deleted=%v", applier.canceled, applier.deleted)
	}
}

func TestFindConflictingActiveBackupPreservesNonTerminalTask(t *testing.T) {
	ledger, err := newTaskLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	applier := &staleBackupApplier{backups: []kube.VeleroBackupSummary{{
		Name: "running-backup", Namespace: "openshift-adp", Phase: "InProgress",
		Labels: map[string]string{"hypercdr.io/managed-by": "hypercdr", "hypercdr.io/plan-id": "plan-1", "hypercdr.io/task-id": "running-task"},
	}}}
	client := &Client{ledger: ledger, applier: applier, backupReader: applier, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	task := protocol.TaskDispatchPayload{TaskID: "new-task", Backup: &protocol.BackupCommand{PlanID: "plan-1"}}
	manifest := velero.BackupManifest{}
	manifest.Metadata.Name = "new-backup"

	existing, conflict, err := client.findConflictingActiveBackup(context.Background(), task, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !conflict || existing.Name != "running-backup" {
		t.Fatalf("expected live backup conflict, got conflict=%v backup=%q", conflict, existing.Name)
	}
	if len(applier.canceled) != 0 || len(applier.deleted) != 0 {
		t.Fatalf("live backup was modified, canceled=%v deleted=%v", applier.canceled, applier.deleted)
	}
}
