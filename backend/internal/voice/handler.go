package voice

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mdjarv/agentique/backend/internal/httpsecurity"
)

// defaultMaxCalls bounds concurrent live calls. Low on purpose: each one holds
// an open microphone and, on a real backend, bills for wall-clock time.
const defaultMaxCalls int64 = 4

// Options configures a [Handler].
type Options struct {
	// Backend selects the speech transport. BackendEcho needs no credentials
	// and contacts nothing.
	Backend Backend
	// APIKey authenticates BackendAIStudio.
	APIKey string
	// Project and Location address BackendVertex.
	Project  string
	Location string
	// Model overrides the backend's default realtime model id.
	Model string
	// IdleTimeout closes a call whose caller has gone quiet. 0 = the built-in
	// default.
	IdleTimeout time.Duration
	// AllowedOrigins are extra browser origins permitted to upgrade;
	// same-origin is always accepted.
	AllowedOrigins map[string]bool
	// AllowTicketOrigin mirrors the main socket: a wsTicket-bearing upgrade was
	// authenticated by the middleware before this handler runs, so it is not
	// judged on origin.
	AllowTicketOrigin bool
	// MaxCalls bounds concurrent calls. 0 = the built-in default.
	MaxCalls int64
	// Registry routes a followed session's progress reports into live calls.
	// Nil disables following — a call still works, it just hears nothing from
	// the sessions it starts.
	Registry *Registry
}

// Handler serves the live voice socket.
//
// Route it under /api/ rather than beside /ws. The auth middleware protects the
// /api/ prefix and the exact string "/ws"; a socket mounted at /ws/voice would
// fall through as an SPA asset and stream a live microphone to a paid API with
// no credential at all.
type Handler struct {
	opts Options

	activeCalls atomic.Int64
}

// NewHandler returns a voice handler, or an error if the options do not
// describe a usable backend.
func NewHandler(opts Options) (*Handler, error) {
	switch opts.Backend {
	case BackendEcho:
		// No credentials by construction.
	case BackendAIStudio:
		if opts.APIKey == "" {
			return nil, fmt.Errorf("voice backend %q needs [voice] api-key", opts.Backend)
		}
	case BackendVertex:
		if opts.Project == "" {
			return nil, fmt.Errorf("voice backend %q needs [voice] project", opts.Backend)
		}
	default:
		return nil, fmt.Errorf("unknown voice backend %q", opts.Backend)
	}
	return &Handler{opts: opts}, nil
}

// Backend reports which speech transport this handler will use.
func (h *Handler) Backend() Backend { return h.opts.Backend }

func (h *Handler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return httpsecurity.WebSocketOriginAllowed(r, h.opts.AllowedOrigins, h.opts.AllowTicketOrigin)
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := h.opts.MaxCalls
	if limit <= 0 {
		limit = defaultMaxCalls
	}
	if active := h.activeCalls.Add(1); active > limit {
		h.activeCalls.Add(-1)
		http.Error(w, "too many voice calls", http.StatusServiceUnavailable)
		return
	}
	defer h.activeCalls.Add(-1)

	engine, err := h.newEngine()
	if err != nil {
		// The detail goes to the log; an unclassified failure returns a fixed
		// message rather than err.Error().
		slog.Error("voice engine start failed", "error", err, "backend", h.opts.Backend)
		http.Error(w, "voice is unavailable", http.StatusServiceUnavailable)
		return
	}

	u := h.upgrader()
	ws, err := u.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written its own response.
		_ = engine.Close()
		slog.Warn("voice upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}

	log := slog.With("subsystem", "voice", "backend", h.opts.Backend)
	log.Info("voice call opened", "remote", r.RemoteAddr)
	newCall(ws, engine, h.opts.Registry, h.opts.IdleTimeout, log).run(r.Context())
	log.Info("voice call closed", "remote", r.RemoteAddr)
}

// newEngine builds the configured speech engine for one call.
//
// A per-call engine is not an optimisation. Sharing one across calls means the
// second caller's stream overwrites the first's, and results are delivered to
// whoever asked most recently.
func (h *Handler) newEngine() (Engine, error) {
	switch h.opts.Backend {
	case BackendEcho:
		return NewEchoEngine(), nil
	case BackendAIStudio, BackendVertex:
		return nil, fmt.Errorf("voice backend %q is not implemented yet", h.opts.Backend)
	default:
		return nil, fmt.Errorf("unknown voice backend %q", h.opts.Backend)
	}
}
