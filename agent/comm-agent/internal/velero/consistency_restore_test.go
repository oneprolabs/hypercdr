package velero

import (
	"os"
	"strings"
	"testing"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/consistency"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
	"sigs.k8s.io/yaml"
)

func completedConsistencyFixture() consistency.Record {
	record := consistency.Record{
		ID: "123", GroupID: "group", Strategy: "offline-v1", Phase: consistency.Completed, Resumed: true, LocksReleased: true,
		ApplicationProfile: "qualified-database-v1", StartedAt: time.Now().UTC().Add(-time.Minute), Deadline: time.Now().UTC().Add(time.Minute),
		Volumes:   []consistency.Volume{{Namespace: "app", Name: "data", UID: "pvc-uid"}},
		Snapshots: []consistency.Snapshot{{VolumeUID: "pvc-uid", Handle: "repository-snapshot", DataUploadUID: "upload-uid", Source: "kopia", Secured: true}},
		Workloads: []consistency.WorkloadState{{Namespace: "app", Name: "database", Kind: "Deployment", UID: "db-uid", Generation: 3, Replicas: 2}},
	}
	proof, err := consistency.NewQuiescenceProof(record, time.Now().UTC(), []byte(`{"verified":true}`))
	if err != nil {
		panic(err)
	}
	record.QuiescenceProof = proof
	return record
}

func TestConsistencyRestorePreservesOriginalReplicasAndLegacyRules(t *testing.T) {
	record := completedConsistencyFixture()
	manifest, err := BuildRestoreManifest(RestoreBuildInput{TaskID: "test", Consistency: &record, Command: protocol.RestoreCommand{VeleroBackupName: "backup", SourceNamespace: "app", TargetNamespace: "restored-app"}})
	if err != nil {
		t.Fatal(err)
	}
	config := BuildRestoreResourceModifierConfigMap(manifest).Data["resource-modifiers.yaml"]
	var parsed map[string]any
	if err := yaml.UnmarshalStrict([]byte(config), &parsed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(config, "groupResource: persistentvolumeclaims") || !strings.Contains(config, "groupResource: deployments.apps") || !strings.Contains(config, `"replicas":2`) || strings.Contains(config, "path: /metadata/uid") {
		t.Fatalf("original-replica modifier missing or incorrectly depends on cleared UID: %s", config)
	}
	if path := os.Getenv("HYPERCDR_CONSISTENCY_MODIFIER_FIXTURE"); path != "" {
		if err := os.WriteFile(path, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := BuildRestoreManifest(RestoreBuildInput{TaskID: "test", Command: protocol.RestoreCommand{VeleroBackupName: "backup", SourceNamespace: "app"}})
	if err != nil || legacy.ConsistencyRules != "" {
		t.Fatalf("legacy restore changed: %v", err)
	}
}

func TestConsistencyRestoreRejectsUnverifiedOrPartialGroup(t *testing.T) {
	for _, scenario := range []string{"failed", "not-resumed", "locks-not-released", "snapshot-only", "missing-upload", "wrong-volume", "duplicate-volume", "duplicate-workload", "negative-replicas", "wrong-namespace", "missing-generation", "unsafe-run-id", "missing-proof", "changed-proof"} {
		t.Run(scenario, func(t *testing.T) {
			record := completedConsistencyFixture()
			switch scenario {
			case "locks-not-released":
				record.LocksReleased = false
			case "missing-proof":
				record.QuiescenceProof = nil
			case "changed-proof":
				record.QuiescenceProof.EvidenceSHA256 = "changed"
			case "failed":
				record.Phase = consistency.Failed
			case "not-resumed":
				record.Resumed = false
			case "snapshot-only":
				record.Snapshots[0].Source = "csi"
			case "missing-upload":
				record.Snapshots[0].DataUploadUID = ""
			case "wrong-volume":
				record.Snapshots[0].VolumeUID = "other"
			case "duplicate-volume":
				record.Volumes = append(record.Volumes, record.Volumes[0])
				record.Snapshots = append(record.Snapshots, record.Snapshots[0])
			case "duplicate-workload":
				record.Workloads = append(record.Workloads, record.Workloads[0])
			case "negative-replicas":
				record.Workloads[0].Replicas = -1
			case "wrong-namespace":
				record.Volumes[0].Namespace = "other"
			case "missing-generation":
				record.Workloads[0].Generation = 0
			case "unsafe-run-id":
				record.ID = `run"injected`
			}
			if _, err := BuildConsistencyRestoreRules(record, []string{"app"}); err == nil {
				t.Fatal("unsafe restore record accepted")
			}
		})
	}
}

func TestConsistencyRestoreCommandEvidenceBuildsRulesAndRejectsConflicts(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-backup", "missing-command", "wrong-task", "partial-group", "filtered", "two-sources", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			record := completedConsistencyFixture()
			group := &protocol.ConsistencyGroup{ID: record.GroupID, Strategy: record.Strategy, ApplicationProfile: record.ApplicationProfile, MaxPauseSeconds: 120,
				Workloads: []protocol.ConsistencyWorkload{{Namespace: "app", Name: "database", Kind: "Deployment", UID: "db-uid"}},
				Volumes:   []protocol.ConsistencyVolume{{Namespace: "app", Name: "data", UID: "pvc-uid", SnapshotClass: "class"}}}
			evidence := &protocol.ConsistencyRestoreEvidence{TaskID: record.ID, CommandID: "backup-command", Backup: protocol.BackupCommand{VeleroBackupName: "backup", SourceNamespace: "app", DataProtectionMode: "application-consistent", ConsistencyGroup: group}, Record: record}
			input := RestoreBuildInput{TaskID: "restore", Command: protocol.RestoreCommand{VeleroBackupName: "backup", SourceNamespace: "app", ConsistencyEvidence: evidence}}
			switch scenario {
			case "wrong-backup":
				input.Command.VeleroBackupName = "other"
			case "missing-command":
				evidence.CommandID = ""
			case "wrong-task":
				evidence.TaskID = "other"
			case "partial-group":
				input.Command.SourceNamespace = "other"
			case "filtered":
				input.Command.ExcludedResources = []string{"deployments"}
			case "two-sources":
				input.Consistency = &record
			case "locked":
				evidence.Record.LocksReleased = false
			}
			manifest, err := BuildRestoreManifest(input)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected evidence acceptance: %v", err)
			}
			if err == nil && !strings.Contains(manifest.ConsistencyRules, `"replicas":2`) {
				t.Fatal("wire evidence did not restore captured replicas")
			}
			if scenario == "valid" {
				input.TaskID = "restore-another-task-with-same-prefix"
				other, err := BuildRestoreManifest(input)
				if err != nil || other.Metadata.Name == manifest.Metadata.Name || other.Spec.ResourceModifier.Name == manifest.Spec.ResourceModifier.Name {
					t.Fatalf("strict task prefix collision reused restore resources: %v", err)
				}
			}
		})
	}
}
