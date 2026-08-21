package velero

import (
	"encoding/json"
	"fmt"
	"sort"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type RestoreManifest struct {
	APIVersion           string              `json:"apiVersion"`
	Kind                 string              `json:"kind"`
	Metadata             ManifestMetadata    `json:"metadata"`
	Spec                 RestoreManifestSpec `json:"spec"`
	StorageClassMappings map[string]string   `json:"-"`
	ImageMappings        map[string]string   `json:"-"`
}

type RestoreManifestSpec struct {
	BackupName               string                     `json:"backupName"`
	IncludedNamespaces       []string                   `json:"includedNamespaces,omitempty"`
	NamespaceMapping         map[string]string          `json:"namespaceMapping,omitempty"`
	IncludeClusterResources  bool                       `json:"includeClusterResources"`
	ExistingResourcePolicy   string                     `json:"existingResourcePolicy,omitempty"`
	DefaultVolumesToFsBackup *bool                      `json:"defaultVolumesToFsBackup,omitempty"`
	PreserveNodePorts        *bool                      `json:"preserveNodePorts,omitempty"`
	ResourceModifier         *TypedLocalObjectReference `json:"resourceModifier,omitempty"`
	IncludedResources        []string                   `json:"includedResources,omitempty"`
	ExcludedResources        []string                   `json:"excludedResources,omitempty"`
}

type RestoreBuildInput struct {
	TaskID         string
	CommandID      string
	TaskType       string
	AgentNamespace string
	Command        protocol.RestoreCommand
}

type TypedLocalObjectReference struct {
	APIGroup *string `json:"apiGroup,omitempty"`
	Kind     string  `json:"kind"`
	Name     string  `json:"name"`
}

type ResourceModifierConfigMap struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   ManifestMetadata  `json:"metadata"`
	Data       map[string]string `json:"data"`
}

func BuildRestoreManifest(input RestoreBuildInput) (RestoreManifest, error) {
	if input.Command.VeleroBackupName == "" {
		return RestoreManifest{}, fmt.Errorf("restore velero backup name is required")
	}
	sourceNamespaces := input.Command.SourceNamespaces
	if len(sourceNamespaces) == 0 && input.Command.SourceNamespace != "" {
		sourceNamespaces = []string{input.Command.SourceNamespace}
	}
	if len(sourceNamespaces) == 0 {
		return RestoreManifest{}, fmt.Errorf("restore source namespace is required")
	}
	sourceNamespace := sourceNamespaces[0]
	agentNamespace := input.AgentNamespace
	if agentNamespace == "" {
		agentNamespace = "hypercdr-agent"
	}

	manifest := RestoreManifest{
		APIVersion: "velero.io/v1",
		Kind:       "Restore",
		Metadata: ManifestMetadata{
			Name:      restoreName(sourceNamespace, input.TaskID),
			Namespace: agentNamespace,
			Labels: map[string]string{
				"hypercdr.io/task-id":          input.TaskID,
				"hypercdr.io/command-id":       input.CommandID,
				"hypercdr.io/task-type":        input.TaskType,
				"hypercdr.io/restore-mode":     input.Command.RestoreMode,
				"hypercdr.io/conflict-policy":  input.Command.ConflictPolicy,
				"hypercdr.io/source-namespace": sourceNamespace,
				"hypercdr.io/target-namespace": input.Command.TargetNamespace,
			},
		},
		Spec: RestoreManifestSpec{
			BackupName:               input.Command.VeleroBackupName,
			IncludedNamespaces:       sourceNamespaces,
			IncludeClusterResources:  input.Command.IncludeClusterScoped,
			ExistingResourcePolicy:   existingResourcePolicy(input.Command.ConflictPolicy),
			DefaultVolumesToFsBackup: boolPtr(true),
			PreserveNodePorts:        boolPtr(shouldPreserveNodePorts(input.Command)),
			IncludedResources:        input.Command.IncludedResources,
			ExcludedResources:        input.Command.ExcludedResources,
		},
		StorageClassMappings: input.Command.StorageClassMappings,
		ImageMappings:        input.Command.ImageMappings,
	}
	manifest.Spec.ResourceModifier = &TypedLocalObjectReference{
		APIGroup: stringPtr(""),
		Kind:     "configmap",
		Name:     resourceModifierName(manifest.Metadata.Name),
	}
	if len(input.Command.TargetNamespaces) > 0 {
		manifest.Spec.NamespaceMapping = input.Command.TargetNamespaces
	} else if input.Command.TargetNamespace != "" && input.Command.TargetNamespace != sourceNamespace {
		manifest.Spec.NamespaceMapping = map[string]string{sourceNamespace: input.Command.TargetNamespace}
	}
	return manifest, nil
}

func BuildRestoreResourceModifierConfigMap(manifest RestoreManifest) ResourceModifierConfigMap {
	namespaces := manifest.Spec.IncludedNamespaces
	if len(namespaces) == 0 {
		namespaces = []string{"*"}
	}
	preserveNodePorts := manifest.Spec.PreserveNodePorts != nil && *manifest.Spec.PreserveNodePorts
	return ResourceModifierConfigMap{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: ManifestMetadata{
			Name:      resourceModifierName(manifest.Metadata.Name),
			Namespace: manifest.Metadata.Namespace,
			Labels:    manifest.Metadata.Labels,
		},
		Data: map[string]string{
			"resource-modifiers.yaml": restoreResourceModifierYAML(namespaces, preserveNodePorts, manifest.StorageClassMappings, manifest.ImageMappings),
		},
	}
}

func restoreName(namespace string, taskID string) string {
	taskPart := sanitizeTaskID(taskID)
	base := "hcdr-restore-" + sanitizeName(namespace) + "-" + taskPart
	if len(base) > 63 {
		return base[:63]
	}
	return base
}

func resourceModifierName(restoreName string) string {
	const suffix = "-restore-mod"
	if len(restoreName)+len(suffix) <= 63 {
		return restoreName + suffix
	}
	return restoreName[:63-len(suffix)] + suffix
}

func restoreResourceModifierYAML(namespaces []string, preserveNodePorts bool, storageMappings map[string]string, imageMappings map[string]string) string {
	yaml := "version: v1\nresourceModifierRules:\n- conditions:\n    groupResource: persistentvolumeclaims\n    namespaces:\n"
	for _, namespace := range namespaces {
		yaml += "    - " + namespace + "\n"
	}
	yaml += "  mergePatches:\n  - patchData: |\n      {\"spec\":{\"volumeName\":null}}\n"
	if !preserveNodePorts {
		yaml += "- conditions:\n    groupResource: services\n    namespaces:\n"
		for _, namespace := range namespaces {
			yaml += "    - " + namespace + "\n"
		}
		yaml += "    matches:\n    - path: \"/spec/type\"\n      value: \"NodePort\"\n"
		yaml += "  patches:\n  - operation: remove\n    path: \"/spec/ports/0/nodePort\"\n"
	}
	for _, source := range sortedMappingKeys(storageMappings) {
		yaml += "- conditions:\n    groupResource: persistentvolumeclaims\n    namespaces:\n"
		for _, namespace := range namespaces {
			yaml += "    - " + namespace + "\n"
		}
		yaml += "    matches:\n    - path: /spec/storageClassName\n      value: " + yamlString(source) + "\n"
		yaml += "  patches:\n  - operation: replace\n    path: /spec/storageClassName\n    value: " + yamlString(storageMappings[source]) + "\n"
	}
	for _, source := range sortedMappingKeys(imageMappings) {
		for _, resource := range []struct{ group, path string }{{"deployments.apps", "/spec/template/spec"}, {"statefulsets.apps", "/spec/template/spec"}, {"daemonsets.apps", "/spec/template/spec"}, {"jobs.batch", "/spec/template/spec"}, {"cronjobs.batch", "/spec/jobTemplate/spec/template/spec"}, {"pods", "/spec"}} {
			for _, containers := range []string{"containers", "initContainers"} {
				for index := 0; index < 10; index++ {
					path := fmt.Sprintf("%s/%s/%d/image", resource.path, containers, index)
					yaml += "- conditions:\n    groupResource: " + resource.group + "\n    namespaces:\n"
					for _, namespace := range namespaces {
						yaml += "    - " + namespace + "\n"
					}
					yaml += "    matches:\n    - path: " + path + "\n      value: " + yamlString(source) + "\n"
					yaml += "  patches:\n  - operation: replace\n    path: " + path + "\n    value: " + yamlString(imageMappings[source]) + "\n"
				}
			}
		}
	}
	return yaml
}

func sortedMappingKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key != "" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func yamlString(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }

func shouldPreserveNodePorts(command protocol.RestoreCommand) bool {
	return command.ConflictPolicy == "replace" && command.TargetNamespace == command.SourceNamespace
}

func existingResourcePolicy(conflictPolicy string) string {
	switch conflictPolicy {
	case "update", "overwrite", "replace":
		return "update"
	default:
		return "none"
	}
}

func stringPtr(value string) *string {
	return &value
}
