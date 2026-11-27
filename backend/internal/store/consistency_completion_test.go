package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"hypercdr-platform/platform/backend/internal/protocol"
)

func TestHistoricalApplicationConsistencyPublicationPersistsAndBindsEvidence(t *testing.T) {
	repo := newTestStore(t)
	cluster, app := seedPlanApplication(t, repo)
	group := &protocol.ConsistencyGroup{ID: "group", Strategy: "offline-v1", ApplicationProfile: "qualified", MaxPauseSeconds: 120,
		Workloads: []protocol.ConsistencyWorkload{{Namespace: app.Namespace, Name: "writer", Kind: "Deployment", UID: "writer-uid"}},
		Volumes:   []protocol.ConsistencyVolume{{Namespace: app.Namespace, Name: "data", UID: "pvc-uid", SnapshotClass: "class"}}}
	plan, err := repo.CreateProtectionPlan(ProtectionPlanInput{TenantID: DefaultTenantID, SourceClusterID: cluster.ID, AppID: app.ID, DataProtection: DataProtectionSettings{Mode: "snapshot-crash-consistent"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := repo.CreateTask(TaskInput{ClusterID: cluster.ID, ProtectionPlanID: plan.ID, CommandID: newID(), Type: "backup", Payload: map[string]any{"sourceNamespace": app.Namespace}})
	if err != nil {
		t.Fatal(err)
	}
	plan.DataProtection = DataProtectionSettings{Mode: "application-consistent", Group: group}
	settingsJSON, err := json.Marshal(plan.DataProtection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`update protection_plans set data_protection=$2::jsonb where id=$1`, plan.ID, string(settingsJSON)); err != nil {
		t.Fatal(err)
	}
	task.Payload["dataProtectionMode"] = "application-consistent"
	task.Payload["consistencyGroup"] = group
	payloadJSON, err := json.Marshal(task.Payload)
	if err != nil {
		t.Fatal(err)
	}
	originalTaskID := task.ID
	task.ID, task.CommandID = newID(), newID()
	if _, err := repo.db.Exec(`insert into tasks select (jsonb_populate_record(null::tasks, to_jsonb(original) || jsonb_build_object('id',$2::text,'command_id',$3::text,'payload',$4::jsonb))).* from tasks original where id=$1`, originalTaskID, task.ID, task.CommandID, string(payloadJSON)); err != nil {
		t.Fatal(err)
	}
	input := completeResultInput(plan, task, "consistent-backup")
	if _, err := repo.PublishBackupResult(input); !errors.Is(err, ErrBackupResultConflict) {
		t.Fatalf("unproven direct publication accepted: %v", err)
	}
	if _, err := repo.CreateRestorePoint(input); !errors.Is(err, ErrBackupResultConflict) {
		t.Fatalf("unproven restore point creation accepted: %v", err)
	}
	now := time.Now().UTC()
	evidence := []byte(`{"sequence":42}`)
	record := protocol.ConsistencyRecord{ID: task.ID, GroupID: group.ID, Strategy: group.Strategy, ApplicationProfile: group.ApplicationProfile,
		StartedAt: now.Add(-time.Minute), Deadline: now.Add(time.Minute), Phase: "completed", Resumed: true, LocksReleased: true,
		Workloads: []protocol.ConsistencyWorkloadState{{Namespace: app.Namespace, Name: "writer", Kind: "Deployment", UID: "writer-uid"}},
		Volumes:   []protocol.ConsistencyVolumeIdentity{{Namespace: app.Namespace, Name: "data", UID: "pvc-uid"}},
		Snapshots: []protocol.ConsistencySnapshot{{Source: "kopia", DataUploadUID: "upload", VolumeUID: "pvc-uid", Handle: "durable", Secured: true}},
		QuiescenceProof: &protocol.ConsistencyQuiescenceProof{RunID: task.ID, GroupID: group.ID, Profile: group.ApplicationProfile, ObservedAt: now,
			WorkloadUIDs: []string{"writer-uid"}, VolumeUIDs: []string{"pvc-uid"}, Evidence: evidence, EvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(evidence))}}
	result := map[string]any{"requestedConsistency": "application-consistent", "achievedConsistency": "application-consistent", "consistency": record}
	input.Metadata = map[string]any{"velero": result}
	point, err := repo.PublishBackupResult(input)
	if err != nil {
		t.Fatal(err)
	}
	loaded, found, err := repo.GetRestorePoint(point.ID)
	if err != nil || !found || !backupConsistencyEqual(loaded.Metadata, input.Metadata) {
		t.Fatalf("evidence not persisted: %v, %v", found, err)
	}
	if _, err := repo.PublishBackupResult(input); err != nil {
		t.Fatalf("identical replay rejected: %v", err)
	}
	record.Snapshots[0].Handle = "different-durable-snapshot"
	result["consistency"] = record
	if _, err := repo.PublishBackupResult(input); !errors.Is(err, ErrBackupResultConflict) {
		t.Fatalf("conflicting evidence replay accepted: %v", err)
	}
	if _, _, err := repo.UpdateTaskStatus(TaskStatusInput{TaskID: task.ID, Payload: map[string]any{"backupResult": nil}}); err == nil {
		t.Fatal("accepted result removal allowed")
	}
	for _, query := range []string{
		`update restore_points set metadata='{}'::jsonb where id=$1`,
		`update restore_points set metadata=jsonb_set(metadata,'{velero,achievedConsistency}','"live-filesystem"') where id=$1`,
	} {
		if _, err := repo.db.Exec(query, point.ID); err == nil {
			t.Fatal("persisted evidence tampering allowed")
		}
	}
	finished, _, err := repo.GetTask(task.ID)
	if err != nil || finished.Status != "succeeded" {
		t.Fatalf("publication did not finalize task: %v", err)
	}
	encoded, err := json.Marshal(finished.Payload["backupResult"])
	if err != nil {
		t.Fatal(err)
	}
	var accepted RestorePointInput
	if err := json.Unmarshal(encoded, &accepted); err != nil || !backupConsistencyEqual(accepted.Metadata, loaded.Metadata) {
		t.Fatalf("task result is not bound to persisted evidence: %v", err)
	}
	for _, scenario := range []string{"valid", "wrong-tenant", "wrong-backup", "wrong-task", "unpublished", "deleted", "missing-command", "different-cluster"} {
		t.Run("restore-evidence-"+scenario, func(t *testing.T) {
			candidate, source := loaded, finished
			switch scenario {
			case "wrong-tenant":
				candidate.TenantID = "other"
			case "wrong-backup":
				candidate.VeleroBackupName = "other"
			case "wrong-task":
				candidate.BackupTaskID = "other"
			case "unpublished":
				source.Status = "running"
			case "deleted":
				candidate.Status = "deleted"
			case "missing-command":
				source.CommandID = ""
			case "different-cluster":
				candidate.SourceClusterID = "other"
			}
			evidence, err := BuildConsistencyRestoreEvidence(candidate, source)
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected restore source acceptance: %v", err)
			}
			if err == nil {
				command := protocol.RestoreCommand{VeleroBackupName: loaded.VeleroBackupName, SourceNamespace: app.Namespace, ConsistencyEvidence: evidence}
				if evidence == nil || protocol.ValidateConsistencyRestoreEvidence(command) != nil {
					t.Fatal("persisted evidence did not produce a valid restore command")
				}
			}
		})
	}
}
