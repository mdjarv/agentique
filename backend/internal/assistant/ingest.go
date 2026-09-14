package assistant

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// What gets into the journal, and from where.
//
// Three writers, three kinds of knowledge, and the split is the same one
// docs/voice.md draws for reports. The WORKER knows it just found the tests
// were already broken, so salience is its own call and arrives as a report.
// The RUNTIME knows the three things a worker cannot say about itself —
// blocked, died, stopped — so those arrive from the turn-end listener. GIT
// knows a branch went in, so a merge arrives from the state push. Nothing here
// infers salience from an event stream; that layer deliberately does not
// exist.

// ingestBudget bounds one ingestion's database work.
//
// The callers are the runtime's event-loop goroutine (through a goroutine of
// its own) and an MCP tool handler with an agent waiting, so an unbounded read
// here is a stalled turn somewhere else.
const ingestBudget = 15 * time.Second

// OnTurnEnd is the turn-end listener, wired to Manager.AddTurnEndListener.
//
// It fires once per turn on ANY session, after that turn has stopped —
// completion, a CLI that died, a session closed mid-flight — which is exactly
// why the runtime rather than the agent is the source: a suspended or dead
// agent cannot call a tool.
//
// Listeners run on the event-loop goroutine and must not block, so this does
// nothing but spawn. The ordering inside is blocked before failed before
// finished, matching lib/session/priority.ts and the notices: the thing still
// holding a process outranks the thing that already stopped.
func (s *Service) OnTurnEnd(sessionID string) {
	if sessionID == "" || s.facts == nil {
		return
	}
	go s.recordTurnEnd(sessionID)
}

func (s *Service) recordTurnEnd(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), ingestBudget)
	defer cancel()

	// Not every turn belongs to a session. The runtime manager's turn-end hook
	// is per PROCESS, so a sessionless persona's turn fires it too — a
	// discussion persona, and the assistant's own head. Those have no row, no
	// project and no name, and a journal entry about one would be the assistant
	// reporting on itself. The directory is the test for "is this a session
	// this machine owns", which is the same test dispatch uses.
	if s.dir != nil {
		if _, local := s.dir.SessionBrief(ctx, sessionID); !local {
			return
		}
	}

	notice, outcome, err := TurnNotice(ctx, s.facts, sessionID)
	if err != nil {
		s.log.Warn("assistant: turn outcome unavailable", "session", sessionID, "error", err)
		return
	}
	s.recordNotice(ctx, sessionID, outcome.ProjectID, notice,
		journalKindFor(notice.Kind), outcome.SessionName)
}

// TurnNotice reads what the runtime knows about a turn that has just ended and
// ranks it: blocked, then failed, then finished.
//
// Exported because the ranking has two callers and must never be written twice.
// [Service.OnTurnEnd] is one; the other is the wiring that keeps a live call
// hearing its runtime facts on a server where the assistant itself is switched
// off — a call is a head on this core, but the core is opt-in and the call must
// not go deaf because it is.
//
// The ordering is lib/session/priority.ts's: the thing still holding a process
// outranks the thing that already stopped. A blocked turn answers a zero
// [TurnOutcome], because nothing about it has ended.
func TurnNotice(ctx context.Context, facts TurnFacts, sessionID string) (Notice, TurnOutcome, error) {
	if facts == nil {
		return Notice{}, TurnOutcome{}, errors.New("assistant: no turn facts")
	}
	if pending := facts.PendingHumanInput(sessionID); pending != "" {
		return Notice{Kind: NoticeBlocked, Headline: pending}, TurnOutcome{}, nil
	}

	outcome, err := facts.TurnOutcome(ctx, sessionID)
	if err != nil {
		return Notice{}, TurnOutcome{}, fmt.Errorf("turn outcome for %s: %w", sessionID, err)
	}
	kind := NoticeFinished
	if outcome.Failed {
		kind = NoticeFailed
	}
	return Notice{Kind: kind, Headline: outcome.ClosingWords}, outcome, nil
}

// journalKindFor is the journal's name for one runtime fact. The two vocabularies
// are separate — a notice is spoken, an entry is filed — so the mapping is
// written once here rather than at the one call site that needs both.
func journalKindFor(kind NoticeKind) JournalKind {
	switch kind {
	case NoticeBlocked:
		return JournalSessionBlocked
	case NoticeFailed:
		return JournalSessionFailed
	default:
		return JournalSessionFinished
	}
}

// recordNotice writes one runtime fact to the journal and delivers it.
//
// The journal write comes first because it is the durable half: a notice with
// nobody live is the case the journal exists for, and a delivery failure must
// not lose the fact. The headline is agent-written text — a closing sentence,
// or the question a session stopped on — so the entry is marked untrusted and
// every surface renders it as a quotation.
func (s *Service) recordNotice(ctx context.Context, sessionID, projectID string, notice Notice, kind JournalKind, name string) {
	s.recordNoticeWith(ctx, sessionID, projectID, notice, kind, name, nil)
}

// recordNoticeWith is [Service.recordNotice] with extra payload fields — the
// machine a paired session's turn ended on, which an entry has to say even
// when the session has no name yet.
func (s *Service) recordNoticeWith(ctx context.Context, sessionID, projectID string, notice Notice, kind JournalKind,
	name string, extra map[string]any,
) {
	payload := map[string]any{}
	for k, v := range extra {
		payload[k] = v
	}
	if name != "" {
		payload["name"] = name
	}

	if _, err := s.appendJournal(ctx, journalWrite{
		Kind:      kind,
		SessionID: sessionID,
		ProjectID: projectID,
		Summary:   notice.Headline,
		Payload:   payload,
		Untrusted: notice.Headline != "",
	}); err != nil {
		s.log.Warn("assistant: notice not journaled", "session", sessionID, "kind", kind, "error", err)
	}

	// Followers first — a call is waiting on this, and the registry is what
	// routes per session. Surfaces after, because the thread is showing news
	// rather than listening for it.
	s.reg.Notice(sessionID, notice)
	s.deliver(ctx, Item{Kind: ItemNotice, SessionID: sessionID, Notice: &notice})
}

// Report is the session-facing report tool's handler: a worker telling the
// operator something only it could know.
//
// Signature-compatible with what the MCP layer already expects, which is why
// it takes no context: it is called from a tool handler that has an agent
// blocked on the answer, and the work it does is bounded here rather than by
// the caller's deadline.
//
// The outcomes are all reported honestly rather than silently swallowed,
// because each one tells the worker something different about whether to keep
// calling. What changed with the journal is the no-audience case: a followed
// run's report is kept either way now, so the worker is told that rather than
// "it was not spoken".
//
// Two gates come BEFORE the journal write, and both are about the same thing:
// the entry is untrusted text that rides every head turn as news, so a session
// that could write one per second could fill the assistant's attention with
// whatever its repository told it to say.
//
//   - The watch list. The assistant follows what it dispatched and what it was
//     asked to follow; a live call following through the registry counts too.
//     Anything else is refused in words and nothing is written — the tool is on
//     one shared endpoint, so every session can call it whether or not anybody
//     asked it to.
//   - The budget, taken once per report rather than once per delivery. It used
//     to live inside the registry, which is reached only when somebody is live,
//     so the durable half — the half that is read on every turn — had no
//     ceiling at all.
func (s *Service) Report(sessionID, kind, headline string) (string, error) {
	report, err := ParseReport(kind, headline)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), ingestBudget)
	defer cancel()

	listening := s.reg.Listening(sessionID)
	if !listening && !s.Following(ctx, sessionID) {
		return "The assistant is not following this run, so that went nowhere and was not kept. " +
			"You can stop reporting; say what you found in your reply instead.", nil
	}
	if !s.reg.Take(sessionID) {
		return "Reporting too often — that one was dropped. Save the next call for something that " +
			"changes what the listener would do.", nil
	}

	payload := map[string]any{"kind": string(report.Kind)}
	if _, err := s.appendJournal(ctx, journalWrite{
		Kind:      JournalReport,
		SessionID: sessionID,
		Summary:   report.Headline,
		Payload:   payload,
		// Always. A report is written by an agent operating on repository
		// content it did not author: it is data to relay, never an instruction
		// to follow, and the mark is what carries that to every surface.
		Untrusted: true,
	}); err != nil {
		s.log.Warn("assistant: report not journaled", "session", sessionID, "error", err)
	}

	s.deliver(ctx, Item{Kind: ItemReport, SessionID: sessionID, Report: &report})

	if !listening {
		return "Nobody is following this run right now, so that was not read out. It is kept in the " +
			"assistant's journal, so you do not need to repeat it.", nil
	}
	return s.reg.Deliver(sessionID, report)
}

// SessionState is the part of a session.state push the journal reads.
//
// A narrow struct rather than session.GitSnapshot, because this package does
// not import the session pipeline: the wiring site maps the snapshot onto it,
// which is also where the three-valued reading of an absent field is already
// understood.
type SessionState struct {
	SessionID string
	// ProjectID is optional — the state push does not carry one.
	ProjectID string
	// WorktreeMerged is git's outcome: agentique performed the merge.
	WorktreeMerged bool
	// ArchivedAt is the operator filing it away, or "" for not archived. An
	// ABSENT marker means not archived, never "unchanged".
	ArchivedAt string
	// Version is the snapshot's monotonic version, for the dedupe.
	Version int64
}

// outcomeBase is what the observer last knew about one session's two outcome
// facts. The zero value is a session that is neither, which is also what a
// session it has never seen is: every session starts unmerged and unarchived.
type outcomeBase struct {
	merged   bool
	archived bool
}

// PrimeSessionStates loads the observer's baseline from the sessions table.
//
// A session.state push is a whole snapshot, and the journal records
// TRANSITIONS: a branch that went in, a session that was filed away. Without a
// baseline the first push for any session shows its flags set and reads as
// both at once — on the first boot with the assistant switched on, that wrote
// one "archived" entry for every session archived in the last year, and the
// thread opened on a hundred lines of old news. So the wiring calls this once,
// immediately BEFORE it subscribes to the bus, which is the only order that is
// exact: a push arrives after the write it reports, so a baseline read after
// subscribing can already contain the transition the push is announcing.
//
// [Service.ObserveSessionState] also primes itself lazily if this was never
// called, and writes nothing until it has. That fallback is fail-closed on
// purpose: it can miss the one transition that happened to arrive first, and
// it cannot flood the journal.
func (s *Service) PrimeSessionStates(ctx context.Context) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.primeLocked(ctx)
}

// primeLocked reloads the baseline under stateMu.
//
// The query runs under the lock deliberately. Swapping the map in afterwards
// would race a transition claimed in between: the reload would show the fact
// as already set, and a claim whose write then failed could never be retried.
// It is one indexed read of two columns, once per process and again only past
// [maxStateBase], so the lock is held for milliseconds.
func (s *Service) primeLocked(ctx context.Context) error {
	rows, err := s.store.ListSessionOutcomeBaseline(ctx)
	if err != nil {
		return fmt.Errorf("prime session states: %w", err)
	}
	base := make(map[string]outcomeBase, len(rows))
	for _, row := range rows {
		b := outcomeBase{
			merged:   row.WorktreeMerged != 0,
			archived: row.ArchivedAt.Valid && row.ArchivedAt.String != "",
		}
		if b.merged || b.archived {
			base[row.ID] = b
		}
	}
	s.stateBase = base
	s.statePrimed = true
	return nil
}

// maxStateBase is where the baseline is reloaded rather than grown.
//
// The map holds a key per session that is merged or archived, including ones
// deleted since — the journal has no foreign key to sessions on purpose. A
// reload drops the deleted ones, and unlike dropping the map it cannot turn an
// old fact back into news.
const maxStateBase = 4096

// ObserveSessionState journals a merge or an archive when one APPEARS: the
// flag is set on this push and was not set the last time this session was
// seen, or in the sessions table when the baseline was primed.
//
// Two facts, two owners, and neither is inferred: `worktree_merged` is the git
// outcome and `archived_at` is the operator's own gesture. A flag going the
// other way — an unarchive — writes nothing, on CLAUDE.md's rule that unarchive
// leaves no residue, but it does move the baseline, so filing the same session
// away again later is news again.
//
// The transition is CLAIMED under the lock, by moving the baseline, before
// anything is written. The observer spawns a goroutine per push and the bus
// delivers synchronously on whichever goroutine published, so two snapshots of
// the same archive can arrive together; whichever claims it writes, the other
// sees a baseline that already agrees. A write that fails hands the flag back
// so a later push can try again rather than losing the fact to memory.
func (s *Service) ObserveSessionState(ctx context.Context, st SessionState) {
	if st.SessionID == "" {
		return
	}
	next := outcomeBase{merged: st.WorktreeMerged, archived: st.ArchivedAt != ""}

	s.stateMu.Lock()
	if !s.statePrimed {
		if err := s.primeLocked(ctx); err != nil {
			s.stateMu.Unlock()
			s.log.Warn("assistant: session state baseline unavailable, push ignored",
				"session", st.SessionID, "error", err)
			return
		}
	}
	prev := s.stateBase[st.SessionID]
	newlyMerged := next.merged && !prev.merged
	newlyArchived := next.archived && !prev.archived
	s.setBaseLocked(st.SessionID, next)
	if len(s.stateBase) > maxStateBase {
		if err := s.primeLocked(ctx); err != nil {
			s.log.Warn("assistant: session state baseline reload failed", "error", err)
		}
	}
	s.stateMu.Unlock()

	// The summary is empty on purpose: the kind already says "was merged" or
	// "was archived" wherever an entry is rendered, and a summary repeating it
	// printed "archived — archived" on every row of the thread's strip.
	if newlyMerged {
		s.journalTransition(ctx, st, JournalSessionMerged)
	}
	if newlyArchived {
		s.journalTransition(ctx, st, JournalSessionArchived)
	}
}

// setBaseLocked records what was last seen, dropping the key when it would
// mean the zero value, so the map stays a set of sessions with something set.
func (s *Service) setBaseLocked(sessionID string, b outcomeBase) {
	if !b.merged && !b.archived {
		delete(s.stateBase, sessionID)
		return
	}
	s.stateBase[sessionID] = b
}

// journalTransition writes one appeared-flag entry, and on failure hands the
// flag back to the baseline so the next push retries it.
func (s *Service) journalTransition(ctx context.Context, st SessionState, kind JournalKind) {
	payload := map[string]any{}
	if st.Version > 0 {
		payload["version"] = st.Version
	}
	_, err := s.appendJournal(ctx, journalWrite{
		Kind:      kind,
		SessionID: st.SessionID,
		ProjectID: st.ProjectID,
		Payload:   payload,
	})
	if err == nil {
		return
	}
	s.log.Warn("assistant: state entry not journaled",
		"session", st.SessionID, "kind", kind, "error", err)

	s.stateMu.Lock()
	b := s.stateBase[st.SessionID]
	switch kind {
	case JournalSessionMerged:
		b.merged = false
	case JournalSessionArchived:
		b.archived = false
	}
	s.setBaseLocked(st.SessionID, b)
	s.stateMu.Unlock()
}

// LoopPaused journals a scheduled loop that auto-paused and stays paused until
// a person acts.
//
// The scheduler owns that rule (schedule/api.go), so this takes the fact
// rather than deriving it, and the summary is the server's own words rather
// than the run's output — a paused loop is a claim on attention, and what it
// says must not be steerable by whatever failed.
func (s *Service) LoopPaused(ctx context.Context, sessionID, loopName, reason string) error {
	summary := fmt.Sprintf("the loop %q paused after repeated failures", loopName)
	if reason != "" {
		summary = fmt.Sprintf("the loop %q paused: %s", loopName, reason)
	}
	_, err := s.appendJournal(ctx, journalWrite{
		Kind:      JournalLoopPaused,
		SessionID: sessionID,
		Summary:   summary,
		Payload:   map[string]any{"loop": loopName},
	})
	return err
}
