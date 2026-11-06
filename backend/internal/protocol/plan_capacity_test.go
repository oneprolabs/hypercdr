package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCapacityUsesExistingTaskContracts(t *testing.T) {
	command := Message[TaskDispatchPayload]{Version: Version, MessageKind: MessageKindRequest, Type: MessagePlatformTaskDispatch, ClusterID: "cluster", Payload: TaskDispatchPayload{TaskID: "task", CommandID: "command", Type: "plan-capacity", PlanCapacity: &PlanCapacityCommand{Namespace: "agent", Plans: []PlanCapacityTarget{{PlanID: "plan", StorageLocation: "store", Namespaces: []string{"demo"}}}}}}
	raw, err := EncodeMessage(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMessage(raw, PlatformToAgent, "cluster"); err != nil {
		t.Fatal(err)
	}
	var decoded Message[TaskDispatchPayload]
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Payload.PlanCapacity.Plans[0].PlanID != "plan" {
		t.Fatalf("command lost: %s %v", raw, err)
	}
	result := Message[TaskCompletedPayload]{Version: Version, MessageKind: MessageKindEvent, Type: MessageAgentTaskCompleted, ClusterID: "cluster", Payload: TaskCompletedPayload{TaskID: "task", CommandID: "command", AckRequired: true, PlanCapacity: &PlanCapacityReport{Plans: []PlanCapacityResult{{PlanID: "plan", Status: "succeeded", MetadataBytes: 1, KopiaBytes: 2, TotalBytes: 3, CollectedAt: time.Now().UTC()}}}}}
	raw, err = EncodeMessage(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMessage(raw, AgentToPlatform, "cluster"); err != nil {
		t.Fatal(err)
	}
	var report Message[TaskCompletedPayload]
	if err := json.Unmarshal(raw, &report); err != nil || report.Payload.PlanCapacity.Plans[0].TotalBytes != 3 {
		t.Fatalf("report lost: %s %v", raw, err)
	}
}
