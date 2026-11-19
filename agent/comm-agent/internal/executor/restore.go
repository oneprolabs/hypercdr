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
		replacements, err := restoreNamespaceReplacements(manifest, e.agentNamespace)
		if err != nil {
			return err
		}
		cleaner, ok := e.applier.(kube.RestoreStateCleaner)
		if !ok {
			return errors.New("kubernetes restore state cleanup is not supported by this executor")
		}
		replacer, ok := e.applier.(kube.NamespaceReplacer)
		if !ok {
			return errors.New("kubernetes namespace replacement is not supported by this executor")
		}
		for _, replacement := range replacements {
			if err := cleaner.CleanupStaleRestoreState(ctx, manifest.Metadata.Namespace, replacement.source, replacement.target, manifest.Metadata.Name); err != nil {
				return fmt.Errorf("failed to clean restore state for target namespace %q: %w", replacement.target, err)
			}
		}
		for _, replacement := range replacements {
			if err := replacer.ReplaceNamespaceAndWait(ctx, replacement.target); err != nil {
				return fmt.Errorf("failed to replace target namespace %q: %w", replacement.target, err)
			}
		}
	}
	modifier, err := kube.ManifestFromStruct(velero.BuildRestoreResourceModifierConfigMap(manifest))
	if err != nil {
		return err
	}
	modifierObject, err := e.applier.ApplyManifest(ctx, modifier)
	if err != nil {
		return err
	}
	modifierWaiter, ok := e.applier.(kube.ResourceModifierWaiter)
	if !ok {
		return errors.New("kubernetes resource modifier propagation check is not supported by this executor")
	}
	if err := modifierWaiter.WaitForResourceModifier(ctx, modifierObject.Namespace, modifierObject.Name, 15*time.Second); err != nil {
		return fmt.Errorf("resource modifier is not ready for Velero restore validation: %w", err)
	}
	unstructured, err := kube.ManifestFromStruct(manifest)
	if err != nil {
		return err
	}
	_, err = e.applier.ApplyManifest(ctx, unstructured)
	return err
}

type namespaceReplacement struct {
	source string
	target string
}

func restoreNamespaceReplacements(manifest velero.RestoreManifest, agentNamespace string) ([]namespaceReplacement, error) {
	if len(manifest.Spec.IncludedNamespaces) == 0 {
		return nil, errors.New("source namespaces are required for namespace replacement")
	}
	replacements := make([]namespaceReplacement, 0, len(manifest.Spec.IncludedNamespaces))
	targets := map[string]bool{}
	for _, source := range manifest.Spec.IncludedNamespaces {
		target := source
		if mapped, exists := manifest.Spec.NamespaceMapping[source]; exists {
			target = mapped
		}
		if strings.TrimSpace(source) == "" || strings.TrimSpace(target) == "" {
			return nil, errors.New("source and target namespaces are required for namespace replacement")
		}
		if target == manifest.Metadata.Namespace || target == agentNamespace {
			return nil, fmt.Errorf("refusing to replace agent namespace %q", target)
		}
		if targets[target] {
			return nil, fmt.Errorf("multiple source namespaces map to target namespace %q", target)
		}
		targets[target] = true
		replacements = append(replacements, namespaceReplacement{source: source, target: target})
	}
	return replacements, nil
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
