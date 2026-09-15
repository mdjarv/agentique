package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/google/uuid"
)

// personaRuntime drives one discussion persona's turns, abstracting how the
// persona is executed so the orchestrator (discussion.go) has a single turn
// path. Two implementations:
//
//   - dbSessionPersona — repo-backed: a full agentique session (today's path),
//     with a worktree, DB row, and the channel-owned lifecycle.
//   - sessionlessPersona — web-only: a raw runtime CLI subprocess with no DB
//     row, worktree, project, or brain recall, owned by the orchestrator.
type personaRuntime interface {
	// Query runs one turn with prompt and returns the persona's reply text.
	Query(ctx context.Context, prompt string) (string, error)
	// Close releases the persona's runtime resources. Idempotent.
	Close() error
}

// dbSessionPersona drives a repo-backed persona that is a full agentique session.
// This is the pre-existing path, factored behind personaRuntime unchanged: install
// a turn-complete hook to capture the reply, kick the turn via QuerySession (which
// appends the cross-injected prompt to history AND runs the turn), and wait.
type dbSessionPersona struct {
	svc       *Service
	sessionID string
}

func (d *dbSessionPersona) Query(ctx context.Context, prompt string) (string, error) {
	// The outcome subscription is atomic with the turn start, so this cannot
	// capture a neighbouring turn (another persona, a human) and cannot miss
	// a fast completion. FinalText is provider-independent (the pipeline
	// accumulates codex's assistant text, whose adapter leaves
	// TurnCompletedEvent.Text empty), and a Stop mid-turn resolves with
	// SessionClosed instead of stranding the round until its timeout.
	_, outcome, err := d.svc.QuerySessionWithOutcome(ctx, d.sessionID, prompt, nil, QueryOrigin{})
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}

	select {
	case out := <-outcome:
		if out.SessionClosed {
			return "", fmt.Errorf("session closed before the turn completed")
		}
		return strings.TrimSpace(out.FinalText), nil
	case <-time.After(discussionTurnTimeout):
		return "", fmt.Errorf("turn timed out after %s", discussionTurnTimeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close is a no-op: a repo-backed persona's lifecycle is owned by the channel.
// teardownDiscussion dissolves the channel (which stops the session) and removes
// the shared worktree exactly once — tearing the session down here would race
// that path and risk reaping the orchestrator-owned shared tree.
func (d *dbSessionPersona) Close() error { return nil }

// sessionlessPersona drives a web-only persona backed by a raw runtime CLI
// session: no agentique sessions row, no worktree, no project, no brain recall.
// It runs headless/fullAuto — tool approvals are auto-allowed by the runtime
// Session's permission pump (which is why it is created through runtime.Manager,
// not a bare connector.Connect: the claude adapter ignores ConnectParams.AutoApprove,
// so the pump is the only thing that enforces it).
type sessionlessPersona struct {
	id   string
	rt   *runtime.Manager
	sess *runtime.Session

	// onText, when set, receives assistant text deltas as they stream. Fixed at
	// construction and never written afterwards, so it needs no lock.
	onText func(string)

	// onThought, when set, receives each block of the persona's own reasoning,
	// "" when the provider withheld the text. Fixed at construction, like onText.
	onThought func(string)

	// done is the per-turn delivery channel for the turn-complete event, swapped
	// in by Query and read by onEvent. Guarded by mu.
	mu   sync.Mutex
	done chan runtime.TurnCompletedEvent
}

// onEvent is the runtime broadcast hook. It forwards the turn-complete event to
// the in-flight Query (if any), text deltas to onText and the persona's own
// thinking blocks to onThought when a caller asked for them; everything else
// (tool events, state changes) is ignored — a
// discussion contribution is mirrored to the channel timeline once, on
// completion, exactly like recordContribution does today. Called synchronously
// from a runtime goroutine, so it must not block.
func (p *sessionlessPersona) onEvent(_ context.Context, e runtime.Event) {
	// Streaming is opt-in per persona: a discussion contribution is whole, and
	// only a caller that renders a reply as it arrives (the assistant's thread)
	// asks for the deltas that make it possible.
	if p.onText != nil {
		if delta, ok := e.(runtime.AssistantTextDeltaEvent); ok {
			p.onText(delta.Delta)
			return
		}
	}
	// A subagent's thinking is not the persona's: it carries a parent tool use,
	// and the persona has no subagents of its own to attribute it to.
	if p.onThought != nil {
		if thought, ok := e.(runtime.ThinkingEvent); ok && thought.ParentToolUseID == "" {
			p.onThought(thought.Content)
			return
		}
	}
	tc, ok := e.(runtime.TurnCompletedEvent)
	if !ok {
		return
	}
	p.mu.Lock()
	ch := p.done
	p.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- tc:
	default:
	}
}

func (p *sessionlessPersona) Query(ctx context.Context, prompt string) (string, error) {
	done := make(chan runtime.TurnCompletedEvent, 1)
	p.mu.Lock()
	p.done = done
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.done = nil
		p.mu.Unlock()
	}()

	if err := p.sess.Query(ctx, prompt); err != nil {
		return "", fmt.Errorf("query: %w", err)
	}

	select {
	case tc := <-done:
		return strings.TrimSpace(tc.Text), nil
	case <-time.After(discussionTurnTimeout):
		return "", fmt.Errorf("turn timed out after %s", discussionTurnTimeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close stops the runtime CLI subprocess and removes it from the runtime manager.
// The per-discussion scratch dir is shared across personas and removed once by
// teardownDiscussion, not here.
func (p *sessionlessPersona) Close() error {
	if p.rt == nil {
		return nil
	}
	return p.rt.Stop(context.Background(), p.id)
}

// PersonaRuntimeParams configures a sessionless persona CLI subprocess.
type PersonaRuntimeParams struct {
	Preamble string // lean persona preamble (see buildPersonaPreamble)
	Model    string
	Effort   string
	WorkDir  string // per-discussion scratch dir — NOT a project worktree

	// ID overrides the generated runtime id. Empty generates one.
	//
	// It exists because an MCP bearer is minted per calling id: a persona that
	// is to reach agentique's own tools needs its token in the store BEFORE the
	// subprocess starts, which cannot be done for an id this function invents.
	ID string

	// MCPConfigs are inline JSON or file paths handed to the provider CLI, for
	// a persona that needs tools of its own. A credential goes in a 0600 FILE
	// and never inline: /proc/<pid>/cmdline is world-readable.
	MCPConfigs []string

	// DisallowedTools are provider-native tool names this persona must not
	// have. Empty leaves the CLI's own default set.
	//
	// It matters because a persona runs fullAuto — the approval pump
	// auto-allows everything, by design, since there is no screen to ask — so
	// the tools it holds are the tools it can use without anybody agreeing.
	// A persona whose whole job is to reach the world through its MCP tools
	// names the native ones here, and the names are the provider's: this is
	// claude-only, like the rest of the sessionless path.
	DisallowedTools []string

	// OnText receives assistant text deltas as the reply streams. Optional, and
	// opting in turns on the provider's partial messages, which is what emits
	// them. Called from a runtime goroutine, so it must not block.
	OnText func(delta string)

	// OnThought receives each block of the persona's reasoning, "" when the
	// provider sends it encrypted (Claude does). Optional. Called from a runtime
	// goroutine, so it must not block.
	OnThought func(text string)
}

// StartPersonaRuntime starts a sessionless web-only persona: a raw runtime CLI
// session driven through runtime.Manager (for the state machine, watchdog, and
// fullAuto approval pump) but with no agentique sessions row, worktree, project,
// MCP server, or brain recall. claude-only for v1 (the sessionless treatment is
// claude-adapter specific).
func (m *Manager) StartPersonaRuntime(_ context.Context, p PersonaRuntimeParams) (personaRuntime, error) {
	id := p.ID
	if id == "" {
		id = "persona-" + uuid.New().String()
	}
	pr := &sessionlessPersona{id: id, rt: m.rt, onText: p.OnText, onThought: p.OnThought}

	// Serialize the routing handshake under routeMu — see Create. The default
	// connector is claude (only "codex" is registered as an alternate), so
	// hinting "claude" falls through to the default. pop() keeps the capture
	// buffer balanced against concurrent DB-session creates even though a
	// sessionless persona never needs direct CLI access.
	m.routeMu.Lock()
	m.connWrap.hintNext("claude")
	// Detached context: the CLI process lifetime is independent of the request
	// ctx — see the comment in Create.
	rtSess, err := m.rt.Create(context.Background(), runtime.CreateParams{
		SessionID:       id,
		WorkDir:         p.WorkDir,
		Preamble:        p.Preamble,
		Model:           p.Model,
		AutoApprove:     runtime.AutoApproveAll,
		Effort:          resolveEffort(p.Effort),
		MCPConfigs:      p.MCPConfigs,
		DisallowedTools: p.DisallowedTools,
		// Partial messages are what emit the text deltas, and they also stream
		// every other inner API event — so they are on only for a caller that
		// asked to stream.
		PartialMessages: p.OnText != nil,
		SessionOptions: []runtime.SessionOption{
			runtime.WithBroadcast(pr.onEvent),
		},
	})
	if err != nil {
		// Create only errors when Connect fails, and capturingConnector buffers
		// the CLISession only on a successful Connect — so there is nothing to
		// pop here (matching Manager.Create's error path).
		m.routeMu.Unlock()
		return nil, fmt.Errorf("start persona runtime: %w", err)
	}
	m.connWrap.pop() // keep the capture buffer balanced; we don't need direct CLI access
	m.routeMu.Unlock()

	pr.sess = rtSess
	return pr, nil
}
