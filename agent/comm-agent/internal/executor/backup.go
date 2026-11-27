package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	if manifest.Spec.SnapshotMoveData != nil && *manifest.Spec.SnapshotMoveData {
		preflightCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		runtimeChecker, ok := e.applier.(interface {
			CheckSnapshotRuntime(context.Context, string) error
		})
		if !ok {
			return errors.New("CSI runtime checker unavailable; upgrade the Agent before using snapshot backups")
		}
		if err := runtimeChecker.CheckSnapshotRuntime(preflightCtx, manifest.Metadata.Namespace); err != nil {
			return err
		}
		preflight, ok := e.applier.(interface {
			PrepareSnapshotBackup(context.Context, []string) (map[string]string, error)
		})
		if !ok {
			return errors.New("snapshot prerequisite checker unavailable; refusing unchecked snapshot capture")
		}
		annotations, err := preflight.PrepareSnapshotBackup(preflightCtx, manifest.Spec.IncludedNamespaces)
		if err != nil {
			return fmt.Errorf("snapshot prerequisites failed: %w", err)
		}
		if optimizer, ok := e.applier.(interface {
			ConfigureBackupVolumes(context.Context, string, []string) error
		}); ok {
			optimizationCtx, optimizationCancel := context.WithTimeout(preflightCtx, 15*time.Second)
			optimizationErr := optimizer.ConfigureBackupVolumes(optimizationCtx, manifest.Metadata.Namespace, manifest.Spec.IncludedNamespaces)
			optimizationCancel()
			if optimizationErr != nil {
				slog.Warn("backup temporary volume optimization unavailable; retaining standard CSI configuration", "error", optimizationErr)
			}
		}
		merged := make(map[string]string, len(manifest.Metadata.Annotations)+len(annotations))
		for key, value := range manifest.Metadata.Annotations {
			merged[key] = value
		}
		for key, value := range annotations {
			if existing := merged[key]; existing != "" && existing != value {
				return fmt.Errorf("snapshot class annotation conflicts with verified selection")
			}
			merged[key] = value
		}
		manifest.Metadata.Annotations = merged
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
