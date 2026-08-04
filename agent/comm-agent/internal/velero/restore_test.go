package velero

import (
	"strings"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestBuildRestoreManifestDisablesPreserveNodePorts(t *testing.T) {
	manifest, err := BuildRestoreManifest(RestoreBuildInput{
		TaskID:         "task-12345678",
		CommandID:      "cmd-12345678",
		TaskType:       "drill",
		AgentNamespace: "hypercdr-agent",
		Command: protocol.RestoreCommand{
			VeleroBackupName:     "backup-demo",
			SourceNamespace:      "demo-mysql-csi",
			TargetNamespace:      "demo-mysql-csi-drill",
			ConflictPolicy:       "skip",
			IncludeClusterScoped: false,
		},
	})
	if err != nil {
		t.Fatalf("BuildRestoreManifest returned error: %v", err)
	}
	if got := manifest.Spec.NamespaceMapping["demo-mysql-csi"]; got != "demo-mysql-csi-drill" {
		t.Fatalf("namespace mapping = %q, want demo-mysql-csi-drill", got)
	}
	if manifest.Spec.DefaultVolumesToFsBackup == nil || !*manifest.Spec.DefaultVolumesToFsBackup {
		t.Fatalf("DefaultVolumesToFsBackup should be true")
	}
	if manifest.Spec.PreserveNodePorts == nil {
		t.Fatalf("PreserveNodePorts should be set")
	}
	if *manifest.Spec.PreserveNodePorts {
		t.Fatalf("PreserveNodePorts should be false for drill restores")
	}
	if manifest.Spec.ResourceModifier == nil {
		t.Fatalf("ResourceModifier should be set")
	}
	if manifest.Spec.ResourceModifier.Kind != "configmap" {
		t.Fatalf("ResourceModifier kind = %q, want configmap", manifest.Spec.ResourceModifier.Kind)
	}
	if manifest.Spec.ResourceModifier.Name == "" {
		t.Fatalf("ResourceModifier name should be set")
	}
}

func TestBuildRestoreResourceModifierConfigMapClearsPVCVolumeName(t *testing.T) {
	manifest, err := BuildRestoreManifest(RestoreBuildInput{
		TaskID:         "task-12345678",
		CommandID:      "cmd-12345678",
		TaskType:       "drill",
		AgentNamespace: "hypercdr-agent",
		Command: protocol.RestoreCommand{
			VeleroBackupName: "backup-demo",
			SourceNamespace:  "demo-mysql-csi",
			TargetNamespace:  "demo-mysql-csi-drill",
		},
	})
	if err != nil {
		t.Fatalf("BuildRestoreManifest returned error: %v", err)
	}

	configMap := BuildRestoreResourceModifierConfigMap(manifest)
	if configMap.Kind != "ConfigMap" {
		t.Fatalf("kind = %q, want ConfigMap", configMap.Kind)
	}
	if configMap.Metadata.Name != manifest.Spec.ResourceModifier.Name {
		t.Fatalf("config map name = %q, want %q", configMap.Metadata.Name, manifest.Spec.ResourceModifier.Name)
	}
	data := configMap.Data["resource-modifiers.yaml"]
	for _, want := range []string{
		"groupResource: persistentvolumeclaims",
		"- demo-mysql-csi",
		"mergePatches:",
		"{\"spec\":{\"volumeName\":null}}",
		"groupResource: services",
		"path: \"/spec/type\"",
		"value: \"NodePort\"",
		"path: \"/spec/ports/0/nodePort\"",
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("resource modifier yaml missing %q:\n%s", want, data)
		}
	}
}

func TestBuildRestoreResourceModifierConfigMapIncludesEnvironmentMappings(t *testing.T) {
	manifest, err := BuildRestoreManifest(RestoreBuildInput{TaskID: "task-map", CommandID: "cmd-map", TaskType: "drill", AgentNamespace: "hypercdr-agent", Command: protocol.RestoreCommand{VeleroBackupName: "backup-1", SourceNamespace: "demo", TargetNamespace: "demo-drill", StorageClassMappings: map[string]string{"source-sc": "target-sc"}, ImageMappings: map[string]string{"docker.io/library/nginx:latest": "registry.local/nginx:v1"}}})
	if err != nil {
		t.Fatal(err)
	}
	yaml := BuildRestoreResourceModifierConfigMap(manifest).Data["resource-modifiers.yaml"]
	for _, expected := range []string{"source-sc", "target-sc", "docker.io/library/nginx:latest", "registry.local/nginx:v1", "/spec/template/spec/containers/0/image"} {
		if !strings.Contains(yaml, expected) {
			t.Fatalf("modifier does not contain %q: %s", expected, yaml)
		}
	}
}

func TestBuildRestoreManifestPreservesNodePortForNamespaceReplacement(t *testing.T) {
	manifest, err := BuildRestoreManifest(RestoreBuildInput{
		TaskID:         "task-12345678",
		CommandID:      "cmd-12345678",
		TaskType:       "drill",
		AgentNamespace: "hypercdr-agent",
		Command: protocol.RestoreCommand{
			VeleroBackupName: "backup-demo",
			SourceNamespace:  "demo-mysql-csi",
			TargetNamespace:  "demo-mysql-csi",
			ConflictPolicy:   "replace",
		},
	})
	if err != nil {
		t.Fatalf("BuildRestoreManifest returned error: %v", err)
	}
	if manifest.Spec.PreserveNodePorts == nil || !*manifest.Spec.PreserveNodePorts {
		t.Fatalf("PreserveNodePorts should be true for namespace replacement")
	}

	configMap := BuildRestoreResourceModifierConfigMap(manifest)
	data := configMap.Data["resource-modifiers.yaml"]
	if strings.Contains(data, "groupResource: services") {
		t.Fatalf("service nodePort modifier should be omitted when preserving node ports:\n%s", data)
	}
}

func TestBuildRestoreManifestRemovesNodePortForCloneRestore(t *testing.T) {
	manifest, err := BuildRestoreManifest(RestoreBuildInput{
		TaskID:         "task-12345678",
		CommandID:      "cmd-12345678",
		TaskType:       "drill",
		AgentNamespace: "hypercdr-agent",
		Command: protocol.RestoreCommand{
			VeleroBackupName: "backup-demo",
			SourceNamespace:  "demo-mysql-csi",
			TargetNamespace:  "demo-mysql-csi-drill",
			ConflictPolicy:   "skip",
		},
	})
	if err != nil {
		t.Fatalf("BuildRestoreManifest returned error: %v", err)
	}
	if manifest.Spec.PreserveNodePorts == nil || *manifest.Spec.PreserveNodePorts {
		t.Fatalf("PreserveNodePorts should be false for clone restores")
	}

	configMap := BuildRestoreResourceModifierConfigMap(manifest)
	data := configMap.Data["resource-modifiers.yaml"]
	if !strings.Contains(data, "groupResource: services") {
		t.Fatalf("service nodePort modifier should be present for clone restores:\n%s", data)
	}
}

func TestBuildRestoreManifestDoesNotExcludeNamespacedCRsByDefault(t *testing.T) {
	manifest, err := BuildRestoreManifest(RestoreBuildInput{
		TaskID: "task-no-implicit-exclusions", TaskType: "drill",
		Command: protocol.RestoreCommand{VeleroBackupName: "backup-demo", SourceNamespace: "demo"},
	})
	if err != nil {
		t.Fatalf("BuildRestoreManifest returned error: %v", err)
	}
	if len(manifest.Spec.ExcludedResources) != 0 {
		t.Fatalf("namespaced CRs must not be excluded by default: %v", manifest.Spec.ExcludedResources)
	}
}

func TestBuildRestoreManifestUsesOnlyExplicitExclusions(t *testing.T) {
	want := []string{"backupactions.actions.kio.kasten.io"}
	manifest, err := BuildRestoreManifest(RestoreBuildInput{TaskID: "task-explicit", TaskType: "drill", Command: protocol.RestoreCommand{VeleroBackupName: "backup-demo", SourceNamespace: "demo", ExcludedResources: want}})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Spec.ExcludedResources) != 1 || manifest.Spec.ExcludedResources[0] != want[0] {
		t.Fatalf("ExcludedResources=%v, want %v", manifest.Spec.ExcludedResources, want)
	}
}
