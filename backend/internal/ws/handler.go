package ws

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler handles WebSocket connections.
type Handler struct {
	Queries *store.Queries
	Manager *session.Manager
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	c := newConn(r.Context(), wsConn, h.Queries, h.Manager)
	c.run()
}
