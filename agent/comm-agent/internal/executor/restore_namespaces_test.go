package executor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"hypercdr-platform/agent/comm-agent/internal/velero"
)

func TestRestoreReplacementScope(t *testing.T) {
	for _, test := range []struct {
		name    string
		sources []string
		mapping map[string]string
		want    []string
		invalid bool
	}{
		{name: "original namespaces", sources: []string{"mysql", "nextcloud"}, want: []string{"mysql", "nextcloud"}},
		{name: "selected clones only", sources: []string{"mysql", "nextcloud"}, mapping: map[string]string{"mysql": "mysql-copy", "nextcloud": "nextcloud-copy", "unselected": "untouched"}, want: []string{"mysql-copy", "nextcloud-copy"}},
		{name: "agent namespace second", sources: []string{"mysql", "hypercdr-agent"}, invalid: true},
		{name: "mapped agent namespace", sources: []string{"mysql", "nextcloud"}, mapping: map[string]string{"nextcloud": "hypercdr-agent"}, invalid: true},
		{name: "duplicate target", sources: []string{"mysql", "nextcloud"}, mapping: map[string]string{"nextcloud": "mysql"}, invalid: true},
		{name: "empty target", sources: []string{"mysql"}, mapping: map[string]string{"mysql": ""}, invalid: true},
		{name: "missing sources", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			applier := &fakeNamespaceReplacingApplier{}
			executor := KubernetesRestoreExecutor{agentNamespace: "hypercdr-agent", applier: applier}
			manifest := velero.RestoreManifest{}
			manifest.APIVersion = "velero.io/v1"
			manifest.Kind = "Restore"
			manifest.Metadata.Name = "test-restore"
			manifest.Metadata.Namespace = "hypercdr-agent"
			manifest.Metadata.Labels = map[string]string{"hypercdr.io/conflict-policy": "replace"}
			manifest.Spec.IncludedNamespaces = test.sources
			manifest.Spec.NamespaceMapping = test.mapping
			err := executor.SubmitRestore(context.Background(), manifest)
			if test.invalid {
				if err == nil || len(applier.cleanedNamespaces) != 0 || len(applier.replacedNamespaces) != 0 || len(applier.manifests) != 0 {
					t.Fatalf("invalid scope mutated cluster: err=%v applier=%+v", err, applier)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(applier.replacedNamespaces, test.want) {
				t.Fatalf("replaced %v, want %v", applier.replacedNamespaces, test.want)
			}
			for index, source := range test.sources {
				if applier.cleanedNamespaces[index] != [2]string{source, test.want[index]} {
					t.Fatalf("unexpected cleanup scope: %v", applier.cleanedNamespaces)
				}
			}
			if len(applier.manifests) != 2 {
				t.Fatalf("expected modifier and restore, got %d", len(applier.manifests))
			}
		})
	}
}

func TestRestoreReplacementFailureDoesNotSubmitRestore(t *testing.T) {
	applier := &fakeNamespaceReplacingApplier{failNamespace: "nextcloud", replaceNamespaceErr: errors.New("delete denied")}
	executor := KubernetesRestoreExecutor{agentNamespace: "hypercdr-agent", applier: applier}
	manifest := velero.RestoreManifest{}
	manifest.Metadata.Namespace = "hypercdr-agent"
	manifest.Metadata.Labels = map[string]string{"hypercdr.io/conflict-policy": "replace"}
	manifest.Spec.IncludedNamespaces = []string{"mysql", "nextcloud"}
	err := executor.SubmitRestore(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "nextcloud") || !errors.Is(err, applier.replaceNamespaceErr) || len(applier.manifests) != 0 {
		t.Fatalf("unexpected result: err=%v manifests=%v", err, applier.manifests)
	}
}

func TestRestoreCleanupFailureDoesNotReplaceNamespaces(t *testing.T) {
	applier := &fakeNamespaceReplacingApplier{failedCleanupNamespace: "nextcloud", cleanupNamespaceErr: errors.New("cleanup denied")}
	executor := KubernetesRestoreExecutor{agentNamespace: "hypercdr-agent", applier: applier}
	manifest := velero.RestoreManifest{}
	manifest.Metadata.Namespace = "hypercdr-agent"
	manifest.Metadata.Labels = map[string]string{"hypercdr.io/conflict-policy": "replace"}
	manifest.Spec.IncludedNamespaces = []string{"mysql", "nextcloud"}
	err := executor.SubmitRestore(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "nextcloud") || !errors.Is(err, applier.cleanupNamespaceErr) || len(applier.replacedNamespaces) != 0 || len(applier.manifests) != 0 {
		t.Fatalf("cleanup failure mutated namespaces: err=%v applier=%+v", err, applier)
	}
}
