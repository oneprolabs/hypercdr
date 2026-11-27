package consistency

import "hypercdr-platform/agent/comm-agent/pkg/protocol"

type Phase = protocol.ConsistencyPhase

const (
	Prepared  Phase = "prepared"
	Quiescing Phase = "quiescing"
	Capturing Phase = "capturing"
	Resuming  Phase = "resuming"
	Uploading Phase = "uploading"
	Completed Phase = "completed"
	Failed    Phase = "failed"
)

type Volume = protocol.ConsistencyVolumeIdentity
type Snapshot = protocol.ConsistencySnapshot
type Record = protocol.ConsistencyRecord
type WorkloadState = protocol.ConsistencyWorkloadState

func VerifyDurableSnapshots(record Record) error {
	return protocol.VerifyConsistencyDurableSnapshots(record)
}
