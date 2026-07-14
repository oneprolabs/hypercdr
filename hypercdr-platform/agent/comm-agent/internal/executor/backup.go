package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/internal/velero"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

const (
	ModeDryRun     = "dry-run"
	ModeKubernetes = "kubernetes"
)

type BackupExecutor interface {
	Name() string
	BuildBackupManifest(task protocol.TaskDispatchPayload) (velero.BackupManifest, error)
	SubmitBackup(ctx context.Context, manifest velero.BackupManifest) error
}

type Config struct {
	Mode           string
	AgentNamespace string
	Applier        kube.ManifestApplier
}

func NewBackupExecutor(cfg Config) BackupExecutor {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", ModeDryRun:
		return DryRunBackupExecutor{agentNamespace: cfg.AgentNamespace}
	case ModeKubernetes:
		return KubernetesBackupExecutor{agentNamespace: cfg.AgentNamespace, applier: cfg.Applier}
	default:
		return UnsupportedBackupExecutor{mode: cfg.Mode}
	}
}

type DryRunBackupExecutor struct {
	agentNamespace string
}

func (e DryRunBackupExecutor) Name() string {
	return ModeDryRun
}

func (e DryRunBackupExecutor) BuildBackupManifest(task protocol.TaskDispatchPayload) (velero.BackupManifest, error) {
	if task.Backup == nil {
		return velero.BackupManifest{}, errors.New("backup command is required")
	}
	return velero.BuildBackupManifest(velero.BackupBuildInput{
		TaskID:         task.TaskID,
		CommandID:      task.CommandID,
		AgentNamespace: e.agentNamespace,
		Command:        *task.Backup,
	})
}

func (e DryRunBackupExecutor) SubmitBackup(ctx context.Context, manifest velero.BackupManifest) error {
	return nil
}

type KubernetesBackupExecutor struct {
	agentNamespace string
	applier        kube.ManifestApplier
}

func (e KubernetesBackupExecutor) Name() string {
	return ModeKubernetes
}

func (e KubernetesBackupExecutor) BuildBackupManifest(task protocol.TaskDispatchPayload) (velero.BackupManifest, error) {
	if task.Backup == nil {
		return velero.BackupManifest{}, errors.New("backup command is required")
	}
	return velero.BuildBackupManifest(velero.BackupBuildInput{
		TaskID:         task.TaskID,
		CommandID:      task.CommandID,
		AgentNamespace: e.agentNamespace,
		Command:        *task.Backup,
	})
}

func (e KubernetesBackupExecutor) SubmitBackup(ctx context.Context, manifest velero.BackupManifest) error {
	if e.applier == nil {
		return errors.New("kubernetes manifest applier is not configured")
	}
	unstructured, err := kube.ManifestFromStruct(manifest)
	if err != nil {
		return err
	}
	_, err = e.applier.ApplyManifest(ctx, unstructured)
	return err
}

type UnsupportedBackupExecutor struct {
	mode string
}

func (e UnsupportedBackupExecutor) Name() string {
	return e.mode
}

func (e UnsupportedBackupExecutor) BuildBackupManifest(task protocol.TaskDispatchPayload) (velero.BackupManifest, error) {
	return velero.BackupManifest{}, fmt.Errorf("unsupported backup executor mode %q", e.mode)
}

func (e UnsupportedBackupExecutor) SubmitBackup(ctx context.Context, manifest velero.BackupManifest) error {
	return fmt.Errorf("unsupported backup executor mode %q", e.mode)
}
