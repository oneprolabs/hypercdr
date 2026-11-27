package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func VerifyConsistencyQuiescenceProof(record ConsistencyRecord) error {
	proof := record.QuiescenceProof
	if proof == nil || record.ID == "" || record.GroupID == "" || record.ApplicationProfile == "" || record.StartedAt.IsZero() || record.Deadline.IsZero() || proof.RunID != record.ID || proof.GroupID != record.GroupID || proof.Profile != record.ApplicationProfile || proof.ObservedAt.Before(record.StartedAt) || proof.ObservedAt.After(record.Deadline) || proof.ObservedAt.After(time.Now()) {
		return fmt.Errorf("missing, stale or mismatched application quiescence proof")
	}
	if len(proof.Evidence) == 0 || len(proof.Evidence) > 16*1024 {
		return fmt.Errorf("quiescence evidence must contain at most 16 KiB of nonsecret application verification data")
	}
	var evidence map[string]json.RawMessage
	if err := json.Unmarshal(proof.Evidence, &evidence); err != nil || len(evidence) == 0 {
		return fmt.Errorf("quiescence evidence must be a nonempty JSON object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, proof.Evidence); err != nil {
		return err
	}
	digest := sha256.Sum256(compact.Bytes())
	if proof.EvidenceSHA256 != hex.EncodeToString(digest[:]) {
		return fmt.Errorf("quiescence evidence digest mismatch")
	}
	var workloadUIDs, volumeUIDs []string
	for _, workload := range record.Workloads {
		workloadUIDs = append(workloadUIDs, workload.UID)
	}
	for _, volume := range record.Volumes {
		volumeUIDs = append(volumeUIDs, volume.UID)
	}
	if !sameConsistencyProofIdentities(proof.WorkloadUIDs, workloadUIDs) || !sameConsistencyProofIdentities(proof.VolumeUIDs, volumeUIDs) {
		return fmt.Errorf("quiescence proof does not cover the exact workload and volume group")
	}
	return nil
}

func sameConsistencyProofIdentities(actual, expected []string) bool {
	if len(actual) == 0 || len(actual) != len(expected) {
		return false
	}
	seen := map[string]bool{}
	for _, identity := range actual {
		if identity == "" || seen[identity] {
			return false
		}
		seen[identity] = true
	}
	for _, identity := range expected {
		if !seen[identity] {
			return false
		}
		delete(seen, identity)
	}
	return len(seen) == 0
}
