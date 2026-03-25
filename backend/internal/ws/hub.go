package ws

import "sync"

// EventListener receives all broadcast events regardless of project.
type EventListener interface {
	OnEvent(projectID, pushType string, payload any)
}

// Hub manages project-scoped broadcast to WebSocket connections
// and global broadcast to EventListeners (used by SSE).
type Hub struct {
	mu        sync.RWMutex
	conns     map[string]map[*conn]struct{} // projectID -> set of conns
	listeners map[EventListener]struct{}
}

func NewHub() *Hub {
	return &Hub{
		conns:     make(map[string]map[*conn]struct{}),
		listeners: make(map[EventListener]struct{}),
	}
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

// AddListener registers a global event listener.
func (h *Hub) AddListener(l EventListener) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listeners[l] = struct{}{}
}

// RemoveListener unregisters a global event listener.
func (h *Hub) RemoveListener(l EventListener) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.listeners, l)
}

// Broadcast sends a push message to all connections subscribed to a project
// and to all global event listeners.
func (h *Hub) Broadcast(projectID, pushType string, payload any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.conns[projectID] {
		c.push(pushType, payload)
	}
	for l := range h.listeners {
		l.OnEvent(projectID, pushType, payload)
	}
}
