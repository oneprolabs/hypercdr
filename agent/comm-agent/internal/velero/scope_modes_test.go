package velero

import (
	"encoding/json"
	"reflect"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

// Exercise both immediate and scheduled backup builders with the exact wire
// modes used by Scope / Custom; assert the serialized CR, not just the UI state.
func TestApplicationScopeModesBuildMatchingBackupAndScheduleFilters(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		selection                                            protocol.ResourceSelection
		included, excluded, clusterIncluded, clusterExcluded []string
	}{
		{name: "selected types", selection: protocol.ResourceSelection{Mode: "custom", NamespaceScoped: []string{"deployments.apps", "secrets"}}, included: []string{"deployments.apps", "secrets"}, clusterExcluded: []string{"*"}},
		{name: "exclude types", selection: protocol.ResourceSelection{Mode: "exclude", NamespaceScoped: []string{"events"}}, included: []string{"*"}, excluded: []string{"events", "events.events.k8s.io"}, clusterExcluded: []string{"*"}},
		{name: "empty exclusions", selection: protocol.ResourceSelection{Mode: "exclude"}, included: []string{"*"}, clusterExcluded: []string{"*"}},
		{name: "legacy cluster selection", selection: protocol.ResourceSelection{Mode: "custom", NamespaceScoped: []string{"deployments.apps"}, ClusterScoped: []string{"storageclasses.storage.k8s.io"}}, included: []string{"deployments.apps"}, clusterIncluded: []string{"storageclasses.storage.k8s.io"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backup, err := BuildBackupManifest(BackupBuildInput{TaskID: "scope-backup", Command: protocol.BackupCommand{SourceNamespace: "demo", IncludedResources: []string{"stale"}, ExcludedResources: []string{"stale"}, IncludeClusterResources: true, ResourceSelection: tc.selection}})
			if err != nil {
				t.Fatal(err)
			}
			schedule, err := BuildScheduleManifest(ScheduleBuildInput{TaskID: "scope-schedule", Command: protocol.ScheduleSyncCommand{PlanID: "scope-plan", SourceNamespace: "demo", Cron: "0 * * * *", IncludedResources: []string{"stale"}, IncludeClusterResources: true, ResourceSelection: tc.selection}})
			if err != nil {
				t.Fatal(err)
			}
			for name, spec := range map[string]BackupManifestSpec{"backup": backup.Spec, "schedule": schedule.Spec.Template} {
				for field, pair := range map[string][2][]string{
					"includedNamespaceScopedResources": {spec.IncludedNamespaceScopedResources, tc.included},
					"excludedNamespaceScopedResources": {spec.ExcludedNamespaceScopedResources, tc.excluded},
					"includedClusterScopedResources":   {spec.IncludedClusterScopedResources, tc.clusterIncluded},
					"excludedClusterScopedResources":   {spec.ExcludedClusterScopedResources, tc.clusterExcluded},
				} {
					if len(pair[0]) != len(pair[1]) || len(pair[1]) > 0 && !reflect.DeepEqual(pair[0], pair[1]) {
						t.Fatalf("%s %s = %v, want %v", name, field, pair[0], pair[1])
					}
				}
				raw, err := json.Marshal(spec)
				if err != nil {
					t.Fatal(err)
				}
				var payload map[string]any
				if err = json.Unmarshal(raw, &payload); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"includedResources", "excludedResources", "includeClusterResources"} {
					if _, found := payload[field]; found {
						t.Fatalf("%s mixes legacy field %s: %s", name, field, raw)
					}
				}
			}
		})
	}
}
