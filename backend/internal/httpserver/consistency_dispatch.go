package httpserver

import (
	"encoding/json"
	"errors"

	"hypercdr-platform/platform/backend/internal/protocol"
	"hypercdr-platform/platform/backend/internal/store"
)

func (r *Router) snapshotBackupDispatchSupported(task store.Task) bool {
	version, err := json.Marshal(task.Payload["dataProtectionSnapshotVersion"])
	if err != nil || string(version) != "1" || task.Type != "backup" || task.ID == "" || task.CommandID == "" || task.ProtectionPlanID == "" || r.store == nil || !r.hub.supportsSnapshotBackup(task.ClusterID) {
		return false
	}
	group, present := task.Payload["consistencyGroup"]
	return present && group == nil && task.Payload["dataProtectionMode"] == "snapshot-crash-consistent"
}

func (r *Router) applicationConsistencyDispatchGroup(task store.Task) (*protocol.ConsistencyGroup, error) {
	return nil, errors.New("application stop/freeze coordination is no longer supported; explicitly select CSI snapshots; refusing legacy fallback")
}
