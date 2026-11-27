package consistency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type QuiescenceProof = protocol.ConsistencyQuiescenceProof

func NewQuiescenceProof(record Record, observedAt time.Time, evidence []byte) (*QuiescenceProof, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, evidence); err != nil {
		return nil, err
	}
	proof := &QuiescenceProof{RunID: record.ID, GroupID: record.GroupID, Profile: record.ApplicationProfile, ObservedAt: observedAt.UTC(), Evidence: append(json.RawMessage(nil), compact.Bytes()...)}
	for _, workload := range record.Workloads {
		proof.WorkloadUIDs = append(proof.WorkloadUIDs, workload.UID)
	}
	for _, volume := range record.Volumes {
		proof.VolumeUIDs = append(proof.VolumeUIDs, volume.UID)
	}
	digest := sha256.Sum256(proof.Evidence)
	proof.EvidenceSHA256 = hex.EncodeToString(digest[:])
	record.QuiescenceProof = proof
	if err := VerifyQuiescenceProof(record); err != nil {
		return nil, err
	}
	return proof, nil
}

func VerifyQuiescenceProof(record Record) error {
	return protocol.VerifyConsistencyQuiescenceProof(record)
}
