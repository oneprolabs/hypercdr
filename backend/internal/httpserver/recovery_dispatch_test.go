package httpserver

import (
	"testing"

	"hypercdr-platform/platform/backend/internal/store"
)

func TestRecoveryDispatchPreservesSelectionMappingsAndValidation(t *testing.T) {
	router := &Router{}
	task := store.Task{ID: "task-1", ClusterID: "cluster-1", CommandID: "command-1", Type: "drill", Payload: map[string]any{
		"veleroBackupName": "backup-1", "sourceNamespace": "demo", "sourceNamespaces": []string{"demo"}, "targetNamespace": "demo-drill",
		"includedResources": []string{"deployments.apps"}, "excludedResources": []string{"secrets"},
		"storageClassMappings": map[string]string{"source-sc": "target-sc"}, "imageMappings": map[string]string{"nginx:latest": "registry.local/nginx:v1"},
		"waitForWorkloads": true, "runValidation": true, "forceStart": true, "contentCatalogLoaded": true, "persistentDataExpected": false,
	}}
	dispatch, err := router.buildStoredTaskDispatch(task)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatch.Payload.Restore
	if command == nil {
		t.Fatal("restore command is nil")
	}
	if len(command.IncludedResources) != 1 || command.ExcludedResources[0] != "secrets" || command.StorageClassMappings["source-sc"] != "target-sc" || command.ImageMappings["nginx:latest"] == "" {
		t.Fatalf("mapping fields lost: %#v", command)
	}
	if !command.WaitForWorkloads || !command.RunValidation || !command.ForceStart || !command.ContentCatalogLoaded || command.PersistentDataExpected {
		t.Fatalf("validation fields lost: %#v", command)
	}
}

func TestRecoveryConflictPolicyRecreatesGeneratedDrillNamespace(t *testing.T) {
	if got := recoveryConflictPolicy("drill", "generated", "demo", "demo-drill", "skip"); got != "replace" {
		t.Fatalf("generated drill policy = %q, want replace", got)
	}
	if got := recoveryConflictPolicy("drill", "custom", "demo", "shared-test", "skip"); got != "skip" {
		t.Fatalf("custom namespace policy = %q, want skip", got)
	}
	if got := recoveryConflictPolicy("drill", "original", "demo", "demo", "replace"); got != "replace" {
		t.Fatalf("original namespace policy = %q, want replace", got)
	}
	if got := recoveryConflictPolicy("drill", "", "demo", "demo-drill", "skip"); got != "replace" {
		t.Fatalf("legacy generated drill policy = %q, want replace", got)
	}
}
