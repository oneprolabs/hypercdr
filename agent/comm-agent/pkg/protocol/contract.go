package protocol

import "reflect"

type Direction string

const (
	AgentToPlatform Direction = "agent-to-platform"
	PlatformToAgent Direction = "platform-to-agent"
)

type Contract struct {
	Type           string    `json:"type"`
	Direction      Direction `json:"direction"`
	Family         string    `json:"family"`
	PayloadType    string    `json:"payloadType"`
	TerminalResult bool      `json:"terminalResult"`
	AllowedKinds   []string  `json:"allowedKinds"`
	payload        reflect.Type
}

func contract(messageType string, direction Direction, family string, payload any, terminal bool, kinds ...string) Contract {
	payloadType := reflect.TypeOf(payload)
	return Contract{Type: messageType, Direction: direction, Family: family, PayloadType: payloadType.Name(), TerminalResult: terminal, AllowedKinds: kinds, payload: payloadType}
}

func Contracts() []Contract {
	return []Contract{
		contract(MessageAgentRegister, AgentToPlatform, "control", RegisterPayload{}, false, MessageKindRequest),
		contract(MessageAgentHeartbeat, AgentToPlatform, "control", HeartbeatPayload{}, false, MessageKindEvent),
		contract(MessageAgentInventoryReport, AgentToPlatform, "observation", InventoryReportPayload{}, false, MessageKindEvent, MessageKindResponse),
		contract(MessageAgentTaskAccepted, AgentToPlatform, "execution", TaskAcceptedPayload{}, false, MessageKindResponse),
		contract(MessageAgentTaskProgress, AgentToPlatform, "telemetry", TaskProgressPayload{}, false, MessageKindEvent),
		contract(MessageAgentTaskCompleted, AgentToPlatform, "execution", TaskCompletedPayload{}, true, MessageKindEvent),
		contract(MessageAgentTaskFailed, AgentToPlatform, "execution", TaskFailedPayload{}, true, MessageKindEvent),
		contract(MessageAgentVeleroEvent, AgentToPlatform, "observation", VeleroEventPayload{}, false, MessageKindEvent),
		contract(MessageAgentLogReport, AgentToPlatform, "query", LogReportPayload{}, false, MessageKindResponse),
		contract(MessageAgentBackupContentReport, AgentToPlatform, "query", BackupContentReportPayload{}, false, MessageKindResponse),
		contract(MessageAgentMessageError, AgentToPlatform, "control", MessageErrorPayload{}, false, MessageKindResponse),
		contract(MessagePlatformRegisterAccepted, PlatformToAgent, "control", RegisterAcceptedPayload{}, false, MessageKindResponse),
		contract(MessagePlatformRegisterRejected, PlatformToAgent, "control", RegisterRejectedPayload{}, false, MessageKindResponse),
		contract(MessagePlatformTaskDispatch, PlatformToAgent, "command", TaskDispatchPayload{}, false, MessageKindRequest),
		contract(MessagePlatformTaskCancel, PlatformToAgent, "command", TaskCancelPayload{}, false, MessageKindRequest),
		contract(MessagePlatformInventoryRequest, PlatformToAgent, "query", InventoryRequestPayload{}, false, MessageKindRequest),
		contract(MessagePlatformLogRequest, PlatformToAgent, "query", LogRequestPayload{}, false, MessageKindRequest),
		contract(MessagePlatformBackupContentRequest, PlatformToAgent, "query", BackupContentRequestPayload{}, false, MessageKindRequest),
		contract(MessagePlatformEventAck, PlatformToAgent, "control", EventAckPayload{}, false, MessageKindResponse),
		contract(MessagePlatformEventError, PlatformToAgent, "control", EventErrorPayload{}, false, MessageKindResponse),
	}
}

func LookupContract(messageType string) (Contract, bool) {
	for _, definition := range Contracts() {
		if definition.Type == messageType {
			return definition, true
		}
	}
	return Contract{}, false
}

func ValidateV1Message(data []byte, direction Direction, authenticatedCluster string) error {
	_, err := DecodeMessage(data, direction, authenticatedCluster)
	return err
}

func newPayload(definition Contract) any {
	return reflect.New(definition.payload).Interface()
}
