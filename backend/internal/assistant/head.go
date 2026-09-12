package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// headIdleTimeout is how long the head's subprocess stays up with nothing to
// do.
//
// It stays up at all because a CLI's first connect costs thirty to forty
// seconds, and the operator is waiting on it; it does not stay up forever
// because it is a process holding memory for a conversation nobody is having.
// Longer than a session's idle eviction on purpose: a thread is picked up and
// put down across an afternoon, where a session is worked in.
const headIdleTimeout = 30 * time.Minute

// headTurnBudget bounds one head turn.
//
// Generous, because the head calls verbs and a verb can summarise a session,
// and terminal rather than advisory: a turn that never returns holds the
// conversation's one lock, so every later message waits behind it.
const headTurnBudget = 10 * time.Minute

// headTailMessages is how much of the conversation goes into a fresh head's
// preamble.
//
// The head's context is a CACHE of the conversation, never the memory: the
// store is the record, and a restarted head reads the tail of it rather than
// resuming a transcript. The number is a balance, not a science — enough that
// picking a conversation back up works, little enough that a fresh head is
// fast.
const headTailMessages = 20

// HeadIDPrefix marks the head's own subprocess to anything keyed on a caller's
// identity.
//
// The head reaches agentique's tools over the same MCP endpoint every coding
// session uses, authenticated by a bearer minted for its own id — so that id is
// the only thing separating the two callers, and the separation has to hold in
// both directions: a session must not be able to invoke the assistant's verbs,
// and the head must not be able to invoke a tool that acts on the session it is
// pretending to be. A session id is a UUID and this is not one, which is what
// makes the test total rather than a convention.
//
// Deliberately not the sessionless persona's own "persona-" prefix, which a
// discussion persona also wears: this test must name the head and nothing else.
const HeadIDPrefix = "assistant-head-"

// IsHeadID reports whether an MCP caller is the assistant's head rather than a
// coding session.
func IsHeadID(id string) bool { return strings.HasPrefix(id, HeadIDPrefix) }

// HeadRuntime is one live head: a Claude persona subprocess, driven one turn
// at a time.
//
// Narrow on purpose. This is what internal/session's sessionless persona
// already is, described without naming it, so the assistant can be tested
// against a fake and this package never imports the session pipeline.
type HeadRuntime interface {
	// Query runs one turn and returns the reply text.
	Query(ctx context.Context, prompt string) (string, error)
	// Close stops the subprocess. Idempotent.
	Close() error
}

// HeadParams is what starting a head takes.
//
// No working directory: the head has no project and no worktree, so where its
// subprocess runs is the manager's business, not the assistant's. No
// session id either — it is not a session, it has no row, and a restart is
// expected to lose it.
//
// No tool list either, and for the same reason rather than by omission. The
// head reaches the world through the verb table and nothing else, so the native
// tools its provider's CLI would otherwise carry — a shell, a file writer, a
// web fetcher — are denied where the subprocess is built, by the manager that
// knows which provider it is spawning and what those tools are called. This
// package cannot name them: it is provider-neutral by construction, and a
// containment claim spelled in a neutral vocabulary would be a claim nothing
// enforces.
type HeadParams struct {
	// Preamble is the whole system instruction, composed from the stores.
	Preamble string
	// Model is a family name or "", meaning whatever a new session would get.
	// Never a version or a slug: a new upstream model must not require an
	// agentique release.
	Model string
	// Effort is the reasoning level, or "" for the service default.
	Effort string
	// OnText receives the reply's text as it streams. Optional, and the head
	// works without it — the reply still arrives whole from Query — so a
	// manager that cannot stream is not a broken one.
	OnText func(delta string)
}

// HeadManager starts heads. Implemented in the server over
// session.Manager.StartPersonaRuntime, which is claude-only.
type HeadManager interface {
	StartHead(ctx context.Context, p HeadParams) (HeadRuntime, error)
}

// headState is the one live head and what guards it.
type headState struct {
	// turn serialises turns. One head, one transcript: two surfaces asking at
	// once would interleave into one conversation the CLI cannot untangle.
	turn sync.Mutex

	// mu guards the rest.
	mu      sync.Mutex
	rt      HeadRuntime
	idle    *time.Timer
	surface string
	// inTurn holds the idle eviction off while a turn runs. The timer measures
	// from the last turn's END, so a turn starting inside the last minutes of
	// the window would otherwise be killed mid-flight — and a Query whose
	// subprocess is gone never sees a completion, so it blocks until the whole
	// turn budget expires and the operator gets nothing for ten minutes.
	inTurn bool
	// closed refuses a start after [Service.Close]. A say queued behind a long
	// turn runs on a background context and would otherwise start a subprocess
	// nothing is left to stop.
	closed bool
}

// runHeadTurn runs one turn through the head and returns its reply.
func (s *Service) runHeadTurn(ctx context.Context, surface, prompt string) (string, error) {
	s.head.turn.Lock()
	defer s.head.turn.Unlock()

	// Held off for the whole turn, including the start and the news read before
	// it, and re-armed however this returns.
	s.beginHeadTurn()
	defer s.endHeadTurn()

	rt, err := s.ensureHead(ctx, surface)
	if err != nil {
		return "", err
	}

	// The one push into a turn. Everything else the head knows it asked for:
	// knowledge is pulled through the verbs, and only news is pushed, because
	// pushing a retriever's guess into every turn is what made the brain noise.
	if news := s.headNews(ctx); news != "" {
		prompt = news + "\n\n" + prompt
	}

	turnCtx, cancel := context.WithTimeout(ctx, headTurnBudget)
	defer cancel()

	reply, err := rt.Query(turnCtx, prompt)
	if err != nil {
		// A head that failed a turn is not trustworthy for the next one: the
		// subprocess may be gone, and a restart costs a connect rather than the
		// conversation, which is on disk.
		s.log.Warn("assistant head turn failed", "surface", surface, "error", err)
		if stopErr := s.StopHead(); stopErr != nil {
			s.log.Warn("assistant head not stopped after a failed turn", "error", stopErr)
		}
		return "", fmt.Errorf("head turn: %w", err)
	}

	return strings.TrimSpace(reply), nil
}

// beginHeadTurn marks a turn in flight and stops the idle timer.
func (s *Service) beginHeadTurn() {
	s.head.mu.Lock()
	defer s.head.mu.Unlock()
	s.head.inTurn = true
	if s.head.idle != nil {
		s.head.idle.Stop()
	}
}

// endHeadTurn clears the mark and re-arms the eviction.
//
// It runs on every exit, not only a successful one: a failed turn that did not
// reach [Service.StopHead] would otherwise leave a live head with no timer on
// it at all, which is a subprocess nothing will ever reclaim. Arming is a no-op
// when there is no head, so the failure path that did stop it costs nothing.
func (s *Service) endHeadTurn() {
	s.head.mu.Lock()
	s.head.inTurn = false
	s.head.mu.Unlock()
	s.armHeadIdle()
}

// ensureHead returns the live head, starting one if there is none.
func (s *Service) ensureHead(ctx context.Context, surface string) (HeadRuntime, error) {
	s.head.mu.Lock()
	s.head.surface = surface
	rt, closed := s.head.rt, s.head.closed
	s.head.mu.Unlock()

	if rt != nil {
		return rt, nil
	}
	if closed {
		return nil, errors.New("assistant: the assistant is shutting down, so this turn is not starting")
	}
	if s.heads == nil {
		return nil, errors.New("assistant: no head is wired, so there is nobody to answer")
	}

	preamble, looked := s.headPreamble(ctx)

	started, err := s.heads.StartHead(ctx, HeadParams{
		Preamble: preamble,
		Model:    s.headModel(ctx),
		OnText:   s.onHeadText,
	})
	if err != nil {
		// Nothing is stamped seen: the news went into a preamble no subprocess
		// ever read, and a start that fails on a missing CLI must not consume
		// what the next one would have said.
		return nil, fmt.Errorf("start head: %w", err)
	}

	s.head.mu.Lock()
	// A concurrent start cannot happen while the turn lock is held, but a stop
	// can, and losing the race must not leak a subprocess.
	if s.head.rt != nil {
		s.head.mu.Unlock()
		_ = started.Close()
		return s.head.rt, nil
	}
	s.head.rt = started
	s.head.mu.Unlock()

	// Only now: the head exists and is holding the news in its instruction.
	s.markLooked(ctx, SurfaceHead, looked)

	s.log.Info("assistant head started", "surface", surface)
	s.armHeadIdle()
	return started, nil
}

// StopHead stops the head's subprocess. Idempotent, and safe on a service that
// never started one.
//
// A restart is not a pause: the next message starts a fresh head from the same
// stores, and nothing is lost but the turn — the conversation, the journal and
// the follow list are on disk.
func (s *Service) StopHead() error {
	s.head.mu.Lock()
	rt, idle := s.head.rt, s.head.idle
	s.head.rt, s.head.idle = nil, nil
	s.head.mu.Unlock()

	if idle != nil {
		idle.Stop()
	}
	if rt == nil {
		return nil
	}
	if err := rt.Close(); err != nil {
		return fmt.Errorf("stop head: %w", err)
	}
	s.log.Info("assistant head stopped")
	return nil
}

// HeadIsUp reports whether a head subprocess is live.
func (s *Service) HeadIsUp() bool {
	s.head.mu.Lock()
	defer s.head.mu.Unlock()
	return s.head.rt != nil
}

// armHeadIdle starts or extends the idle eviction.
//
// The timer is created on first use rather than in the constructor, so nothing
// about building a Service reaches outside the process — a stray timer in a
// constructor is the same class of mistake as a stray sweep.
func (s *Service) armHeadIdle() {
	s.head.mu.Lock()
	defer s.head.mu.Unlock()

	if s.head.rt == nil {
		return
	}
	if s.head.idle != nil {
		s.head.idle.Reset(headIdleTimeout)
		return
	}
	s.head.idle = time.AfterFunc(headIdleTimeout, s.evictIdleHead)
}

// evictIdleHead is the timer's callback: stop the head, unless it is mid-turn.
//
// The second guard is not redundant with [Service.beginHeadTurn] stopping the
// timer — a timer can already have fired and be waiting on the mutex — and it
// is the one that must not get this wrong: closing the subprocess under a live
// Query leaves that Query waiting on a completion that can never arrive.
func (s *Service) evictIdleHead() {
	s.head.mu.Lock()
	inTurn := s.head.inTurn
	if inTurn && s.head.idle != nil {
		s.head.idle.Reset(headIdleTimeout)
	}
	s.head.mu.Unlock()
	if inTurn {
		return
	}

	if err := s.StopHead(); err != nil {
		s.log.Warn("assistant head not evicted", "error", err)
		return
	}
	s.log.Info("assistant head evicted after idle", "after", headIdleTimeout)
}

// onHeadText is the streaming hook. The reply is stored whole on completion;
// this is only so a thread can show it arriving.
func (s *Service) onHeadText(delta string) {
	if delta == "" {
		return
	}
	s.head.mu.Lock()
	surface := s.head.surface
	s.head.mu.Unlock()

	d := Delta{Surface: surface, Text: delta}
	s.broadcast(EventDelta, d)
	s.deliver(context.Background(), Item{Kind: ItemDelta, Delta: &d})
}

// headModel is the family name the head runs, from the state row.
//
// Empty is the answer whenever nothing has been chosen, and empty means
// "whatever a new session would get" — no family is hardcoded here, on the
// model-catalog rule.
func (s *Service) headModel(ctx context.Context) string {
	state, err := s.store.GetAssistantState(ctx)
	if err != nil {
		return ""
	}
	return state.Model
}

// headNews is the head's own SinceLast, rendered for a prompt.
//
// The head is not a surface, but "what has happened since I last looked" is
// the same question, so it uses the same marks under its own key. A turn with
// no news carries none of this.
//
// It reads first and stamps only what it actually rendered into the turn. The
// gate is the LOOK, not its journal half: a purely conversational call mirrors
// two messages and writes no journal row, and gating on the journal dropped the
// whole call — after the marks had advanced, so the head could never see it
// again.
func (s *Service) headNews(ctx context.Context) string {
	update, err := s.unseen(ctx, SurfaceHead)
	if err != nil {
		s.log.Warn("assistant head news unavailable", "error", err)
		return ""
	}
	if len(update.Journal) == 0 && len(update.Messages) == 0 {
		return ""
	}
	news := renderNews(update)
	s.markLooked(ctx, SurfaceHead, update)
	return news
}

// headPreamble composes a fresh head's whole system instruction from the
// stores: what it is, what it may do, what is going on, what it has missed,
// and the tail of the conversation it is joining.
//
// It answers the look it read as well as the text, because the look is not
// consumed until the head is up: [Service.ensureHead] stamps it once
// StartHead has returned. A start that fails on a missing CLI or a connect
// timeout would otherwise have marked the news seen for a head that never
// existed, and the next successful start would read none — the same failure
// the call's greeting reads its news at greeting time to avoid.
func (s *Service) headPreamble(ctx context.Context) (preamble string, looked Update) {
	brief := HeadBriefing{Verbs: s.Verbs(), HasMemory: s.HasMemory()}

	if s.dir != nil {
		brief.Orientation = s.dir.Orientation(ctx)
	}

	// The pinned set and the index, and nothing else from the store: every other
	// fact is behind the recall verb. Both halves degrade to nothing rather than
	// failing the start, and the third return is what says which of "empty" and
	// "unreadable" happened — the preamble must not print the first for the second.
	brief.Pinned, brief.Index, brief.MemoryUnread = s.memoryBriefing(ctx)

	update, err := s.unseen(ctx, SurfaceHead)
	if err != nil {
		s.log.Warn("assistant head news unavailable", "error", err)
	} else {
		brief.News = renderNews(update)
	}

	page, err := s.History(ctx, "", headTailMessages)
	if err != nil {
		// A head with no history is a head that has forgotten the conversation,
		// which is worse than a slow one but not worse than no assistant at all.
		s.log.Warn("assistant conversation tail unavailable", "error", err)
	}
	brief.Tail = page.Messages

	return HeadInstruction(brief), update
}
