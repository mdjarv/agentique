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
	live, err := d.svc.ensureLive(ctx, d.sessionID)
	if err != nil {
		return "", fmt.Errorf("ensure live: %w", err)
	}

	done := make(chan string, 1)
	live.SetTurnCompleteHook(func(tc runtime.TurnCompletedEvent) {
		select {
		case done <- tc.Text:
		default:
		}
	})
	defer live.SetTurnCompleteHook(nil)

	if err := d.svc.QuerySession(ctx, d.sessionID, prompt, nil); err != nil {
		return "", fmt.Errorf("query: %w", err)
	}

	select {
	case text := <-done:
		return strings.TrimSpace(text), nil
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

	// done is the per-turn delivery channel for the turn-complete event, swapped
	// in by Query and read by onEvent. Guarded by mu.
	mu   sync.Mutex
	done chan runtime.TurnCompletedEvent
}

// onEvent is the runtime broadcast hook. It forwards only the turn-complete event
// to the in-flight Query (if any); everything else (partials, tool events, state
// changes) is ignored — a discussion contribution is mirrored to the channel
// timeline once, on completion, exactly like recordContribution does today. Called
// synchronously from a runtime goroutine, so it must not block.
func (p *sessionlessPersona) onEvent(_ context.Context, e runtime.Event) {
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
}

// StartPersonaRuntime starts a sessionless web-only persona: a raw runtime CLI
// session driven through runtime.Manager (for the state machine, watchdog, and
// fullAuto approval pump) but with no agentique sessions row, worktree, project,
// MCP server, or brain recall. claude-only for v1 (the sessionless treatment is
// claude-adapter specific).
func (m *Manager) StartPersonaRuntime(_ context.Context, p PersonaRuntimeParams) (personaRuntime, error) {
	id := "persona-" + uuid.New().String()
	pr := &sessionlessPersona{id: id, rt: m.rt}

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
		SessionID:   id,
		WorkDir:     p.WorkDir,
		Preamble:    p.Preamble,
		Model:       p.Model,
		AutoApprove: runtime.AutoApproveAll,
		Effort:      resolveEffort(p.Effort),
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
