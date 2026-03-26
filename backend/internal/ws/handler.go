package ws

import (
	"log/slog"
	"net/http"

	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler handles WebSocket connections.
type Handler struct {
	Service           *session.Service
	GitService        *session.GitService
	ProjectGitService *project.GitService
	Queries           *store.Queries
	Hub               *Hub
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}

	slog.Info("ws connected", "remote", r.RemoteAddr)
	c := newConn(r.Context(), wsConn, h.Service, h.GitService, h.ProjectGitService, h.Queries, h.Hub)
	c.run()
	slog.Info("ws disconnected", "remote", r.RemoteAddr)
}
