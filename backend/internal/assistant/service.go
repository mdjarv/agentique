package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/allbin/agentkit/eventbus"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/usage"
)

// Push types, all on the assistant.* prefix and all on the GLOBAL topic: the
// conversation is project-less, so there is no topic to scope them to and no
// new routing to add — project-less channels already fan out globally.
const (
	// EventMessage carries a stored [Message].
	EventMessage = "assistant.message"
	// EventDelta carries a [Delta]: the head's reply in progress.
	EventDelta = "assistant.delta"
	// EventStep carries a [StepPush]: one thing the head did during a turn,
	// started or settled.
	EventStep = "assistant.step"
	// EventJournal carries a new [JournalEntry].
	EventJournal = "assistant.journal"
	// EventProposal carries a [Proposal] on create and on decide. It is
	// declared in proposals.go, beside the type it carries.
)

// Page sizes. Every read is bounded, because the journal and the conversation
// only grow and a surface asking for "everything" is asking for a machine's
// whole history.
const (
	// maxJournalPage bounds one journal read.
	maxJournalPage = 200
	// maxHistoryPage bounds one page of the conversation.
	maxHistoryPage = 200
	// maxSinceLastJournal bounds the news one look answers with. A strip and a
	// greeting both read this; past a few dozen entries neither is readable,
	// and the digest is the surface for the rest.
	maxSinceLastJournal = 50
	// maxSinceLastMessages bounds the conversation half of a look.
	maxSinceLastMessages = 50
)

// Update is what a surface has missed: the journal since it last looked, and
// what was said in the conversation since then.
//
// This is what a call's greeting reads and what the thread pins at its top as
// recent updates. It is the ONE push into a head's turn, and it is journal
// rather than brain: news is news, where knowledge is pulled.
type Update struct {
	// Journal is oldest first, so it reads as a sequence of events.
	Journal []JournalEntry `json:"journal,omitempty"`
	// Messages is oldest first, for the same reason.
	Messages []Message `json:"messages,omitempty"`
	// Since is the mark this look was measured against, or "" for a surface
	// that had never looked before.
	Since string `json:"since,omitempty"`
	// LookedAt is the mark this look leaves behind.
	LookedAt string `json:"lookedAt,omitempty"`
}

// Allowances is what is left of the subscription windows.
//
// One method, satisfied by *usage.Collector as it stands: the collector answers
// from cache and never touches the network, which is what makes it safe to ask
// inside a verb. Costs are not part of the answer and never will be.
type Allowances interface {
	Document(ctx context.Context) usage.Document
}

// TurnFacts answers what the runtime knows about a turn that has just ended.
//
// The three things here are exactly the three a working agent CANNOT report
// about itself — it is blocked, it died, it stopped — which is why they come
// from the runtime rather than from a tool call. The interface exists so this
// package does not import internal/session; the server implements it over
// session.Service, the same code the voice watcher ran.
type TurnFacts interface {
	// PendingHumanInput is what a session is stopped waiting for, or "".
	PendingHumanInput(sessionID string) string
	// TurnOutcome describes the turn that just ended.
	TurnOutcome(ctx context.Context, sessionID string) (TurnOutcome, error)
}

// TurnOutcome is one finished turn, as much of it as the journal records.
type TurnOutcome struct {
	// Failed says the session ended this turn in a failed state.
	Failed bool
	// ProjectID places the session, for a journal entry that names its
	// subjects.
	ProjectID string
	// SessionName is what to call it. Empty is fine; the entry falls back to
	// the id.
	SessionName string
	// ClosingWords is the turn's last assistant text, already clamped. Empty
	// is a fine answer: a run that ended without saying anything should not
	// have words invented for it.
	ClosingWords string
}

// Service is the assistant. One per server.
//
// Everything it needs from the rest of the server arrives through a narrow
// interface, and every one of them is optional: a service with no directory
// answers vaguely rather than failing, a service with no head cannot be talked
// to but still journals, a service with no dispatcher refuses to dispatch in
// words. That is the same rule [Directory] has always had, applied to the
// whole constructor — a half-wired assistant must degrade, because the
// alternative is a server that will not boot.
type Service struct {
	store Store
	dir   Directory
	disp  Dispatcher
	heads HeadManager
	allow Allowances
	facts TurnFacts
	mem   Memory
	// memoryBudget bounds the memory briefing; [memoryBriefingBudget] unless a
	// test shortens it.
	memoryBudget time.Duration
	actions      Actions
	triager      Triager
	// summarizer folds a day of the journal into one sentence. Nil means the
	// journal is not folded at all — see [Service.Compact]: deleting rows nothing
	// can account for is losing them.
	summarizer Summarizer
	reg        *Registry
	bus        eventbus.Broadcaster
	log        *slog.Logger
	now        func() time.Time

	// digestAt is the local wall-clock time the timed digest posts, or the zero
	// value for no timed digest. Configuration rather than a constant, and
	// parsed before it gets here: an unparsable one is a boot warning, never a
	// tick that guesses.
	digestAt DigestTime
	// beat guards the heartbeat's ticks against each other.
	beat heartbeatState

	// The verb table, built once in New. Immutable afterwards: nothing outside
	// the table can be called, and the table cannot grow at runtime.
	verbs  []Verb
	byName map[string]*Verb

	surfaces *surfaceSet

	// convMu guards the lazy creation of the conversation channel, so two
	// simultaneous first messages cannot create two conversations.
	convMu    sync.Mutex
	channelID string

	// compactMu serialises [Service.Compact], on decideMu's argument and for a
	// heavier reason: the fold is a check-then-act across a model call, and it
	// has three ways in (the heartbeat's daily trigger, the `compact_journal`
	// verb, the `assistant.compact` op). Two passes over one day would both see
	// an unfolded day, both pay for a summary and leave the day with two. It is
	// TAKEN rather than waited on — a second caller is told a pass is running
	// rather than held for five minutes.
	compactMu sync.Mutex

	// decideMu serialises [Service.Decide]. The status guard in SQL protects
	// the ROW, not the executing: two accepts arriving together would both read
	// `open` and both merge. Decisions are rare and the socket op runs off the
	// dispatch loop, so serialising them costs nothing anybody can feel.
	decideMu sync.Mutex

	// stateBase is what the session.state observer last knew about each
	// session's two outcome facts, keyed by session id and holding only the
	// sessions where at least one is set — false/false is what an absent key
	// means, and it is also what a brand-new session is. A push is journaled
	// when a flag flips against this, never because it is set: a snapshot is a
	// state, not a transition. statePrimed says the map has been loaded from
	// the sessions table at least once; until it has, the observer writes
	// nothing, because an empty baseline reads every archived session as news.
	stateMu     sync.Mutex
	stateBase   map[string]outcomeBase
	statePrimed bool

	head headState
}

// Option configures a [Service]. Functional options because every
// collaborator is optional and there are nine of them; a positional
// constructor would be one transposition away from handing the assistant its
// logger as its directory.
type Option func(*Service)

// WithDirectory gives the assistant its reads: orientation, sessions,
// projects, summaries and session creation. Nil is valid and means the
// assistant can talk about nothing but what it is told.
func WithDirectory(d Directory) Option { return func(s *Service) { s.dir = d } }

// WithDispatcher gives it the one route into the session pipeline.
func WithDispatcher(d Dispatcher) Option { return func(s *Service) { s.disp = d } }

// WithHeadManager gives it the ability to start its own head.
func WithHeadManager(m HeadManager) Option { return func(s *Service) { s.heads = m } }

// WithActions is in proposals.go, beside the interface it takes; WithTriager and
// WithDigestAt are in heartbeat.go, and WithSummarizer is in compaction.go,
// beside theirs.

// WithAllowances lets the `allowances` verb answer.
func WithAllowances(a Allowances) Option { return func(s *Service) { s.allow = a } }

// WithTurnFacts lets the turn-end listener say what ended.
func WithTurnFacts(f TurnFacts) Option { return func(s *Service) { s.facts = f } }

// WithRegistry hands in the report registry rather than letting the service
// make its own, so a call following a session and the assistant writing its
// journal are looking at one registry.
func WithRegistry(r *Registry) Option {
	return func(s *Service) {
		if r != nil {
			s.reg = r
		}
	}
}

// WithBroadcaster wires the pushes.
func WithBroadcaster(b eventbus.Broadcaster) Option {
	return func(s *Service) {
		if b != nil {
			s.bus = b
		}
	}
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.log = l
		}
	}
}

// WithClock replaces time.Now, for tests that need to see a stamp.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// New builds the assistant.
//
// **It starts nothing.** No subprocess, no ticker, no sweep, no head: a
// constructor a test might call must not reach outside the process, and the
// head is started lazily by the first [Service.Say]. The heartbeat's timer is
// the same rule — [Service.RunHeartbeat] is called from the serve command's
// production block and stops with its context. Shutting down is
// [Service.Close], called from the same block.
func New(st Store, opts ...Option) (*Service, error) {
	if st == nil {
		return nil, errors.New("assistant: a store is required")
	}

	s := &Service{
		store:        st,
		reg:          NewRegistry(),
		bus:          eventbus.NopBroadcaster{},
		log:          slog.Default(),
		now:          time.Now,
		memoryBudget: memoryBriefingBudget,
		surfaces:     newSurfaceSet(),
		stateBase:    make(map[string]outcomeBase),
	}
	for _, opt := range opts {
		opt(s)
	}

	s.verbs = s.buildVerbs()
	s.byName = make(map[string]*Verb, len(s.verbs))
	for i := range s.verbs {
		s.byName[s.verbs[i].Name] = &s.verbs[i]
	}
	return s, nil
}

// Registry is the report registry this assistant routes through, so a call can
// follow a session on the same one the journal reads.
func (s *Service) Registry() *Registry { return s.reg }

// RegisterSurface adds a surface and returns the release func. Releasing twice
// is safe. A surface whose name is not a surface name is refused: the name is
// a JSON path component in its own seen mark.
func (s *Service) RegisterSurface(su Surface) (release func(), err error) {
	if su == nil {
		return nil, errors.New("assistant: nil surface")
	}
	if err := checkSurface(su.Name()); err != nil {
		return nil, fmt.Errorf("register surface: %w", err)
	}
	s.surfaces.add(su)

	var once sync.Once
	return func() { once.Do(func() { s.surfaces.remove(su) }) }, nil
}

// UnregisterSurface removes a surface. Removing one that was never registered
// is a no-op.
func (s *Service) UnregisterSurface(su Surface) {
	if su == nil {
		return
	}
	s.surfaces.remove(su)
}

// deliver hands an item to every registered surface.
//
// Failures are logged and never propagated: a surface that cannot render is
// that surface's problem, and the caller is an MCP tool handler or the
// runtime's turn-end listener, neither of which can do anything useful with
// "the thread was closed".
func (s *Service) deliver(ctx context.Context, item Item) {
	for _, su := range s.surfaces.snapshot() {
		if err := su.Deliver(ctx, item); err != nil {
			s.log.Warn("assistant item not delivered",
				"surface", su.Name(), "kind", item.Kind, "session", item.SessionID, "error", err)
		}
	}
}

// broadcast pushes on the global topic.
func (s *Service) broadcast(eventType string, payload any) {
	s.bus.Broadcast(eventType, payload)
}

// SinceLast is what this surface has missed, and it stamps the surface as
// having looked.
//
// Two halves with two mechanisms, because they answer two questions. The
// journal half is per ROW (`seen_by`), so an entry written while nobody was
// looking is still unseen when a surface finally arrives — stamped through the
// newest row of the look rather than row by row, so a look means "caught up to
// here" (see [Service.markLooked]). The conversation half is a per-surface
// MARK, because there is no row to stamp for a message nobody has read and a
// surface that has never looked has no journal row to carry its mark either.
//
// A surface that has never looked gets no conversation messages: it is about
// to render the whole tail anyway through [Service.History], and "new since
// last time" is not a thing that exists on a first visit.
//
// The mark is whole seconds and a message's stamp is fractional, so a message
// written inside the same second as a look reads as already seen. That is the
// conservative direction: showing news twice is a nuisance where a look that
// re-delivers what the reader was looking at is a bug.
func (s *Service) SinceLast(ctx context.Context, surface string) (Update, error) {
	update, err := s.unseen(ctx, surface)
	if err != nil {
		return Update{}, err
	}
	s.markLooked(ctx, surface, update)
	return update, nil
}

// Look is [Service.SinceLast] with the news discarded: the stamp alone.
//
// The thread renders its strip from the pure journal read on the socket's read
// lane, so what it owes when it is on screen is only the acknowledgement — the
// journal stamped through its newest row and the conversation mark moved. That
// is what clears the rail row's notch, and it is a write, so it has an op of
// its own on the mutation lane rather than riding the read.
func (s *Service) Look(ctx context.Context, surface string) error {
	_, err := s.SinceLast(ctx, surface)
	return err
}

// UnseenCount answers how many journal entries surface has never been shown.
//
// A pure read, for the rail row's notch at connect; the journal pushes keep
// the client's count current after that, and a look zeroes it. It counts
// rather than lists because the notch is one bit and the number is only
// spoken to a screen reader.
func (s *Service) UnseenCount(ctx context.Context, surface string) (int, error) {
	if err := checkSurface(surface); err != nil {
		return 0, err
	}
	n, err := s.store.CountAssistantJournalUnseen(ctx, nullString(surface))
	if err != nil {
		return 0, fmt.Errorf("count unseen journal for %q: %w", surface, err)
	}
	return int(n), nil
}

// unseen is [Service.SinceLast] without the stamp: a pure read.
//
// The two halves are separate because a look is not always a look. The head
// composes a preamble from this BEFORE its subprocess is known to have started,
// and a start that fails must not have consumed the news — which is the same
// reason a call reads its greeting's news at greeting time rather than at
// connect. So the read is pure and [Service.markLooked] is called once the news
// has actually reached somebody.
//
// It also creates nothing. A missing conversation reads as an empty one, so
// this can run on the socket's read lane, where creating the channel row would
// be a write.
func (s *Service) unseen(ctx context.Context, surface string) (Update, error) {
	if err := checkSurface(surface); err != nil {
		return Update{}, err
	}

	mark, err := s.surfaceMark(ctx, surface)
	if err != nil {
		return Update{}, err
	}
	update := Update{Since: mark, LookedAt: formatTime(s.now())}

	rows, err := s.store.ListAssistantJournalUnseen(ctx, store.ListAssistantJournalUnseenParams{
		Surface: nullString(surface),
		Lim:     maxSinceLastJournal,
	})
	if err != nil {
		return Update{}, fmt.Errorf("read unseen journal for %q: %w", surface, err)
	}
	// Newest first out of the query, oldest first out of here: this is read as
	// a sequence of events, by a person or by a preamble.
	for i := len(rows) - 1; i >= 0; i-- {
		update.Journal = append(update.Journal, journalEntryFrom(rows[i]))
	}

	channelID, found := s.conversationID(ctx)
	if mark != "" && found {
		messages, err := s.store.ListAssistantMessagesSince(ctx, store.ListAssistantMessagesSinceParams{
			ChannelID: channelID,
			Since:     mark,
			Lim:       maxSinceLastMessages,
		})
		if err != nil {
			return Update{}, fmt.Errorf("read conversation since %q: %w", mark, err)
		}
		for _, row := range messages {
			update.Messages = append(update.Messages, messageFrom(row))
		}
	}

	return update, nil
}

// markLooked records that this surface has now been shown everything in update.
//
// Best effort on purpose: showing the news twice is a nuisance, where failing
// the look because a stamp did not land would hide it entirely.
//
// The journal half is stamped THROUGH the newest row the look carried, not row
// by row. A look is bounded (fifty entries) and the journal is not, so stamping
// only what was returned left an older backlog unseen — and the next look then
// answered with the next fifty OLDER rows, announcing last week after today.
// One boundary means "caught up to here", which is the only reading that is
// monotonic in time.
func (s *Service) markLooked(ctx context.Context, surface string, update Update) {
	if update.LookedAt == "" {
		// Not a look: a zero Update is what a failed read answers, and stamping
		// for one would record that a surface had seen something nobody read.
		return
	}
	if len(update.Journal) > 0 {
		// Oldest first in the Update, so the last entry is the newest one — the
		// boundary everything at or before is now accounted for.
		newest := update.Journal[len(update.Journal)-1]
		err := s.store.MarkAssistantJournalSeenThrough(ctx, store.MarkAssistantJournalSeenThroughParams{
			Surface:   nullString(surface),
			At:        update.LookedAt,
			ThroughAt: newest.At,
			ThroughID: newest.ID,
		})
		if err != nil {
			s.log.Warn("assistant journal not stamped seen",
				"surface", surface, "through", newest.ID, "error", err)
		}
	}
	err := s.store.SetAssistantSurfaceMark(ctx, store.SetAssistantSurfaceMarkParams{
		Surface: nullString(surface),
		At:      update.LookedAt,
		Now:     update.LookedAt,
	})
	if err != nil {
		s.log.Warn("assistant surface mark not written", "surface", surface, "error", err)
	}
}

// surfaceMark reads when this surface last looked, or "".
func (s *Service) surfaceMark(ctx context.Context, surface string) (string, error) {
	state, err := s.store.GetAssistantState(ctx)
	if err != nil {
		// No state row yet is the ordinary first-boot case, not a failure: no
		// surface has looked, so there is no mark.
		return "", nil
	}
	if state.SurfaceMarks == "" {
		return "", nil
	}
	var marks map[string]string
	if err := json.Unmarshal([]byte(state.SurfaceMarks), &marks); err != nil {
		s.log.Warn("assistant surface marks unreadable", "error", err)
		return "", nil
	}
	return marks[surface], nil
}

// Follow adds a session to the watch list.
//
// The follow set was per call and in process memory, which is why a report
// arriving between calls went nowhere. It is a row now: the assistant follows
// everything it dispatches and anything it is asked to follow, and a report
// from any of them lands in the journal whether or not anyone is live.
//
// source is who asked — a surface name, "operator", or "dispatch" for work the
// assistant started. Free text, because the set of surfaces is not closed.
func (s *Service) Follow(ctx context.Context, sessionID, source string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("assistant: no session to follow")
	}
	err := s.store.UpsertAssistantFollow(ctx, store.UpsertAssistantFollowParams{
		SessionID: sessionID,
		Since:     formatTime(s.now()),
		Source:    source,
	})
	if err != nil {
		return fmt.Errorf("follow %s: %w", sessionID, err)
	}
	return nil
}

// Unfollow drops a session from the watch list. Unfollowing one that is not
// followed is not an error: the state the caller wanted is the state it gets.
func (s *Service) Unfollow(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return errors.New("assistant: no session to unfollow")
	}
	if err := s.store.DeleteAssistantFollow(ctx, sessionID); err != nil {
		return fmt.Errorf("unfollow %s: %w", sessionID, err)
	}
	if err := s.store.DeleteAssistantPeerFollow(ctx, sessionID); err != nil {
		return fmt.Errorf("unfollow remote %s: %w", sessionID, err)
	}
	return nil
}

// Following reports whether the assistant is watching this session.
//
// Read from the row rather than from the registry: the registry knows who is
// LIVE, and following outlives every surface being closed.
func (s *Service) Following(ctx context.Context, sessionID string) bool {
	follows, err := s.store.ListAssistantFollows(ctx)
	if err != nil {
		s.log.Warn("assistant follow list unreadable", "error", err)
		return false
	}
	for _, f := range follows {
		if f.SessionID == sessionID {
			return true
		}
	}
	return s.followingRemote(ctx, sessionID)
}

// Follows returns the watch list, oldest first.
func (s *Service) Follows(ctx context.Context) ([]store.AssistantFollow, error) {
	follows, err := s.store.ListAssistantFollows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list follows: %w", err)
	}
	return follows, nil
}

// MarkBriefed records that a surface has been told what this session is doing,
// so a greeting does not re-brief on every call.
func (s *Service) MarkBriefed(ctx context.Context, sessionID string, briefed bool) error {
	err := s.store.SetAssistantFollowBriefed(ctx, store.SetAssistantFollowBriefedParams{
		Briefed:   boolToInt(briefed),
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("mark %s briefed: %w", sessionID, err)
	}
	return nil
}

// Close releases what the assistant holds: today, the head's subprocess.
// Idempotent, and safe on a service that never started one.
//
// It also shuts the door. A say queued behind a long turn runs on a background
// context and would otherwise reach [Service.ensureHead] after shutdown and
// start a fresh subprocess — which outlives the process by design, taking its
// credential file with it, since the cleanup that removes that file is the one
// thing a dead server cannot run.
func (s *Service) Close() error {
	s.head.mu.Lock()
	s.head.closed = true
	s.head.mu.Unlock()
	return s.StopHead()
}

// nullString wraps a value for a query sqlc could not type through a JSON
// path expression. Always valid: an empty surface name never reaches here,
// because [checkSurface] runs first.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}
