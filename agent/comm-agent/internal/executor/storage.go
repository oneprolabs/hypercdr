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

type StorageExecutor interface {
	Name() string
	BuildStorageManifests(task protocol.TaskDispatchPayload) (velero.StorageManifests, error)
	SubmitStorage(ctx context.Context, manifests velero.StorageManifests) error
}

func NewStorageExecutor(cfg Config) StorageExecutor {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", ModeDryRun:
		return DryRunStorageExecutor{agentNamespace: cfg.AgentNamespace}
	case ModeKubernetes:
		return KubernetesStorageExecutor{agentNamespace: cfg.AgentNamespace, applier: cfg.Applier}
	default:
		return UnsupportedStorageExecutor{mode: cfg.Mode}
	}
}

type DryRunStorageExecutor struct {
	agentNamespace string
}

func (e DryRunStorageExecutor) Name() string {
	return ModeDryRun
}

func (e DryRunStorageExecutor) BuildStorageManifests(task protocol.TaskDispatchPayload) (velero.StorageManifests, error) {
	if task.StorageSync == nil {
		return velero.StorageManifests{}, errors.New("storage sync command is required")
	}
	return velero.BuildStorageManifests(velero.StorageBuildInput{
		TaskID:         task.TaskID,
		CommandID:      task.CommandID,
		AgentNamespace: e.agentNamespace,
		Command:        *task.StorageSync,
	})
}

func (e DryRunStorageExecutor) SubmitStorage(ctx context.Context, manifests velero.StorageManifests) error {
	return nil
}

type KubernetesStorageExecutor struct {
	agentNamespace string
	applier        kube.ManifestApplier
}

func (e KubernetesStorageExecutor) Name() string {
	return ModeKubernetes
}

func (e KubernetesStorageExecutor) BuildStorageManifests(task protocol.TaskDispatchPayload) (velero.StorageManifests, error) {
	if task.StorageSync == nil {
		return velero.StorageManifests{}, errors.New("storage sync command is required")
	}
	return velero.BuildStorageManifests(velero.StorageBuildInput{
		TaskID:         task.TaskID,
		CommandID:      task.CommandID,
		AgentNamespace: e.agentNamespace,
		Command:        *task.StorageSync,
	})
}

func (e KubernetesStorageExecutor) SubmitStorage(ctx context.Context, manifests velero.StorageManifests) error {
	if e.applier == nil {
		return errors.New("kubernetes manifest applier is not configured")
	}
	if manifests.Secret != nil {
		unstructured, err := kube.ManifestFromStruct(manifests.Secret)
		if err != nil {
			return err
		}
		if _, err := e.applier.ApplyManifest(ctx, unstructured); err != nil {
			return err
		}
	}
	unstructured, err := kube.ManifestFromStruct(manifests.BackupStorageLocation)
	if err != nil {
		return err
	}
	_, err = e.applier.ApplyManifest(ctx, unstructured)
	return err
}

type UnsupportedStorageExecutor struct {
	mode string
}

func (e UnsupportedStorageExecutor) Name() string {
	return e.mode
}

func (e UnsupportedStorageExecutor) BuildStorageManifests(task protocol.TaskDispatchPayload) (velero.StorageManifests, error) {
	return velero.StorageManifests{}, fmt.Errorf("unsupported storage executor mode %q", e.mode)
}

func (e UnsupportedStorageExecutor) SubmitStorage(ctx context.Context, manifests velero.StorageManifests) error {
	return fmt.Errorf("unsupported storage executor mode %q", e.mode)
}
