package wsclient

import (
	"testing"

	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

func TestEventAcknowledgementIsScopedToExactResult(t *testing.T) {
	for _, test := range []struct {
		name       string
		eventType  string
		ackType    string
		ackTask    string
		ackCommand string
		persisted  bool
		removed    bool
		terminal   bool
	}{
		{"observation", protocol.MessageAgentVeleroEvent, protocol.MessageAgentVeleroEvent, "task", "command", true, true, false},
		{"observation_without_terminal_context", protocol.MessageAgentVeleroEvent, protocol.MessageAgentVeleroEvent, "", "", true, true, false},
		{"completion", protocol.MessageAgentTaskCompleted, protocol.MessageAgentTaskCompleted, "task", "command", true, true, true},
		{"failure", protocol.MessageAgentTaskFailed, protocol.MessageAgentTaskFailed, "task", "command", true, true, true},
		{"wrong_type", protocol.MessageAgentTaskCompleted, protocol.MessageAgentVeleroEvent, "task", "command", true, false, false},
		{"wrong_task", protocol.MessageAgentTaskCompleted, protocol.MessageAgentTaskCompleted, "other", "command", true, false, false},
		{"wrong_command", protocol.MessageAgentTaskCompleted, protocol.MessageAgentTaskCompleted, "task", "other", true, false, false},
		{"not_persisted", protocol.MessageAgentTaskCompleted, protocol.MessageAgentTaskCompleted, "task", "command", false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			box, err := newEventOutbox(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			event := protocol.Message[map[string]any]{MessageID: "event", Type: test.eventType, Payload: map[string]any{"taskId": "task", "commandId": "command"}}
			if err := box.add(event); err != nil {
				t.Fatal(err)
			}
			ack := protocol.EventAckPayload{AckMessageID: "event", AckType: test.ackType, TaskID: test.ackTask, CommandID: test.ackCommand, Persisted: test.persisted}
			_, terminal, err := box.acknowledge(ack)
			if err != nil || terminal != test.terminal || (len(box.list()) == 0) != test.removed {
				t.Fatalf("terminal=%v remaining=%d err=%v", terminal, len(box.list()), err)
			}
			if test.removed {
				_, terminal, err = box.acknowledge(ack)
				if err != nil || terminal {
					t.Fatalf("duplicate ACK changed ledger: %v %v", terminal, err)
				}
			}
		})
	}
}
