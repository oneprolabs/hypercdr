package protocol

import "time"

func NewMessage[T any](messageKind string, messageType string, clusterID string, agentID string, payload T) Message[T] {
	return Message[T]{
		Version:     Version,
		MessageID:   newMessageID(),
		MessageKind: messageKind,
		Type:        messageType,
		ClusterID:   clusterID,
		AgentID:     agentID,
		Timestamp:   time.Now().UTC(),
		Payload:     payload,
	}
}
