package ws

import (
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/allbin/agentkit/eventbus"
	"github.com/gorilla/websocket"
	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/httpsecurity"
	"github.com/mdjarv/agentique/backend/internal/persona"
	"github.com/mdjarv/agentique/backend/internal/project"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/schedule"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/team"
)

// Handler handles WebSocket connections.
type Handler struct {
	Service           *session.Service
	GitService        *session.GitService
	ProjectGitService *project.GitService
	Queries           *store.Queries
	Bus               *eventbus.Bus
	TeamService       *team.Service           // nil when experimental teams is disabled
	PersonaService    *persona.Service        // nil when experimental teams is disabled
	BrowserService    *session.BrowserService // nil when browser support is unavailable
	ScheduleService   *schedule.Scheduler     // nil when the scheduler is disabled
	Catalog           *providers.Catalog      // model catalog; nil falls back to base aliases
	AllowedOrigins    map[string]bool         // additional origins; same-origin is always accepted
	AllowTicketOrigin bool                    // auth middleware validated wsTicket before this handler
	SessionTracker    SessionTracker
	MaxConnections    int64
	MaxMessageBytes   int64
	activeConnections atomic.Int64
}

const defaultMaxConnections int64 = 128

// SessionTracker owns the lifetime of authenticated WebSockets. The auth
// module implements it so revocation and expiry can close established sockets.
type SessionTracker interface {
	TrackWebSocket(*store.GetAuthSessionRow, func()) (func(), error)
}

func (h *Handler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if httpsecurity.OriginAllowed(r, h.AllowedOrigins) {
				return true
			}
			// A wsTicket-bearing upgrade is authenticated by the one-time
			// ticket (validated by the auth middleware before this handler
			// runs), not by origin — cross-origin multi-machine clients
			// connect this way. The origin allowlist only guards
			// cookie-authenticated upgrades against cross-site hijacking.
			if h.AllowTicketOrigin && r.URL.Query().Get("wsTicket") != "" {
				return true
			}
			return false
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := h.MaxConnections
	if limit <= 0 {
		limit = defaultMaxConnections
	}
	if active := h.activeConnections.Add(1); active > limit {
		h.activeConnections.Add(-1)
		http.Error(w, "too many WebSocket connections", http.StatusServiceUnavailable)
		return
	}
	defer h.activeConnections.Add(-1)

	u := h.upgrader()
	wsConn, err := u.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}

	slog.Info("ws connected", "remote", r.RemoteAddr)
	c := newConn(r.Context(), wsConn, h.Service, h.GitService, h.ProjectGitService, h.Queries, h.Bus, h.TeamService, h.PersonaService, h.BrowserService, h.ScheduleService, h.Catalog, h.MaxMessageBytes)
	if h.SessionTracker != nil {
		session := auth.UserFromContext(r.Context())
		untrack, err := h.SessionTracker.TrackWebSocket(session, c.close)
		if err != nil {
			c.close()
			slog.Warn("ws auth session tracking failed", "error", err, "remote", r.RemoteAddr)
			return
		}
		defer untrack()
	}
	c.run()
	slog.Info("ws disconnected", "remote", r.RemoteAddr)
}
