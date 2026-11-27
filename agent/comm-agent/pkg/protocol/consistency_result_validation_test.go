package protocol

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func TestSharedConsistencyCompletionRequiresExactTaskAndDurableInventory(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-task", "wrong-group", "wrong-volume-name", "wrong-workload-name", "not-completed", "not-resumed", "locked", "failure", "missing-proof", "csi-only", "no-upload-id", "unsecured", "missing-snapshot", "duplicate-upload"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now().UTC()
			group := &ConsistencyGroup{ID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified", MaxPauseSeconds: 120,
				Workloads: []ConsistencyWorkload{{Namespace: "app", Name: "writer", Kind: "Deployment", UID: "writer-uid"}}, Volumes: []ConsistencyVolume{{Namespace: "app", Name: "data", UID: "pvc-uid", SnapshotClass: "class"}}}
			task := TaskDispatchPayload{TaskID: "run", Type: "backup", Backup: &BackupCommand{SourceNamespace: "app", DataProtectionMode: "application-consistent", ConsistencyGroup: group}}
			evidence := []byte(`{"sequence":42}`)
			record := ConsistencyRecord{ID: "run", GroupID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified", StartedAt: now.Add(-time.Minute), Deadline: now.Add(time.Minute), Phase: "completed", Resumed: true, LocksReleased: true,
				Workloads: []ConsistencyWorkloadState{{Namespace: "app", Name: "writer", Kind: "Deployment", UID: "writer-uid"}}, Volumes: []ConsistencyVolumeIdentity{{Namespace: "app", Name: "data", UID: "pvc-uid"}},
				Snapshots:       []ConsistencySnapshot{{Source: "kopia", DataUploadUID: "upload-uid", VolumeUID: "pvc-uid", Handle: "durable", Secured: true}},
				QuiescenceProof: &ConsistencyQuiescenceProof{RunID: "run", GroupID: "group", Profile: "qualified", ObservedAt: now, WorkloadUIDs: []string{"writer-uid"}, VolumeUIDs: []string{"pvc-uid"}, Evidence: evidence, EvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(evidence))}}
			switch scenario {
			case "wrong-task":
				record.ID = "other"
			case "wrong-group":
				record.GroupID = "other"
			case "wrong-volume-name":
				record.Volumes[0].Name = "other"
			case "wrong-workload-name":
				record.Workloads[0].Name = "other"
			case "not-completed":
				record.Phase = "uploading"
			case "not-resumed":
				record.Resumed = false
			case "locked":
				record.LocksReleased = false
			case "failure":
				record.Failure = "failure"
			case "missing-proof":
				record.QuiescenceProof = nil
			case "csi-only":
				record.Snapshots[0].Source = "csi"
			case "no-upload-id":
				record.Snapshots[0].DataUploadUID = ""
			case "unsecured":
				record.Snapshots[0].Secured = false
			case "missing-snapshot":
				record.Snapshots = nil
			case "duplicate-upload":
				group.Volumes = append(group.Volumes, ConsistencyVolume{Namespace: "app", Name: "second", UID: "second-pvc", SnapshotClass: "class"})
				record.Volumes = append(record.Volumes, ConsistencyVolumeIdentity{Namespace: "app", Name: "second", UID: "second-pvc"})
				record.QuiescenceProof.VolumeUIDs = append(record.QuiescenceProof.VolumeUIDs, "second-pvc")
				record.Snapshots = append(record.Snapshots, ConsistencySnapshot{Source: "kopia", DataUploadUID: "upload-uid", VolumeUID: "second-pvc", Handle: "second-durable", Secured: true})
			}
			if err := ValidateConsistencyTaskResult(task, record); (err == nil) != (scenario == "valid") {
				t.Fatalf("scenario %s: %v", scenario, err)
			}
			if scenario == "valid" {
				for _, restoreScenario := range []string{"valid", "legacy", "wrong-backup", "missing-command", "wrong-task", "partial-group", "filtered", "locked"} {
					t.Run("restore-"+restoreScenario, func(t *testing.T) {
						backup := *task.Backup
						backup.VeleroBackupName = "backup"
						restoreEvidence := &ConsistencyRestoreEvidence{TaskID: task.TaskID, CommandID: "command", Backup: backup, Record: record}
						command := RestoreCommand{VeleroBackupName: "backup", SourceNamespace: "app", ConsistencyEvidence: restoreEvidence}
						switch restoreScenario {
						case "legacy":
							command.ConsistencyEvidence = nil
						case "wrong-backup":
							command.VeleroBackupName = "other"
						case "missing-command":
							restoreEvidence.CommandID = ""
						case "wrong-task":
							restoreEvidence.TaskID = "other"
						case "partial-group":
							command.SourceNamespace = "other"
						case "filtered":
							command.ExcludedResources = []string{"deployments"}
						case "locked":
							restoreEvidence.Record.LocksReleased = false
						}
						if err := ValidateConsistencyRestoreEvidence(command); (err == nil) != (restoreScenario == "valid" || restoreScenario == "legacy") {
							t.Fatalf("unexpected restore acceptance: %v", err)
						}
					})
				}
				record.Deadline = record.StartedAt.Add(80 * time.Second)
				record.ResumeRequestedAt = now
				record.ResumeReadyDeadline = record.StartedAt.Add(120 * time.Second)
				if err := ValidateConsistencyTaskResult(task, record); err != nil {
					t.Fatalf("bounded completion rejected: %v", err)
				}
				task.Backup.ConsistencyGroup.MaxPauseSeconds = 90
				if err := ValidateConsistencyTaskResult(task, record); err == nil {
					t.Fatal("completion ignored the dispatched pause budget")
				}
			}
		})
	}
}

func TestConsistencyResumeWindowEvidenceRejectsInvalidTiming(t *testing.T) {
	for _, scenario := range []string{"legacy", "valid", "missing-request", "missing-deadline", "late-request", "before-start", "over-budget", "short-plan-budget", "proof-after-request"} {
		t.Run(scenario, func(t *testing.T) {
			started := time.Now().UTC().Add(-2 * time.Minute)
			record := ConsistencyRecord{StartedAt: started, Deadline: started.Add(80 * time.Second), ResumeRequestedAt: started.Add(70 * time.Second), ResumeReadyDeadline: started.Add(120 * time.Second), QuiescenceProof: &ConsistencyQuiescenceProof{ObservedAt: started.Add(20 * time.Second)}}
			budget := 120
			switch scenario {
			case "legacy":
				record.ResumeRequestedAt, record.ResumeReadyDeadline = time.Time{}, time.Time{}
			case "missing-request":
				record.ResumeRequestedAt = time.Time{}
			case "missing-deadline":
				record.ResumeReadyDeadline = time.Time{}
			case "late-request":
				record.ResumeRequestedAt = record.Deadline
			case "before-start":
				record.ResumeRequestedAt = started.Add(-time.Second)
			case "over-budget":
				record.ResumeReadyDeadline = started.Add(121 * time.Second)
			case "short-plan-budget":
				budget = 90
			case "proof-after-request":
				record.QuiescenceProof.ObservedAt = record.ResumeRequestedAt.Add(time.Second)
			}
			if err := VerifyConsistencyResumeWindow(record, budget); (err == nil) != (scenario == "legacy" || scenario == "valid") {
				t.Fatalf("scenario=%s err=%v", scenario, err)
			}
		})
	}
}
