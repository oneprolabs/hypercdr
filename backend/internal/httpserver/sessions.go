package httpserver

import (
	"sort"
	"sync"

	"github.com/gorilla/websocket"
	"hypercdr-platform/platform/backend/internal/protocol"
)

// The authenticated socket's cluster owns a response, even if another agent
// knows its request ID or supplies a different cluster ID in the message body.
func agentRequestKey(clusterID, requestID string) string {
	return clusterID + "/" + requestID
}

type sessionHub struct {
	mu                 sync.RWMutex
	sessions           map[string]*websocket.Conn
	capacity           map[string]bool
	forceCleanup       map[string]bool
	taskAbort          map[string]bool
	consistency        map[string]map[string]bool
	snapshotBackup     map[string]bool
	consistencyRestore map[string]bool
}

func newSessionHub() *sessionHub {
	return &sessionHub{sessions: map[string]*websocket.Conn{}, capacity: map[string]bool{}, forceCleanup: map[string]bool{}, taskAbort: map[string]bool{}}
}

func (h *sessionHub) set(clusterID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[clusterID] = conn
	delete(h.capacity, clusterID)
	delete(h.forceCleanup, clusterID)
	delete(h.taskAbort, clusterID)
	delete(h.consistency, clusterID)
	delete(h.snapshotBackup, clusterID)
	delete(h.consistencyRestore, clusterID)
}

func (h *sessionHub) setConsistencyRestoreSupport(clusterID string, conn *websocket.Conn, supported bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] == conn {
		if h.consistencyRestore == nil {
			h.consistencyRestore = map[string]bool{}
		}
		h.consistencyRestore[clusterID] = supported
	}
}

func (h *sessionHub) supportsConsistencyRestore(clusterID string) bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[clusterID] != nil && h.consistencyRestore[clusterID]
}

func (h *sessionHub) setSnapshotBackupSupport(clusterID string, conn *websocket.Conn, supported bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] == conn {
		if h.snapshotBackup == nil {
			h.snapshotBackup = map[string]bool{}
		}
		h.snapshotBackup[clusterID] = supported
	}
}

func (h *sessionHub) supportsSnapshotBackup(clusterID string) bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[clusterID] != nil && h.snapshotBackup[clusterID]
}

func (h *sessionHub) setApplicationConsistencySupport(clusterID string, conn *websocket.Conn, capability *protocol.ApplicationConsistencyCapability) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] != conn {
		return
	}
	delete(h.consistency, clusterID)
	if capability == nil || capability.Validate() != nil {
		return
	}
	if h.consistency == nil {
		h.consistency = map[string]map[string]bool{}
	}
	profiles := map[string]bool{}
	for _, profile := range capability.Profiles {
		profiles[profile] = true
	}
	h.consistency[clusterID] = profiles
}

func (h *sessionHub) supportsApplicationConsistency(clusterID, profile string) bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[clusterID] != nil && h.consistency[clusterID][profile]
}

func (h *sessionHub) applicationConsistencyProfiles(clusterID string) []string {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.sessions[clusterID] == nil {
		return nil
	}
	profiles := make([]string, 0, len(h.consistency[clusterID]))
	for profile, supported := range h.consistency[clusterID] {
		if supported {
			profiles = append(profiles, profile)
		}
	}
	sort.Strings(profiles)
	return profiles
}

func (h *sessionHub) setTaskAbortSupport(clusterID string, conn *websocket.Conn, supported bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] == conn {
		h.taskAbort[clusterID] = supported
	}
}

func (h *sessionHub) supportsTaskAbort(clusterID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[clusterID] != nil && h.taskAbort[clusterID]
}

func (h *sessionHub) setForceCleanupSupport(clusterID string, conn *websocket.Conn, supported bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] == conn {
		h.forceCleanup[clusterID] = supported
	}
}

func (h *sessionHub) supportsForceCleanup(clusterID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[clusterID] != nil && h.forceCleanup[clusterID]
}

func (h *sessionHub) setCapacitySupport(clusterID string, conn *websocket.Conn, supported bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] == conn {
		h.capacity[clusterID] = supported
	}
}

func (h *sessionHub) supportsCapacity(clusterID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[clusterID] != nil && h.capacity[clusterID]
}

func (h *sessionHub) remove(clusterID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] == conn {
		delete(h.sessions, clusterID)
		delete(h.capacity, clusterID)
		delete(h.forceCleanup, clusterID)
		delete(h.taskAbort, clusterID)
		delete(h.consistency, clusterID)
		delete(h.snapshotBackup, clusterID)
		delete(h.consistencyRestore, clusterID)
	}
}

func (h *sessionHub) get(clusterID string) (*websocket.Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	conn, ok := h.sessions[clusterID]
	return conn, ok
}

func (h *sessionHub) has(clusterID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.sessions[clusterID]
	return ok
}

func (h *sessionHub) close(clusterID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conn, ok := h.sessions[clusterID]; ok {
		_ = conn.Close()
		delete(h.sessions, clusterID)
		delete(h.capacity, clusterID)
		delete(h.forceCleanup, clusterID)
		delete(h.taskAbort, clusterID)
		delete(h.consistency, clusterID)
	}
}
