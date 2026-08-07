package velero

import (
	"encoding/json"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestBuildBackupManifestPopulatesExcludedResourcesFromCommand(t *testing.T) {
	cmd := protocol.BackupCommand{
		SourceNamespace:   "kasten-io",
		StorageRepo:       "my-minio",
		IncludedResources: []string{"deployments.apps", "services"},
		LabelSelector:     protocol.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
		ExcludedResources: []string{"configmaps", "secrets"},
	}
	manifest, err := BuildBackupManifest(BackupBuildInput{
		TaskID:         "task-1",
		CommandID:      "cmd-1",
		AgentNamespace: "hypercdr-agent",
		Command:        cmd,
	})
	if err != nil {
		t.Fatalf("BuildBackupManifest failed: %v", err)
	}
	if got, want := manifest.Spec.IncludedNamespaces, []string{"kasten-io"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("IncludedNamespaces = %v, want %v", got, want)
	}
	if manifest.Spec.DefaultVolumesToFsBackup == nil || !*manifest.Spec.DefaultVolumesToFsBackup {
		t.Errorf("DefaultVolumesToFsBackup should be true for kasten-io style workloads")
	}
	wantExcluded := []string{"configmaps", "secrets"}
	if len(manifest.Spec.ExcludedResources) != len(wantExcluded) {
		t.Fatalf("ExcludedResources = %v, want %v", manifest.Spec.ExcludedResources, wantExcluded)
	}
	if len(manifest.Spec.IncludedResources) != 2 || manifest.Spec.IncludedResources[0] != "deployments.apps" {
		t.Fatalf("IncludedResources = %v", manifest.Spec.IncludedResources)
	}
	if manifest.Spec.LabelSelector == nil || manifest.Spec.LabelSelector.MatchLabels["app"] != "demo" {
		t.Fatalf("LabelSelector = %#v", manifest.Spec.LabelSelector)
	}
	for i, want := range wantExcluded {
		if manifest.Spec.ExcludedResources[i] != want {
			t.Errorf("ExcludedResources[%d] = %q, want %q", i, manifest.Spec.ExcludedResources[i], want)
		}
	}
}

func TestBuildBackupManifestOmitsExcludedResourcesWhenEmpty(t *testing.T) {
	cmd := protocol.BackupCommand{
		SourceNamespace: "dev-test",
		StorageRepo:     "my-minio",
	}
	manifest, err := BuildBackupManifest(BackupBuildInput{
		TaskID:    "task-2",
		CommandID: "cmd-2",
		Command:   cmd,
	})
	if err != nil {
		t.Fatalf("BuildBackupManifest failed: %v", err)
	}
	if len(manifest.Spec.ExcludedResources) != 0 {
		t.Errorf("ExcludedResources should be empty, got %v", manifest.Spec.ExcludedResources)
	}

	// Round-trip through JSON: the field should be omitted when empty.
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if containsString(string(payload), "excludedResources") {
		t.Errorf("expected excludedResources to be omitted in JSON, got: %s", payload)
	}
}

func TestBuildBackupManifestAddsSourceClusterLabel(t *testing.T) {
	manifest, err := BuildBackupManifest(BackupBuildInput{
		TaskID:          "task-2",
		CommandID:       "cmd-2",
		SourceClusterID: "cluster-source",
		Command: protocol.BackupCommand{
			SourceNamespace: "dev-test",
			StorageRepo:     "my-minio",
		},
	})
	if err != nil {
		t.Fatalf("BuildBackupManifest failed: %v", err)
	}
	if got := manifest.Metadata.Labels["hypercdr.io/source-cluster-id"]; got != "cluster-source" {
		t.Fatalf("source cluster label = %q, want cluster-source", got)
	}
}

func TestBuildBackupManifestUsesVeleroScopedResourceFields(t *testing.T) {
	manifest, err := BuildBackupManifest(BackupBuildInput{
		TaskID: "task-scoped",
		Command: protocol.BackupCommand{
			SourceNamespace:         "demo",
			StorageRepo:             "my-minio",
			IncludedResources:       []string{"deployments.apps"},
			ExcludedResources:       []string{"secrets"},
			IncludeClusterResources: true,
			ResourceSelection: protocol.ResourceSelection{
				Mode:            "custom",
				NamespaceScoped: []string{"deployments.apps", "persistentvolumeclaims"},
				ClusterScoped:   []string{"persistentvolumes"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildBackupManifest failed: %v", err)
	}
	if len(manifest.Spec.IncludedResources) != 0 || len(manifest.Spec.ExcludedResources) != 0 {
		t.Fatalf("legacy filters must be omitted for scoped selection: %#v", manifest.Spec)
	}
	if manifest.Spec.IncludeClusterResources != nil {
		t.Fatal("legacy includeClusterResources must be omitted for scoped selection")
	}
	if got := manifest.Spec.IncludedNamespaceScopedResources; len(got) != 2 || got[0] != "deployments.apps" {
		t.Fatalf("namespace scoped resources = %v", got)
	}
	if got := manifest.Spec.IncludedClusterScopedResources; len(got) != 1 || got[0] != "persistentvolumes" {
		t.Fatalf("cluster scoped resources = %v", got)
	}
}

func TestBuildScheduleManifestAddsSourceClusterLabel(t *testing.T) {
	manifest, err := BuildScheduleManifest(ScheduleBuildInput{
		TaskID:          "task-schedule",
		CommandID:       "cmd-schedule",
		SourceClusterID: "cluster-source",
		Command: protocol.ScheduleSyncCommand{
			PlanID:           "plan-a",
			Cron:             "0 * * * *",
			SourceNamespaces: []string{"dev-test"},
			StorageRepo:      "my-minio",
		},
	})
	if err != nil {
		t.Fatalf("BuildScheduleManifest failed: %v", err)
	}
	if got := manifest.Metadata.Labels["hypercdr.io/source-cluster-id"]; got != "cluster-source" {
		t.Fatalf("schedule source cluster label = %q, want cluster-source", got)
	}
	if got := manifest.Spec.Template.Metadata.Labels["hypercdr.io/source-cluster-id"]; got != "cluster-source" {
		t.Fatalf("template source cluster label = %q, want cluster-source", got)
	}
}

func TestBuildScheduleManifestUsesVeleroScopedResourceFields(t *testing.T) {
	manifest, err := BuildScheduleManifest(ScheduleBuildInput{
		TaskID: "task-scoped-schedule",
		Command: protocol.ScheduleSyncCommand{
			PlanID:                  "plan-scoped",
			Cron:                    "0 * * * *",
			SourceNamespaces:        []string{"demo"},
			StorageRepo:             "my-minio",
			IncludeClusterResources: true,
			ExcludeResources:        []protocol.ExcludeRule{{Resource: "secrets"}},
			ResourceSelection: protocol.ResourceSelection{
				Mode:            "custom",
				NamespaceScoped: []string{"deployments.apps"},
				ClusterScoped:   []string{"persistentvolumes"},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildScheduleManifest failed: %v", err)
	}
	template := manifest.Spec.Template
	if len(template.IncludedResources) != 0 || len(template.ExcludedResources) != 0 || template.IncludeClusterResources != nil {
		t.Fatalf("legacy fields must be omitted for scoped selection: %#v", template)
	}
	if got := template.IncludedNamespaceScopedResources; len(got) != 1 || got[0] != "deployments.apps" {
		t.Fatalf("namespace scoped resources = %v", got)
	}
	if got := template.IncludedClusterScopedResources; len(got) != 1 || got[0] != "persistentvolumes" {
		t.Fatalf("cluster scoped resources = %v", got)
	}
}

func TestBuildBackupManifestRejectsEmptySourceNamespace(t *testing.T) {
	if _, err := BuildBackupManifest(BackupBuildInput{
		TaskID: "task-3",
		Command: protocol.BackupCommand{
			StorageRepo: "my-minio",
		},
	}); err == nil {
		t.Fatal("expected error for empty source namespace")
	}
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
