package consistency

import (
	"encoding/json"
	"testing"
	"time"
)

func proofRecord(t *testing.T) Record {
	t.Helper()
	record := Record{ID: "run", GroupID: "group", Strategy: "offline-v1", ApplicationProfile: "paired-ledger-v1", StartedAt: time.Now().UTC().Add(-time.Minute), Deadline: time.Now().UTC().Add(time.Minute),
		Workloads: []WorkloadState{{Namespace: "app", Name: "writer", UID: "writer-uid"}},
		Volumes:   []Volume{{Namespace: "app", Name: "ledger", UID: "ledger-uid"}, {Namespace: "app", Name: "commits", UID: "commits-uid"}}}
	proof, err := NewQuiescenceProof(record, time.Now(), []byte("{\n  \"sequence\": 42, \"event\": \"validated-stop\"\n}"))
	if err != nil {
		t.Fatal(err)
	}
	record.QuiescenceProof = proof
	return record
}

func TestQuiescenceProofSurvivesPersistedJSONRoundTrip(t *testing.T) {
	record := proofRecord(t)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var reloaded Record
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	if err := VerifyQuiescenceProof(reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.QuiescenceProof.ObservedAt.Location() != time.UTC {
		t.Fatal("proof observation timestamp is not UTC")
	}
}

func TestQuiescenceProofRejectsMissingStaleOrChangedEvidence(t *testing.T) {
	for _, scenario := range []string{"missing", "run", "group", "profile", "missing-start", "stale", "late", "future", "digest", "changed-report", "empty-report", "array-report", "oversized", "missing-volume", "duplicate-volume", "unknown-workload", "duplicate-inventory"} {
		t.Run(scenario, func(t *testing.T) {
			record := proofRecord(t)
			proof := record.QuiescenceProof
			switch scenario {
			case "missing":
				record.QuiescenceProof = nil
			case "run":
				proof.RunID = "other"
			case "group":
				proof.GroupID = "other"
			case "profile":
				proof.Profile = "other"
			case "missing-start":
				record.StartedAt = time.Time{}
			case "stale":
				proof.ObservedAt = record.StartedAt.Add(-time.Second)
			case "late":
				proof.ObservedAt = record.Deadline.Add(time.Second)
			case "future":
				proof.ObservedAt = time.Now().Add(30 * time.Second)
			case "digest":
				proof.EvidenceSHA256 = "incorrect"
			case "changed-report":
				proof.Evidence = json.RawMessage(`{"sequence":43}`)
			case "empty-report":
				proof.Evidence = json.RawMessage(`{}`)
			case "array-report":
				proof.Evidence = json.RawMessage(`[]`)
			case "oversized":
				proof.Evidence = make(json.RawMessage, 16*1024+1)
			case "missing-volume":
				proof.VolumeUIDs = proof.VolumeUIDs[:1]
			case "duplicate-volume":
				proof.VolumeUIDs[1] = proof.VolumeUIDs[0]
			case "unknown-workload":
				proof.WorkloadUIDs[0] = "other"
			case "duplicate-inventory":
				record.Volumes[1].UID = record.Volumes[0].UID
			}
			if err := VerifyQuiescenceProof(record); err == nil {
				t.Fatal("unsafe quiescence proof accepted")
			}
		})
	}
}
