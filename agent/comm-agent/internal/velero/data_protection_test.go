package velero

import (
	"encoding/json"
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestSwitchingToFilesystemClearsSnapshotDataMovement(t *testing.T) {
	var spec BackupManifestSpec
	if err := configureDataProtection(&spec, "snapshot-crash-consistent"); err != nil {
		t.Fatal(err)
	}
	if err := configureDataProtection(&spec, "live-filesystem"); err != nil {
		t.Fatal(err)
	}
	if spec.SnapshotMoveData != nil || *spec.SnapshotVolumes || !*spec.DefaultVolumesToFsBackup {
		t.Fatalf("filesystem mode retained CSI settings: %+v", spec)
	}
}

func TestOrdinaryManifestsRejectExplicitConsistencyGroups(t *testing.T) {
	for _, mode := range []string{"", "live-filesystem", "snapshot-crash-consistent", "application-consistent"} {
		group := &protocol.ConsistencyGroup{ID: "must-not-ignore"}
		if _, err := BuildBackupManifest(BackupBuildInput{Command: protocol.BackupCommand{SourceNamespace: "app", DataProtectionMode: mode, ConsistencyGroup: group}}); err == nil {
			t.Fatalf("ordinary backup ignored consistency group in mode %q", mode)
		}
		if _, err := BuildScheduleManifest(ScheduleBuildInput{Command: protocol.ScheduleSyncCommand{PlanID: "plan", Cron: "0 * * * *", SourceNamespace: "app", DataProtectionMode: mode, ConsistencyGroup: group}}); err == nil {
			t.Fatalf("native schedule bypassed consistency group in mode %q", mode)
		}
	}
}

func TestDataProtectionModesForManualAndScheduledBackups(t *testing.T) {
	for _, mode := range []string{"", "live-filesystem", "snapshot-crash-consistent", "application-consistent", "unexpected"} {
		t.Run(mode, func(t *testing.T) {
			manual, manualErr := BuildBackupManifest(BackupBuildInput{Command: protocol.BackupCommand{SourceNamespace: "test-app", DataProtectionMode: mode}})
			scheduled, scheduleErr := BuildScheduleManifest(ScheduleBuildInput{Command: protocol.ScheduleSyncCommand{PlanID: "test-plan", Cron: "0 * * * *", SourceNamespace: "test-app", DataProtectionMode: mode}})
			invalid := mode == "application-consistent" || mode == "unexpected"
			if (manualErr != nil) != invalid || (scheduleErr != nil) != (invalid || mode == "snapshot-crash-consistent") {
				t.Fatalf("manual error=%v schedule error=%v invalid=%v", manualErr, scheduleErr, invalid)
			}
			if invalid {
				return
			}
			specs := []BackupManifestSpec{manual.Spec}
			if scheduleErr == nil {
				specs = append(specs, scheduled.Spec.Template)
			}
			for _, spec := range specs {
				if mode == "snapshot-crash-consistent" {
					if !*spec.SnapshotVolumes || *spec.DefaultVolumesToFsBackup || spec.SnapshotMoveData == nil || !*spec.SnapshotMoveData {
						t.Fatalf("snapshot mode must move stable snapshot data, got %#v", spec)
					}
				} else if *spec.SnapshotVolumes || !*spec.DefaultVolumesToFsBackup || spec.SnapshotMoveData != nil {
					t.Fatalf("legacy behavior changed: %#v", spec)
				}
				payload, err := json.Marshal(spec)
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]any
				if err := json.Unmarshal(payload, &wire); err != nil {
					t.Fatal(err)
				}
				if mode == "snapshot-crash-consistent" && wire["snapshotMoveData"] != true {
					t.Fatalf("snapshotMoveData missing from Velero wire configuration: %s", payload)
				}
			}
		})
	}
}
