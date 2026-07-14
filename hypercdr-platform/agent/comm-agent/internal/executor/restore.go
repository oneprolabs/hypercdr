package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/internal/velero"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type RestoreExecutor interface {
	Name() string
	BuildRestoreManifest(task protocol.TaskDispatchPayload) (velero.RestoreManifest, error)
	SubmitRestore(ctx context.Context, manifest velero.RestoreManifest) error
}

func NewRestoreExecutor(cfg Config) RestoreExecutor {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", ModeDryRun:
		return DryRunRestoreExecutor{agentNamespace: cfg.AgentNamespace}
	case ModeKubernetes:
		return KubernetesRestoreExecutor{agentNamespace: cfg.AgentNamespace, applier: cfg.Applier}
	default:
		return UnsupportedRestoreExecutor{mode: cfg.Mode}
	}
}

type DryRunRestoreExecutor struct {
	agentNamespace string
}

func (e DryRunRestoreExecutor) Name() string {
	return ModeDryRun
}

func (e DryRunRestoreExecutor) BuildRestoreManifest(task protocol.TaskDispatchPayload) (velero.RestoreManifest, error) {
	if task.Restore == nil {
		return velero.RestoreManifest{}, errors.New("restore command is required")
	}
	return velero.BuildRestoreManifest(velero.RestoreBuildInput{
		TaskID:         task.TaskID,
		CommandID:      task.CommandID,
		TaskType:       task.Type,
		AgentNamespace: e.agentNamespace,
		Command:        *task.Restore,
	})
}

func (e DryRunRestoreExecutor) SubmitRestore(ctx context.Context, manifest velero.RestoreManifest) error {
	return nil
}

type KubernetesRestoreExecutor struct {
	agentNamespace string
	applier        kube.ManifestApplier
}

func (e KubernetesRestoreExecutor) Name() string {
	return ModeKubernetes
}

func (e KubernetesRestoreExecutor) BuildRestoreManifest(task protocol.TaskDispatchPayload) (velero.RestoreManifest, error) {
	if task.Restore == nil {
		return velero.RestoreManifest{}, errors.New("restore command is required")
	}
	return velero.BuildRestoreManifest(velero.RestoreBuildInput{
		TaskID:         task.TaskID,
		CommandID:      task.CommandID,
		TaskType:       task.Type,
		AgentNamespace: e.agentNamespace,
		Command:        *task.Restore,
	})
}

func (e KubernetesRestoreExecutor) SubmitRestore(ctx context.Context, manifest velero.RestoreManifest) error {
	if e.applier == nil {
		return errors.New("kubernetes manifest applier is not configured")
	}
	if manifest.Metadata.Labels["hypercdr.io/restore-mode"] == "dataOnly" {
		return errors.New("data-only restore executor is not enabled")
	}
	waiter, ok := e.applier.(kube.VeleroBackupWaiter)
	if !ok {
		return errors.New("kubernetes velero backup readiness check is not supported by this executor")
	}
	if err := waiter.WaitForVeleroBackup(ctx, manifest.Metadata.Namespace, manifest.Spec.BackupName, 3*time.Minute); err != nil {
		return fmt.Errorf("velero backup is not available for restore: %w", err)
	}
	if manifest.Metadata.Labels["hypercdr.io/conflict-policy"] == "replace" {
		targetNamespace := manifest.Metadata.Labels["hypercdr.io/target-namespace"]
		if targetNamespace == "" {
			return errors.New("target namespace is required for namespace replacement")
		}
		if targetNamespace == manifest.Metadata.Namespace || targetNamespace == e.agentNamespace {
			return fmt.Errorf("refusing to replace agent namespace %q", targetNamespace)
		}
		cleaner, ok := e.applier.(kube.RestoreStateCleaner)
		if !ok {
			return errors.New("kubernetes restore state cleanup is not supported by this executor")
		}
		sourceNamespace := firstIncludedNamespace(manifest)
		if err := cleaner.CleanupStaleRestoreState(ctx, manifest.Metadata.Namespace, sourceNamespace, targetNamespace, manifest.Metadata.Name); err != nil {
			return err
		}
		replacer, ok := e.applier.(kube.NamespaceReplacer)
		if !ok {
			return errors.New("kubernetes namespace replacement is not supported by this executor")
		}
		if err := replacer.DeleteNamespaceAndWait(ctx, targetNamespace); err != nil {
			return err
		}
	}
	modifier, err := kube.ManifestFromStruct(velero.BuildRestoreResourceModifierConfigMap(manifest))
	if err != nil {
		return err
	}
	if _, err = e.applier.ApplyManifest(ctx, modifier); err != nil {
		return err
	}
	unstructured, err := kube.ManifestFromStruct(manifest)
	if err != nil {
		return err
	}
	_, err = e.applier.ApplyManifest(ctx, unstructured)
	return err
}

func firstIncludedNamespace(manifest velero.RestoreManifest) string {
	if len(manifest.Spec.IncludedNamespaces) == 0 {
		return ""
	}
	return manifest.Spec.IncludedNamespaces[0]
}

type UnsupportedRestoreExecutor struct {
	mode string
}

func (e UnsupportedRestoreExecutor) Name() string {
	return e.mode
}

func (e UnsupportedRestoreExecutor) BuildRestoreManifest(task protocol.TaskDispatchPayload) (velero.RestoreManifest, error) {
	return velero.RestoreManifest{}, fmt.Errorf("unsupported restore executor mode %q", e.mode)
}

func (e UnsupportedRestoreExecutor) SubmitRestore(ctx context.Context, manifest velero.RestoreManifest) error {
	return fmt.Errorf("unsupported restore executor mode %q", e.mode)
}
