package velero

import (
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestNativeScheduleRejectsStrictModesEvenWithoutGroup(t *testing.T) {
	for _, mode := range []string{"application-consistent", "snapshot-crash-consistent", "unknown", " application-consistent "} {
		for _, withGroup := range []bool{false, true} {
			command := protocol.ScheduleSyncCommand{PlanID: "plan", Cron: "0 * * * *", SourceNamespace: "app", DataProtectionMode: mode}
			if withGroup {
				command.ConsistencyGroup = &protocol.ConsistencyGroup{ID: "group"}
			}
			if _, err := BuildScheduleManifest(ScheduleBuildInput{Command: command}); err == nil {
				t.Fatalf("native Schedule accepted mode %q with group=%v", mode, withGroup)
			}
		}
	}
}
