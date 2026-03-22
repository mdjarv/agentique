package ws

import "sync"

// Hub manages project-scoped broadcast to WebSocket connections.
type Hub struct {
	mu    sync.RWMutex
	conns map[string]map[*conn]struct{} // projectID -> set of conns
}

func NewHub() *Hub {
	return &Hub{conns: make(map[string]map[*conn]struct{})}
}

// Subscribe registers a connection for events in a project.
func (h *Hub) Subscribe(projectID string, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[projectID] == nil {
		h.conns[projectID] = make(map[*conn]struct{})
	}
	h.conns[projectID][c] = struct{}{}
}

// Unsubscribe removes a connection from all projects.
func (h *Hub) Unsubscribe(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for pid, set := range h.conns {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, pid)
		}
	}
}

// Broadcast sends a push message to all connections subscribed to a project.
func (h *Hub) Broadcast(projectID, pushType string, payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.conns[projectID] {
		c.push(pushType, payload)
	}
}
