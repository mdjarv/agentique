package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/google/uuid"
	"github.com/mdjarv/agentique/backend/internal/procctl"
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
//
// Nobody is at a screen for a persona, so a turn waits on the model and on its
// tools and on nothing else. A question the model asks through the CLI, or an
// approval that parks, is refused the moment it is raised (refusePending), and a
// CLI that ends mid-turn ends the Query with it. Either one left alone waits out
// the caller's whole budget on something that cannot arrive: on 2026-09-15 the
// assistant's head called AskUserQuestion and spent its ten minutes that way.
type sessionlessPersona struct {
	id string
	rt *runtime.Manager

	// onText, when set, receives assistant text deltas as they stream. Fixed at
	// construction and never written afterwards, so it needs no lock.
	onText func(string)

	// onThought, when set, receives each block of the persona's own reasoning,
	// "" when the provider withheld the text. Fixed at construction, like onText.
	onThought func(string)

	mu sync.Mutex
	// sess is set once the runtime has created the session. Guarded by mu,
	// because the runtime can broadcast before Create has returned it.
	sess *runtime.Session
	// done is the per-turn delivery channel, swapped in by Query and read by
	// onEvent. Guarded by mu.
	done chan personaTurnEnd
	// tools is what the CLI said it offers, from its init event, and inited
	// whether that event has arrived. Guarded by mu.
	tools  []string
	inited bool
}

// personaTurnEnd is how one turn ended: its reply, or why there is none.
type personaTurnEnd struct {
	text string
	err  error
}

// personaNobodyToAsk is what a persona's CLI is told when it asks a person for
// something. It reaches the model as the tool's result, so it says what to do
// instead.
const personaNobodyToAsk = "Nobody can answer this here: there is no screen and no one to approve " +
	"or choose. Ask your question in your reply instead, as plain text, and stop there."

// onEvent is the runtime broadcast hook. It forwards the turn's end to the
// in-flight Query (if any), text deltas to onText and the persona's own
// thinking blocks to onThought when a caller asked for them, and refuses
// anything that parks on a person. Everything else is ignored — a discussion
// contribution is mirrored to the channel timeline once, on completion, exactly
// like recordContribution does today. Called synchronously from a runtime
// goroutine, so it must not block.
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

	switch ev := e.(type) {
	case runtime.SessionInitEvent:
		p.mu.Lock()
		p.tools = append([]string(nil), ev.Tools...)
		p.inited = true
		p.mu.Unlock()
		// What a persona can call is a claim its caller makes; the log is where
		// it can be checked afterwards.
		slog.Info("persona runtime started", "persona", p.id, "tools", ev.Tools)
	case runtime.PendingChangeEvent:
		if ev.HasQuestion || ev.HasApproval {
			// SubmitAnswer broadcasts, on this same hook: answering off the
			// broadcasting goroutine is what keeps that from re-entering it.
			go p.refusePending()
		}
	case runtime.StateChangeEvent:
		if personaEnded(ev.To) {
			p.endTurn(personaTurnEnd{err: fmt.Errorf("the persona's CLI %s before its turn completed", ev.To)})
		}
	case runtime.TurnCompletedEvent:
		p.endTurn(personaTurnEnd{text: ev.Text})
	}
}

// personaEnded reports whether a runtime state means no turn can complete.
func personaEnded(st runtime.State) bool {
	return st == runtime.StateFailed || st == runtime.StateDone || st == runtime.StateStopped
}

// endTurn delivers a turn's end to the Query waiting on it, if any. The first
// end wins: a completion followed by the CLI exiting is a completed turn.
func (p *sessionlessPersona) endTurn(end personaTurnEnd) {
	p.mu.Lock()
	ch := p.done
	p.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- end:
	default:
	}
}

// refusePending answers, with a no, everything the CLI is waiting on a person
// for.
//
// A question is cancelled rather than answered: an answer would be read as the
// person's choice, and nobody made one. An approval cannot park on a fullAuto
// persona today, and is refused all the same, because the rule is that nothing
// on a persona waits for a person — not that one kind of thing does not.
func (p *sessionlessPersona) refusePending() {
	p.mu.Lock()
	sess := p.sess
	p.mu.Unlock()
	if sess == nil {
		return
	}
	for {
		approval, question := sess.PendingState()
		if approval == nil && question == nil {
			return
		}
		if question != nil {
			slog.Info("persona question refused: nobody can answer on a persona", "persona", p.id)
			if err := sess.SubmitAnswer(question.ID, nil); err != nil && !errors.Is(err, runtime.ErrPendingNotFound) {
				slog.Warn("persona question not refused", "persona", p.id, "error", err)
				return
			}
		}
		if approval != nil {
			slog.Info("persona approval refused: nobody can approve on a persona", "persona", p.id,
				"tool", approval.ToolName)
			err := sess.SubmitApproval(approval.ID, runtime.Decision{Allow: false, DenyMessage: personaNobodyToAsk})
			if err != nil && !errors.Is(err, runtime.ErrPendingNotFound) {
				slog.Warn("persona approval not refused", "persona", p.id, "error", err)
				return
			}
		}
	}
}

// offeredTools is the tool list the CLI reported at init, and whether it has
// reported one yet.
func (p *sessionlessPersona) offeredTools() ([]string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.tools...), p.inited
}

func (p *sessionlessPersona) Query(ctx context.Context, prompt string) (string, error) {
	done := make(chan personaTurnEnd, 1)
	p.mu.Lock()
	p.done = done
	sess := p.sess
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.done = nil
		p.mu.Unlock()
	}()
	if sess == nil {
		return "", errors.New("query: the persona has no runtime session")
	}

	if err := sess.Query(ctx, prompt); err != nil {
		return "", fmt.Errorf("query: %w", err)
	}

	select {
	case end := <-done:
		if end.err != nil {
			return "", end.err
		}
		return strings.TrimSpace(end.text), nil
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

	// Contained spawns the persona with MCPConfigs as its whole tool set: no
	// provider-native tool, and no MCP server from the user's own
	// configuration. It goes through the connector registered with
	// [Manager.SetContainedConnector], and a start is refused when there is
	// none rather than downgraded to the ordinary one.
	//
	// It matters because a persona runs fullAuto — the approval pump
	// auto-allows everything, by design, since there is no screen to ask — so
	// the tools it holds are the tools it can use without anybody agreeing. It
	// is an allowlist, never a list of names to deny: the CLI adds tools between
	// releases, and a deny list written against one release is out of date on
	// the next. The head's said Task and Bash while the CLI was offering it
	// Agent, Workflow, SendMessage, RemoteTrigger, Artifact, the user's Google
	// Drive connector and AskUserQuestion, which is the one that hung a turn.
	//
	// A discussion persona is not contained: reading the web is its job, and it
	// is started by the operator for a conversation they are watching.
	Contained bool

	// OnText receives assistant text deltas as the reply streams. Optional, and
	// opting in turns on the provider's partial messages, which is what emits
	// them. Called from a runtime goroutine, so it must not block.
	OnText func(delta string)

	// OnThought receives each block of the persona's reasoning, "" when the
	// provider sends it encrypted (Claude does). Optional. Called from a runtime
	// goroutine, so it must not block.
	OnThought func(text string)
}

// withReaperMarker returns preamble carrying procctl.CLIProcessMarker, adding
// one sentence at the front when it does not already.
//
// The orphan reaper recognises an agentique CLI by that marker in its
// --append-system-prompt value and by nothing else, so a persona whose caller
// wrote its own preamble was invisible to it: the assistant's head never
// carried the marker, and a head orphaned by a server crash was never reaped.
// The manager owns the process lifecycle, so the guarantee lives here rather
// than in every caller's prompt.
func withReaperMarker(preamble string) string {
	if strings.Contains(preamble, procctl.CLIProcessMarker) {
		return preamble
	}
	line := "You are " + procctl.CLIProcessMarker + "."
	if preamble == "" {
		return line
	}
	return line + "\n\n" + preamble
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

	// Checked before anything is spawned: a caller asking for containment is
	// counting on it, so no contained route means no persona.
	if p.Contained && !m.connWrap.hasContained() {
		return nil, fmt.Errorf("start persona runtime: %w", errNoContainedConnector)
	}

	// Serialize the routing handshake under routeMu — see Create. The default
	// connector is claude (only "codex" is registered as an alternate), so
	// hinting "claude" falls through to the default. pop() keeps the capture
	// buffer balanced against concurrent DB-session creates even though a
	// sessionless persona never needs direct CLI access.
	m.routeMu.Lock()
	if p.Contained {
		m.connWrap.hintContained()
	} else {
		m.connWrap.hintNext("claude")
	}
	// Detached context: the CLI process lifetime is independent of the request
	// ctx — see the comment in Create.
	rtSess, err := m.rt.Create(context.Background(), runtime.CreateParams{
		SessionID:   id,
		WorkDir:     p.WorkDir,
		Preamble:    withReaperMarker(p.Preamble),
		Model:       p.Model,
		AutoApprove: runtime.AutoApproveAll,
		Effort:      resolveEffort(p.Effort),
		MCPConfigs:  p.MCPConfigs,
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

	pr.mu.Lock()
	pr.sess = rtSess
	pr.mu.Unlock()
	return pr, nil
}
