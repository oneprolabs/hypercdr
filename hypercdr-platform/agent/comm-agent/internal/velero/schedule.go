package velero

import (
	"fmt"
	"hash/fnv"
	"strings"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type ScheduleManifest struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   ManifestMetadata     `json:"metadata"`
	Spec       ScheduleManifestSpec `json:"spec"`
}

type ScheduleManifestSpec struct {
	Schedule string             `json:"schedule"`
	Template BackupManifestSpec `json:"template"`
}

type DeleteBackupRequestManifest struct {
	APIVersion string                          `json:"apiVersion"`
	Kind       string                          `json:"kind"`
	Metadata   ManifestMetadata                `json:"metadata"`
	Spec       DeleteBackupRequestManifestSpec `json:"spec"`
}

type DeleteBackupRequestManifestSpec struct {
	BackupName string `json:"backupName"`
}

type ScheduleBuildInput struct {
	TaskID          string
	CommandID       string
	SourceClusterID string
	AgentNamespace  string
	Command         protocol.ScheduleSyncCommand
}

func BuildScheduleManifest(input ScheduleBuildInput) (ScheduleManifest, error) {
	if input.Command.PlanID == "" {
		return ScheduleManifest{}, fmt.Errorf("schedule plan id is required")
	}
	if input.Command.Cron == "" {
		return ScheduleManifest{}, fmt.Errorf("schedule cron is required")
	}
	sourceNamespaces := input.Command.SourceNamespaces
	if len(sourceNamespaces) == 0 && input.Command.SourceNamespace != "" {
		sourceNamespaces = []string{input.Command.SourceNamespace}
	}
	if len(sourceNamespaces) == 0 {
		return ScheduleManifest{}, fmt.Errorf("schedule source namespaces are required")
	}
	agentNamespace := input.AgentNamespace
	if agentNamespace == "" {
		agentNamespace = "hypercdr-agent"
	}
	name := input.Command.ScheduleName
	if name == "" {
		name = "hcdr-plan-" + sanitizeName(input.Command.PlanID)
	}
	labels := map[string]string{
		"hypercdr.io/managed-by": "hypercdr",
		"hypercdr.io/plan-id":    input.Command.PlanID,
		"hypercdr.io/task-id":    input.TaskID,
		"hypercdr.io/command-id": input.CommandID,
	}
	if input.SourceClusterID != "" {
		labels["hypercdr.io/source-cluster-id"] = input.SourceClusterID
	}
	if len(sourceNamespaces) == 1 {
		labels["hypercdr.io/source-namespace"] = sourceNamespaces[0]
	}
	template := BackupManifestSpec{
		Metadata: &BackupTemplateMetadata{
			Labels: labels,
		},
		IncludedNamespaces:       sourceNamespaces,
		StorageLocation:          input.Command.StorageRepo,
		IncludeClusterResources:  input.Command.IncludeClusterResources,
		SnapshotVolumes:          boolPtr(false),
		DefaultVolumesToFsBackup: boolPtr(true),
		ExcludedResources:        convertExcludeRules(input.Command.ExcludeResources),
	}
	if input.Command.LabelSelector != "" {
		template.LabelSelector = parseSimpleLabelSelector(input.Command.LabelSelector)
	}
	return ScheduleManifest{
		APIVersion: "velero.io/v1",
		Kind:       "Schedule",
		Metadata: ManifestMetadata{
			Name:      name,
			Namespace: agentNamespace,
			Labels:    labels,
		},
		Spec: ScheduleManifestSpec{
			Schedule: input.Command.Cron,
			Template: template,
		},
	}, nil
}

func BuildDeleteBackupRequestManifest(agentNamespace string, backupName string) (DeleteBackupRequestManifest, error) {
	if backupName == "" {
		return DeleteBackupRequestManifest{}, fmt.Errorf("backup name is required")
	}
	if agentNamespace == "" {
		agentNamespace = "hypercdr-agent"
	}
	name := deleteBackupRequestName(backupName)
	return DeleteBackupRequestManifest{
		APIVersion: "velero.io/v1",
		Kind:       "DeleteBackupRequest",
		Metadata: ManifestMetadata{
			Name:      strings.Trim(name, "-"),
			Namespace: agentNamespace,
			Labels: map[string]string{
				"hypercdr.io/managed-by": "hypercdr",
			},
		},
		Spec: DeleteBackupRequestManifestSpec{BackupName: backupName},
	}, nil
}

func deleteBackupRequestName(backupName string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(backupName))
	suffix := fmt.Sprintf("-%08x", hash.Sum32())
	prefix := "hcdr-delete-" + sanitizeName(backupName)
	maxPrefix := 63 - len(suffix)
	if len(prefix) > maxPrefix {
		prefix = strings.Trim(prefix[:maxPrefix], "-")
	}
	if prefix == "" {
		prefix = "hcdr-delete"
	}
	return strings.Trim(prefix+suffix, "-")
}
