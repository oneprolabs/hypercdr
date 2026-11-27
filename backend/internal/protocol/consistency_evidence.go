package protocol

import (
	"encoding/json"
	"time"
)

type ConsistencyPhase string

type ConsistencyRestoreEvidence struct {
	TaskID    string            `json:"taskId"`
	CommandID string            `json:"commandId"`
	Backup    BackupCommand     `json:"backup"`
	Record    ConsistencyRecord `json:"record"`
}

type ConsistencyVolumeIdentity struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

type ConsistencySnapshot struct {
	Source        string `json:"source,omitempty"`
	DataUploadUID string `json:"dataUploadUid,omitempty"`
	VolumeUID     string `json:"volumeUid"`
	Handle        string `json:"handle"`
	Secured       bool   `json:"secured"`
}

type ConsistencyWorkloadState struct {
	ResourceVersion string `json:"resourceVersion,omitempty"`
	Generation      int64  `json:"generation,omitempty"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	UID             string `json:"uid"`
	Replicas        int64  `json:"replicas"`
}

type ConsistencyQuiescenceProof struct {
	RunID          string          `json:"runId"`
	GroupID        string          `json:"groupId"`
	Profile        string          `json:"profile"`
	ObservedAt     time.Time       `json:"observedAt"`
	WorkloadUIDs   []string        `json:"workloadUids"`
	VolumeUIDs     []string        `json:"volumeUids"`
	Evidence       json.RawMessage `json:"evidence"`
	EvidenceSHA256 string          `json:"evidenceSha256"`
}

type ConsistencyRecord struct {
	ApplicationProfile  string                      `json:"applicationProfile,omitempty"`
	StartedAt           time.Time                   `json:"startedAt"`
	QuiescenceProof     *ConsistencyQuiescenceProof `json:"quiescenceProof,omitempty"`
	LocksReleased       bool                        `json:"locksReleased,omitempty"`
	GroupID             string                      `json:"groupId,omitempty"`
	Strategy            string                      `json:"strategy,omitempty"`
	ID                  string                      `json:"id"`
	Phase               ConsistencyPhase            `json:"phase"`
	Deadline            time.Time                   `json:"deadline"`
	ResumeRequestedAt   time.Time                   `json:"resumeRequestedAt,omitzero"`
	ResumeReadyDeadline time.Time                   `json:"resumeReadyDeadline,omitzero"`
	Volumes             []ConsistencyVolumeIdentity `json:"volumes"`
	Snapshots           []ConsistencySnapshot       `json:"snapshots,omitempty"`
	Resumed             bool                        `json:"resumed"`
	Failure             string                      `json:"failure,omitempty"`
	Workloads           []ConsistencyWorkloadState  `json:"workloads,omitempty"`
}
