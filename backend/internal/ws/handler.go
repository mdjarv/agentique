package ws

import (
	"log/slog"
	"net/http"

	"github.com/allbin/agentique/backend/internal/persona"
	"github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/team"
	"github.com/gorilla/websocket"
)

// Handler handles WebSocket connections.
type Handler struct {
	Service           *session.Service
	GitService        *session.GitService
	ProjectGitService *project.GitService
	Queries           *store.Queries
	Hub               *Hub
	TeamService       *team.Service           // nil when experimental teams is disabled
	PersonaService    *persona.Service         // nil when experimental teams is disabled
	BrowserService    *session.BrowserService  // nil when browser support is unavailable
	AllowedOrigins    map[string]bool          // nil/empty = accept all origins
}

func (h *Handler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if len(h.AllowedOrigins) == 0 {
				return true
			}
			return h.AllowedOrigins[r.Header.Get("Origin")]
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u := h.upgrader()
	wsConn, err := u.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}

	slog.Info("ws connected", "remote", r.RemoteAddr)
	c := newConn(r.Context(), wsConn, h.Service, h.GitService, h.ProjectGitService, h.Queries, h.Hub, h.TeamService, h.PersonaService, h.BrowserService)
	c.run()
	slog.Info("ws disconnected", "remote", r.RemoteAddr)
}
