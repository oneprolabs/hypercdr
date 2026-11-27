package wsclient

import "hypercdr-platform/agent/comm-agent/pkg/protocol"

func requiresCoordinatedBackup(task protocol.TaskDispatchPayload) bool {
	return task.Backup != nil && (task.Backup.DataProtectionMode == "application-consistent" || task.Backup.ConsistencyGroup != nil)
}
