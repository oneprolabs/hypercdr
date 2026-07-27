package kube

import (
	"context"
	"encoding/json"
	"fmt"
)

type Manifest map[string]any

type AppliedObject struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

type ManifestApplier interface {
	ApplyManifest(ctx context.Context, manifest Manifest) (AppliedObject, error)
}

type ObjectDeleter interface {
	DeleteObject(ctx context.Context, object AppliedObject) error
}

type NamespaceReplacer interface {
	ReplaceNamespaceAndWait(ctx context.Context, namespace string) error
}

type RestoreStateCleaner interface {
	CleanupStaleRestoreState(ctx context.Context, agentNamespace string, sourceNamespace string, targetNamespace string, currentRestoreName string) error
}

type VeleroProtectionCleaner interface {
	DeleteVeleroBackupArtifacts(ctx context.Context, agentNamespace string, backupNames []string) (map[string][]string, error)
	DeleteVeleroBackupArtifactsByNamePrefix(ctx context.Context, agentNamespace string, backupNamePrefix string) (map[string][]string, error)
	DeleteBackupRepositories(ctx context.Context, agentNamespace string, storageLocation string, namespaces []string) ([]string, error)
	DeleteBackupObjectsByNamePrefix(ctx context.Context, agentNamespace string, storageLocation string, backupNamePrefix string) ([]string, error)
	DeleteRestoreObjects(ctx context.Context, agentNamespace string, storageLocation string, restoreNames []string) ([]string, error)
	DeleteKopiaRepositories(ctx context.Context, agentNamespace string, storageLocation string, namespaces []string) ([]string, error)
}

func ManifestFromStruct(value any) (Manifest, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func ObjectFromManifest(manifest Manifest) (AppliedObject, error) {
	object := AppliedObject{}
	object.APIVersion, _ = manifest["apiVersion"].(string)
	object.Kind, _ = manifest["kind"].(string)
	metadata, _ := manifest["metadata"].(map[string]any)
	if metadata != nil {
		object.Name, _ = metadata["name"].(string)
		object.Namespace, _ = metadata["namespace"].(string)
	}
	if object.APIVersion == "" || object.Kind == "" || object.Name == "" {
		return AppliedObject{}, fmt.Errorf("manifest apiVersion, kind, and metadata.name are required")
	}
	return object, nil
}
