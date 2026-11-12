package httpserver

import (
	"sync"

	"github.com/gorilla/websocket"
)

// The authenticated socket's cluster owns a response, even if another agent
// knows its request ID or supplies a different cluster ID in the message body.
func agentRequestKey(clusterID, requestID string) string {
	return clusterID + "/" + requestID
}

type sessionHub struct {
	mu           sync.RWMutex
	sessions     map[string]*websocket.Conn
	capacity     map[string]bool
	forceCleanup map[string]bool
	taskAbort    map[string]bool
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
	}
}
