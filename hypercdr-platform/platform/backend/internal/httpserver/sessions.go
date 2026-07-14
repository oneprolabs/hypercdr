package httpserver

import (
	"sync"

	"github.com/gorilla/websocket"
)

type sessionHub struct {
	mu       sync.RWMutex
	sessions map[string]*websocket.Conn
}

func newSessionHub() *sessionHub {
	return &sessionHub{sessions: map[string]*websocket.Conn{}}
}

func (h *sessionHub) set(clusterID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[clusterID] = conn
}

func (h *sessionHub) remove(clusterID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[clusterID] == conn {
		delete(h.sessions, clusterID)
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
	}
}
