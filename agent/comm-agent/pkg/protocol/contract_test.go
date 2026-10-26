package protocol

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestContractsCoverEveryWireMessage(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "messages.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]bool{}
	ast.Inspect(source, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Values) != 1 || !strings.HasPrefix(spec.Names[0].Name, "Message") || strings.HasPrefix(spec.Names[0].Name, "MessageKind") {
			return true
		}
		literal, ok := spec.Values[0].(*ast.BasicLit)
		if ok {
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			constants[value] = true
		}
		return true
	})
	seen := map[string]bool{}
	for _, definition := range Contracts() {
		if seen[definition.Type] || !constants[definition.Type] {
			t.Fatalf("duplicate or undeclared contract: %s", definition.Type)
		}
		seen[definition.Type] = true
		if definition.TerminalResult != (definition.Type == MessageAgentTaskCompleted || definition.Type == MessageAgentTaskFailed) {
			t.Fatalf("invalid terminal authority: %s", definition.Type)
		}
		for _, kind := range definition.AllowedKinds {
			data, err := json.Marshal(Message[map[string]any]{Version: Version, MessageKind: kind, Type: definition.Type, ClusterID: "cluster", Payload: map[string]any{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateV1Message(data, definition.Direction, "cluster"); err != nil {
				t.Fatalf("valid %s: %v", definition.Type, err)
			}
			other := AgentToPlatform
			if definition.Direction == other {
				other = PlatformToAgent
			}
			if err := ValidateV1Message(data, other, "cluster"); err == nil {
				t.Fatalf("wrong direction accepted: %s", definition.Type)
			}
		}
	}
	for messageType := range constants {
		if !seen[messageType] {
			t.Errorf("missing contract: %s", messageType)
		}
	}
}

func TestV1EnvelopeCompatibilityAndRejection(t *testing.T) {
	for _, test := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"legacy omissions", `{"type":"agent.task.progress","payload":{"taskId":"task"}}`, true},
		{"additive fields", `{"version":"v1","type":"agent.task.progress","future":true,"payload":{"future":true}}`, true},
		{"known zero", `{"type":"agent.task.completed","payload":{"sizeMetricsV2":{"schemaVersion":2,"newData":{"known":true,"totalBytes":0}}}}`, true},
		{"inventory event", `{"type":"agent.inventory.report","messageKind":"event","payload":{}}`, true},
		{"inventory response", `{"type":"agent.inventory.report","messageKind":"response","payload":{}}`, true},
		{"future version", `{"version":"v2","type":"agent.task.progress","payload":{}}`, false},
		{"cross cluster", `{"clusterId":"other","type":"agent.task.progress","payload":{}}`, false},
		{"wrong kind", `{"messageKind":"request","type":"agent.task.progress","payload":{}}`, false},
		{"wrong direction", `{"type":"platform.task.dispatch","payload":{}}`, false},
		{"unknown type", `{"type":"agent.unknown","payload":{}}`, false},
		{"missing payload", `{"type":"agent.task.progress"}`, false},
		{"null payload", `{"type":"agent.task.progress","payload":null}`, false},
		{"array payload", `{"type":"agent.task.progress","payload":[]}`, false},
		{"wrong payload field type", `{"type":"agent.task.progress","payload":{"progress":"finished"}}`, false},
		{"invalid json", `{`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateV1Message([]byte(test.data), AgentToPlatform, "cluster")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestContractCopiesCannotMutateAuthority(t *testing.T) {
	definitions := Contracts()
	definitions[0].AllowedKinds[0] = "corrupted"
	definitions[0].TerminalResult = true
	definition, ok := LookupContract(MessageAgentRegister)
	if !ok || definition.TerminalResult || definition.AllowedKinds[0] != MessageKindRequest {
		t.Fatal("caller mutated authoritative contract")
	}
}
