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

// briefingBudget bounds everything gathered between the socket opening and the
// engine existing: the persona, the project context and the orientation.
//
// A budget rather than a hope. None of it is required — a call that opens
// knowing less still works, and a call that never opens does not — so a read
// that hangs must cost the drafter context rather than cost the operator the
// call. The socket is already up by the time this applies, so it no longer
// delays the upgrade; it only bounds how long `ready` can be held back.
const briefingBudget = 5 * time.Second

// engineDialBudget bounds the speech backend's handshake.
//
// The backend is a network round trip to somebody else's service, and it had no
// timeout of ours: a dial that never answered held its call slot open until the
// browser gave up, and there are only [defaultMaxCalls] of those. It is worse
// now that the client rings while it connects — the operator would hear a call
// ringing forever at a backend that is never going to answer, which is exactly
// the wrong thing to tell someone who cannot look at the screen.
//
// Generous rather than tight: a cold realtime session legitimately takes
// seconds, and a call that opens late still works, so this is the point at
// which "slow" has become "not coming" rather than a latency target.
const engineDialBudget = 15 * time.Second

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
	// Directory is what this machine knows about its own sessions. Nil is
	// valid: the assistant's directory tools then answer, in words, that they
	// are not available, and the call is exactly the single-session call it was
	// before.
	Directory Directory
}

// Handler serves the live voice socket.
//
// Route it under /api/ rather than beside /ws. The auth middleware protects the
// /api/ prefix and the exact string "/ws"; a socket mounted at /ws/voice would
// fall through as an SPA asset and stream a live microphone to a paid API with
// no credential at all.
type Handler struct {
	opts Options

	// newEngine builds one call's engine. A field rather than a plain method so
	// a test can make engine creation fail or block *after* the upgrade, which
	// is the ordering this handler exists to guarantee. Production always gets
	// [Handler.defaultEngine].
	newEngine func(ctx context.Context, systemInstruction string, persona Persona) (Engine, error)

	// briefingBudget is [briefingBudget], as a field so a test can assert the
	// budget without waiting one out.
	briefingBudget time.Duration

	// engineDialBudget is [engineDialBudget], a field for the same reason.
	engineDialBudget time.Duration

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
	h := &Handler{opts: opts, briefingBudget: briefingBudget, engineDialBudget: engineDialBudget}
	h.newEngine = h.defaultEngine
	return h, nil
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

	// The query parameter is the call's *initial* focus — the session the
	// operator was looking at when they pressed the button — not a fixed target.
	initialFocus := r.URL.Query().Get("sessionId")

	// UPGRADE FIRST, then gather. The order is the feature.
	//
	// Everything the drafter is told — persona, project context, orientation —
	// and the engine handshake itself take real time, and none of it can be
	// reported to a browser still waiting on an HTTP response. Building it
	// before the upgrade opened the socket seconds late and, on a long session
	// whose summary kept missing its budget, often not at all: the browser gave
	// up, the request context was cancelled underneath the reads, and the
	// operator got a call that transcribed their speech and never answered.
	//
	// So the cheap, fail-fast half of the handshake goes first, and the socket
	// is a fact before anything slow is attempted. Every failure after this
	// point is reported *on the socket*, because an HTTP status written into a
	// hijacked connection is read by nobody.
	u := h.upgrader()
	ws, err := u.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written its own response.
		slog.Warn("voice upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}

	log := slog.With("subsystem", "voice", "backend", h.opts.Backend)
	log.Info("voice call opened", "remote", r.RemoteAddr, "session", initialFocus)

	// A live speech session must outlive the request that opened it: the HTTP
	// context is cancelled when ServeHTTP returns, which for a hijacked
	// WebSocket is not what the call's lifetime is made of. The read loop
	// observing the socket close is what ends the call.
	callCtx := context.WithoutCancel(r.Context())

	engine, err := h.openEngine(callCtx, initialFocus)
	if err != nil {
		// The detail goes to the log; the browser gets a fixed message, the
		// same rule an unclassified 500 follows.
		log.Error("voice engine start failed", "error", err)
		refuse(ws, "the voice backend is unavailable")
		return
	}

	newCall(ws, engine, h.opts, initialFocus, log).run(callCtx)
	log.Info("voice call closed", "remote", r.RemoteAddr)
}

// openEngine gathers what the drafter is told and starts the speech engine.
//
// The drafter's instruction is built per call, because project context belongs
// to the session this call is attached to, and the persona is read per call so
// a change in the settings page takes effect on the next call rather than the
// next restart. Both are bounded by [briefingBudget]: they are held between the
// socket opening and `ready` reaching the browser, which is dead air.
func (h *Handler) openEngine(ctx context.Context, initialFocus string) (Engine, error) {
	budget := h.briefingBudget
	if budget <= 0 {
		budget = briefingBudget
	}
	briefCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	persona := h.persona(briefCtx)
	instruction := SystemInstruction(Briefing{
		InitialFocus:   initialFocus,
		ProjectContext: h.projectContext(briefCtx, initialFocus),
		Orientation:    h.orientation(briefCtx),
		Persona:        persona,
	})
	// The engine's context is the call's, not the briefing's: it must outlive
	// both the request and the budget that bounded the gathering.
	return h.dialEngine(ctx, instruction, persona)
}

// dialEngine builds the engine, giving up on a backend that will not answer.
//
// The wait is bounded here rather than by handing [Handler.newEngine] a
// deadline context, because that context is the *call's*: cancelling it on
// expiry would take the engine down later, mid-conversation, having succeeded.
// So the dial runs where it can outlive the wait, and a late arrival is closed
// rather than abandoned — an engine nobody is holding is a paid session nobody
// is listening to.
func (h *Handler) dialEngine(ctx context.Context, instruction string, persona Persona) (Engine, error) {
	budget := h.engineDialBudget
	if budget <= 0 {
		budget = engineDialBudget
	}

	type dialed struct {
		engine Engine
		err    error
	}
	// Buffered: the dial must be able to finish and exit even after the wait
	// has been given up on, or a slow backend leaks a goroutine per call.
	done := make(chan dialed, 1)
	go func() {
		engine, err := h.newEngine(ctx, instruction, persona)
		done <- dialed{engine, err}
	}()

	timer := time.NewTimer(budget)
	defer timer.Stop()

	select {
	case d := <-done:
		return d.engine, d.err
	case <-timer.C:
		go func() {
			if d := <-done; d.engine != nil {
				_ = d.engine.Close()
			}
		}()
		return nil, fmt.Errorf("the speech backend did not answer within %s", budget)
	}
}

// refuse says on an already-open socket why the call is not happening, and
// hangs up.
//
// The `error` frame carries the reason rather than a `closed` frame because the
// client keeps a terminal detail across the socket closing behind it: the
// operator reads why, not just that. Best effort throughout — there is nothing
// left to do about a write that fails here.
func refuse(ws *websocket.Conn, reason string) {
	_ = ws.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = ws.WriteJSON(serverMessage{Type: msgError, Message: reason})
	_ = ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseInternalServerErr, ""),
		time.Now().Add(writeTimeout))
	_ = ws.Close()
}

// defaultEngine builds the configured speech engine for one call.
//
// A per-call engine is not an optimisation. Sharing one across calls means the
// second caller's stream overwrites the first's, and results are delivered to
// whoever asked most recently.
func (h *Handler) defaultEngine(ctx context.Context, systemInstruction string, persona Persona) (Engine, error) {
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

// orientation is what is going on across this machine when the call opens.
//
// It costs one paragraph and saves the assistant a tool call before it can
// answer the first question, which over a call is the difference between an
// answer and a pause. A call with no directory opens without it, exactly as a
// call with no session opens without project context.
func (h *Handler) orientation(ctx context.Context) string {
	if h.opts.Directory == nil {
		return ""
	}
	return h.opts.Directory.Orientation(ctx)
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
