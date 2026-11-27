package protocol

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestConsistencyEvidencePreservesExistingJournalWireShape(t *testing.T) {
	fixture := `{"applicationProfile":"paired-ledger-v1","startedAt":"2026-11-25T03:55:36Z","quiescenceProof":{"runId":"run","groupId":"group","profile":"paired-ledger-v1","observedAt":"2026-11-25T03:55:54Z","workloadUids":["writer-uid"],"volumeUids":["pvc-uid"],"evidence":{"event":"validated-stop","sequence":19179},"evidenceSha256":"digest"},"locksReleased":true,"groupId":"group","strategy":"offline-v1","id":"run","phase":"completed","deadline":"2026-11-25T03:56:56Z","volumes":[{"namespace":"app","name":"data","uid":"pvc-uid"}],"snapshots":[{"source":"kopia","dataUploadUid":"upload-uid","volumeUid":"pvc-uid","handle":"snapshot-id","secured":true}],"resumed":true,"workloads":[{"resourceVersion":"15","generation":3,"namespace":"app","name":"writer","kind":"Deployment","uid":"writer-uid","replicas":1}]}`
	var record ConsistencyRecord
	if err := json.Unmarshal([]byte(fixture), &record); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var original, roundTrip map[string]any
	if err := json.Unmarshal([]byte(fixture), &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, roundTrip) {
		t.Fatalf("journal fields changed during protocol consolidation: %s", encoded)
	}
	if record.Phase != "completed" || record.QuiescenceProof.RunID != record.ID || record.Snapshots[0].DataUploadUID != "upload-uid" {
		t.Fatal("typed evidence lost identity fields")
	}
}

func TestSharedQuiescenceProofVerifierRejectsUnboundEvidence(t *testing.T) {
	for _, scenario := range []string{"valid", "pretty-json", "missing", "wrong-run", "wrong-group", "wrong-profile", "future", "outside-pause", "digest", "wrong-workload", "wrong-volume", "duplicate-identities", "non-object", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now().UTC()
			evidence := []byte(`{"sequence":42}`)
			record := ConsistencyRecord{ID: "run", GroupID: "group", ApplicationProfile: "qualified", StartedAt: now.Add(-time.Minute), Deadline: now.Add(time.Minute), Workloads: []ConsistencyWorkloadState{{UID: "writer"}}, Volumes: []ConsistencyVolumeIdentity{{UID: "volume"}}}
			proof := &ConsistencyQuiescenceProof{RunID: "run", GroupID: "group", Profile: "qualified", ObservedAt: now, WorkloadUIDs: []string{"writer"}, VolumeUIDs: []string{"volume"}, Evidence: evidence, EvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(evidence))}
			record.QuiescenceProof = proof
			switch scenario {
			case "pretty-json":
				proof.Evidence = []byte("{\n  \"sequence\": 42\n}")
			case "missing":
				record.QuiescenceProof = nil
			case "wrong-run":
				proof.RunID = "other"
			case "wrong-group":
				proof.GroupID = "other"
			case "wrong-profile":
				proof.Profile = "other"
			case "future":
				proof.ObservedAt = now.Add(time.Second)
			case "outside-pause":
				proof.ObservedAt = record.StartedAt.Add(-time.Second)
			case "digest":
				proof.EvidenceSHA256 = "changed"
			case "wrong-workload":
				proof.WorkloadUIDs[0] = "other"
			case "wrong-volume":
				proof.VolumeUIDs[0] = "other"
			case "duplicate-identities":
				proof.VolumeUIDs = []string{"volume", "volume"}
				record.Volumes = append(record.Volumes, record.Volumes[0])
			case "non-object":
				proof.Evidence = []byte(`[]`)
			case "oversized":
				proof.Evidence = make([]byte, 16*1024+1)
			}
			if err := VerifyConsistencyQuiescenceProof(record); (err == nil) != (scenario == "valid" || scenario == "pretty-json") {
				t.Fatalf("scenario %s: %v", scenario, err)
			}
		})
	}
}
