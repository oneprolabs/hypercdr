package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type DecodeError struct {
	Stage       string
	MessageType string
	Err         error
}

func (e *DecodeError) Error() string {
	if e.MessageType == "" {
		return fmt.Sprintf("protocol %s: %v", e.Stage, e.Err)
	}
	return fmt.Sprintf("protocol %s for %s: %v", e.Stage, e.MessageType, e.Err)
}

func (e *DecodeError) Unwrap() error { return e.Err }

func EncodeMessage[T any](message Message[T]) ([]byte, error) {
	definition, ok := LookupContract(message.Type)
	if !ok {
		return nil, &DecodeError{Stage: "encode", MessageType: message.Type, Err: fmt.Errorf("unknown message type")}
	}
	if err := validateMessageMetadata(message.Version, message.MessageKind, definition); err != nil {
		return nil, &DecodeError{Stage: "encode", MessageType: message.Type, Err: err}
	}
	payload, err := json.Marshal(message.Payload)
	if err != nil {
		return nil, &DecodeError{Stage: "encode", MessageType: message.Type, Err: fmt.Errorf("payload: %w", err)}
	}
	if err := validatePayload(payload, definition); err != nil {
		return nil, &DecodeError{Stage: "encode", MessageType: message.Type, Err: err}
	}
	return json.Marshal(message)
}

func DecodeMessage(data []byte, direction Direction, authenticatedCluster string) (Message[json.RawMessage], error) {
	var message Message[json.RawMessage]
	if err := json.Unmarshal(data, &message); err != nil {
		return message, &DecodeError{Stage: "envelope", Err: err}
	}
	definition, ok := LookupContract(message.Type)
	if !ok {
		return message, &DecodeError{Stage: "contract", MessageType: message.Type, Err: fmt.Errorf("unknown message type")}
	}
	if err := validateMessageMetadata(message.Version, message.MessageKind, definition); err != nil {
		return message, &DecodeError{Stage: "metadata", MessageType: message.Type, Err: err}
	}
	if definition.Direction != direction {
		return message, &DecodeError{Stage: "direction", MessageType: message.Type, Err: fmt.Errorf("got %s, want %s", direction, definition.Direction)}
	}
	if authenticatedCluster != "" && message.ClusterID != "" && message.ClusterID != authenticatedCluster {
		return message, &DecodeError{Stage: "identity", MessageType: message.Type, Err: fmt.Errorf("cluster does not match authenticated connection")}
	}
	if err := validatePayload(message.Payload, definition); err != nil {
		return message, &DecodeError{Stage: "payload", MessageType: message.Type, Err: err}
	}
	return message, nil
}

func validateMessageMetadata(version string, kind string, definition Contract) error {
	if version != "" && version != Version {
		return fmt.Errorf("unsupported protocol version %q", version)
	}
	if kind == "" {
		return nil
	}
	for _, allowed := range definition.AllowedKinds {
		if kind == allowed {
			return nil
		}
	}
	return fmt.Errorf("invalid message kind %q", kind)
}

func validatePayload(payload json.RawMessage, definition Contract) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("payload must be an object")
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("payload must be an object")
	}
	if err := json.Unmarshal(trimmed, newPayload(definition)); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}
	return nil
}
