package velero

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type BackupManifest struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ManifestMetadata   `json:"metadata"`
	Spec       BackupManifestSpec `json:"spec"`
}

type ManifestMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// BackupManifestSpec mirrors the Velero v1 Backup CR. The wire shape is
// intentionally close to upstream so future fields can be added without
// protocol changes on the platform side.
type BackupManifestSpec struct {
	Metadata                 *BackupTemplateMetadata `json:"metadata,omitempty"`
	IncludedNamespaces       []string                `json:"includedNamespaces"`
	ExcludedNamespaces       []string                `json:"excludedNamespaces,omitempty"`
	ExcludedResources        []string                `json:"excludedResources,omitempty"`
	LabelSelector            map[string]string       `json:"labelSelector,omitempty"`
	StorageLocation          string                  `json:"storageLocation,omitempty"`
	IncludeClusterResources  bool                    `json:"includeClusterResources"`
	SnapshotVolumes          *bool                   `json:"snapshotVolumes,omitempty"`
	DefaultVolumesToFsBackup *bool                   `json:"defaultVolumesToFsBackup,omitempty"`
}

type BackupTemplateMetadata struct {
	Labels map[string]string `json:"labels,omitempty"`
}

type BackupBuildInput struct {
	TaskID          string
	CommandID       string
	SourceClusterID string
	AgentNamespace  string
	Command         protocol.BackupCommand
}

func BuildBackupManifest(input BackupBuildInput) (BackupManifest, error) {
	sourceNamespaces := input.Command.SourceNamespaces
	if len(sourceNamespaces) == 0 && input.Command.SourceNamespace != "" {
		sourceNamespaces = []string{input.Command.SourceNamespace}
	}
	if len(sourceNamespaces) == 0 {
		return BackupManifest{}, fmt.Errorf("backup source namespace is required")
	}
	sourceNamespace := sourceNamespaces[0]
	agentNamespace := input.AgentNamespace
	if agentNamespace == "" {
		agentNamespace = "hypercdr-agent"
	}
	name := input.Command.VeleroBackupName
	if name == "" {
		name = backupName(sourceNamespace, input.TaskID)
	}
	trigger := input.Command.Trigger
	if trigger == "" {
		trigger = "manual"
	}
	manifest := BackupManifest{
		APIVersion: "velero.io/v1",
		Kind:       "Backup",
		Metadata: ManifestMetadata{
			Name:      name,
			Namespace: agentNamespace,
			Labels: map[string]string{
				"hypercdr.io/task-id":          input.TaskID,
				"hypercdr.io/command-id":       input.CommandID,
				"hypercdr.io/managed-by":       "hypercdr",
				"hypercdr.io/source-namespace": sourceNamespace,
				"hypercdr.io/backup-mode":      trigger,
			},
		},
		Spec: BackupManifestSpec{
			IncludedNamespaces:       sourceNamespaces,
			StorageLocation:          input.Command.StorageRepo,
			IncludeClusterResources:  input.Command.IncludeClusterResources,
			SnapshotVolumes:          boolPtr(false),
			DefaultVolumesToFsBackup: boolPtr(true),
			ExcludedResources:        convertExcludeRules(input.Command.ExcludeResources),
		},
	}
	if input.Command.LabelSelector != "" {
		manifest.Spec.LabelSelector = parseSimpleLabelSelector(input.Command.LabelSelector)
	}
	if input.Command.PlanID != "" {
		manifest.Metadata.Labels["hypercdr.io/plan-id"] = input.Command.PlanID
	}
	if input.SourceClusterID != "" {
		manifest.Metadata.Labels["hypercdr.io/source-cluster-id"] = input.SourceClusterID
	}
	return manifest, nil
}

// convertExcludeRules turns protocol-level ExcludeRule entries into the
// "<group>/<resource>" form that the Velero Backup CR expects in
// spec.excludedResources. Group "core" is normalised to the empty string
// (Velero convention). Empty resources are skipped. The output preserves
// the input order and removes duplicates. If no rules survive
// normalisation, a nil slice is returned so the JSON marshaler omits the
// field entirely.
func convertExcludeRules(rules []protocol.ExcludeRule) []string {
	if len(rules) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		group := strings.TrimSpace(r.Group)
		if group == "core" {
			group = ""
		}
		for _, resourceItem := range strings.Split(r.Resource, ",") {
			resource := strings.TrimSpace(resourceItem)
			if resource == "" {
				continue
			}
			var key string
			if group == "" {
				key = resource
			} else {
				key = group + "/" + resource
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func boolPtr(value bool) *bool {
	return &value
}

func backupName(namespace string, taskID string) string {
	taskPart := sanitizeTaskID(taskID)
	base := "hcdr-" + sanitizeName(namespace) + "-" + time.Now().UTC().Format("20060102150405") + "-" + taskPart
	if len(base) > 63 {
		return base[:63]
	}
	return base
}

func sanitizeTaskID(taskID string) string {
	taskPart := strings.ReplaceAll(taskID, "-", "")
	if len(taskPart) > 8 {
		taskPart = taskPart[:8]
	}
	if taskPart == "" {
		return "task"
	}
	return taskPart
}

func sanitizeName(value string) string {
	value = strings.ToLower(value)
	re := regexp.MustCompile(`[^a-z0-9-]+`)
	value = re.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "backup"
	}
	return value
}

func parseSimpleLabelSelector(selector string) map[string]string {
	labels := map[string]string{}
	parts := strings.Split(selector, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || strings.Contains(part, "!=") {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			labels[key] = value
		}
	}
	return labels
}
