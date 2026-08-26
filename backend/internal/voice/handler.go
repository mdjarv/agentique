package voice

import (
	"context"
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
	// Personas supplies the operator's chosen voice and character per call.
	// Nil leaves the built-in behaviour, which is what an operator who has
	// never opened the settings page gets.
	Personas PersonaSource
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
	// Persona is resolved per call from Personas; callers do not set it.
	Persona Persona
	// Dispatcher hands a drafted prompt to the session that does the work.
	// Nil leaves the call conversational — it can talk, but cannot start work.
	Dispatcher Dispatcher
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

	// A live speech session must outlive the request that opened it: the HTTP
	// context is cancelled when ServeHTTP returns, which for a hijacked
	// WebSocket is not when the call ends.
	// The drafter's instruction is built per call, because project context
	// belongs to the session this call is attached to.
	// Both are read per call, so a change in the settings page takes effect on
	// the next call rather than the next restart.
	//
	// The query parameter is the call's *initial* focus — the session the
	// operator was looking at when they pressed the button — not a fixed target.
	initialFocus := r.URL.Query().Get("sessionId")
	persona := h.persona(r.Context())
	instruction := SystemInstruction(h.projectContext(r.Context(), initialFocus), persona)

	engine, err := h.newEngine(context.WithoutCancel(r.Context()), instruction, persona)
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
	log.Info("voice call opened", "remote", r.RemoteAddr, "session", initialFocus)
	newCall(ws, engine, h.opts, initialFocus, log).run(r.Context())
	log.Info("voice call closed", "remote", r.RemoteAddr)
}

// newEngine builds the configured speech engine for one call.
//
// A per-call engine is not an optimisation. Sharing one across calls means the
// second caller's stream overwrites the first's, and results are delivered to
// whoever asked most recently.
func (h *Handler) newEngine(ctx context.Context, systemInstruction string, persona Persona) (Engine, error) {
	switch h.opts.Backend {
	case BackendEcho:
		return NewEchoEngine(), nil
	case BackendAIStudio, BackendVertex:
		// The engine's context is the call's, not the request's: it must
		// outlive the HTTP handler that created it.
		opts := h.opts
		opts.Persona = persona
		return newGeminiEngine(ctx, opts, systemInstruction, slog.With("subsystem", "voice"))
	default:
		return nil, fmt.Errorf("unknown voice backend %q", h.opts.Backend)
	}
}

// projectContext asks the dispatcher what the drafter should know. A call with
// no session, or no dispatcher, still works — it just drafts generically.
func (h *Handler) projectContext(ctx context.Context, sessionID string) string {
	if h.opts.Dispatcher == nil || sessionID == "" {
		return ""
	}
	return h.opts.Dispatcher.ProjectContext(ctx, sessionID)
}

// PersonaSource supplies the operator's current voice settings.
type PersonaSource interface {
	Persona(ctx context.Context) Persona
}

// persona reads the current settings, falling back to the built-in behaviour.
func (h *Handler) persona(ctx context.Context) Persona {
	if h.opts.Personas == nil {
		return Persona{}.Sanitize()
	}
	return h.opts.Personas.Persona(ctx).Sanitize()
}
