package peer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Outbox event kinds.
const (
	// EventReport is one agent-written report from a followed session.
	EventReport = "report"
	// EventTurnEnd is a followed session's turn ending: finished, failed, or
	// stopped on something only a person can answer.
	EventTurnEnd = "turn_end"
)

const (
	// outboxRetention is how long undelivered news is kept for a follower that
	// has not polled. Long enough for a laptop closed over a weekend.
	outboxRetention = 7 * 24 * time.Hour
	// pruneEvery bounds how often a write also sweeps retention.
	pruneEvery = time.Hour
	// maxEventsPerPoll bounds one answer; a follower further behind polls again.
	maxEventsPerPoll = 200
	// MaxWait is the longest a poll is held open with nothing to say.
	MaxWait = 25 * time.Second
	// Reports per session in a window, the same shape as the registry's bucket:
	// a run finds two surprises in its first minute and then nothing for ten.
	reportsPerWindow = 3
	reportWindow     = 9 * time.Minute
	ingestBudget     = 5 * time.Second
)

// Store is the peer tables.
type Store interface {
	UpsertPeerFollow(ctx context.Context, arg store.UpsertPeerFollowParams) error
	ListPeerFollowers(ctx context.Context, sessionID string) ([]string, error)
	InsertPeerOutbox(ctx context.Context, arg store.InsertPeerOutboxParams) (int64, error)
	ListPeerOutboxSince(ctx context.Context, arg store.ListPeerOutboxSinceParams) ([]store.ListPeerOutboxSinceRow, error)
	LatestPeerOutboxSeq(ctx context.Context, credentialID string) (int64, error)
	PrunePeerOutbox(ctx context.Context, at string) (int64, error)
}

// Event is one row of a follower's outbox as it goes on the wire. Payload is
// agent-written where it carries a headline, and says so with `untrusted`.
type Event struct {
	Seq       int64           `json:"seq"`
	Kind      string          `json:"kind"`
	SessionID string          `json:"sessionId"`
	Payload   json.RawMessage `json:"payload"`
	At        string          `json:"at"`
}

// EventsResponse answers GET /api/peer/events. Latest is the newest seq this
// follower has, so one that has never read can start from now.
type EventsResponse struct {
	Events []Event `json:"events"`
	Latest int64   `json:"latest"`
}

// Outbox records what a paired server following a session here has not heard
// yet: the reports its agent files and how its turns end.
//
// It exists on every server whatever its feature flags, because the machine a
// session runs on need not run an assistant of its own. The acting server
// reads it by polling ([Outbox.Events]); nothing here dials out.
type Outbox struct {
	store   Store
	facts   assistant.TurnFacts
	now     func() time.Time
	reports *limiter

	mu        sync.Mutex
	wake      chan struct{}
	lastPrune time.Time
}

// OutboxOption configures an [Outbox].
type OutboxOption func(*Outbox)

// withOutboxClock replaces the clock, for tests.
func withOutboxClock(now func() time.Time) OutboxOption {
	return func(o *Outbox) {
		o.now = now
		o.reports = newLimiter(now)
	}
}

// NewOutbox builds the outbox. facts may be nil, in which case a turn ending
// is recorded without its outcome words. It does no IO.
func NewOutbox(st Store, facts assistant.TurnFacts, opts ...OutboxOption) *Outbox {
	o := &Outbox{store: st, facts: facts, now: time.Now, reports: newLimiter(time.Now), wake: make(chan struct{})}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Follow records that the credential's server sent to or created this session.
func (o *Outbox) Follow(ctx context.Context, sessionID, credential, policyID string) error {
	return o.store.UpsertPeerFollow(ctx, store.UpsertPeerFollowParams{
		SessionID:    sessionID,
		CredentialID: credential,
		PolicyID:     policyID,
		FollowedAt:   o.stamp(),
	})
}

// Report files one agent report for every paired server following the session.
// It answers whether anybody was following and the report was kept; a report
// over the session's budget is not kept, and says so through the error-free
// false.
func (o *Outbox) Report(ctx context.Context, sessionID string, report assistant.Report) (bool, error) {
	followers, err := o.store.ListPeerFollowers(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("list peer followers: %w", err)
	}
	if len(followers) == 0 {
		return false, nil
	}
	if !o.reports.take(sessionID, reportsPerWindow, reportWindow) {
		return false, nil
	}
	payload := map[string]any{
		"kind":     string(report.Kind),
		"headline": report.Headline,
		// Written by an agent about repository content nobody here authored:
		// relayed, never followed.
		"untrusted": true,
	}
	return true, o.record(ctx, followers, EventReport, sessionID, payload)
}

// OnTurnEnd is the turn-end listener, wired to Manager.AddTurnEndListener. It
// returns at once: the listener runs on the runtime's own path.
func (o *Outbox) OnTurnEnd(sessionID string) {
	if sessionID == "" {
		return
	}
	go o.recordTurnEnd(sessionID)
}

func (o *Outbox) recordTurnEnd(sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), ingestBudget)
	defer cancel()

	followers, err := o.store.ListPeerFollowers(ctx, sessionID)
	if err != nil {
		slog.Warn("peer outbox: followers unavailable", "session", sessionID, "error", err)
		return
	}
	if len(followers) == 0 {
		return
	}

	payload := map[string]any{"kind": string(assistant.NoticeFinished)}
	if o.facts != nil {
		notice, outcome, err := assistant.TurnNotice(ctx, o.facts, sessionID)
		if err != nil {
			slog.Warn("peer outbox: turn outcome unavailable", "session", sessionID, "error", err)
		} else {
			payload["kind"] = string(notice.Kind)
			if notice.Headline != "" {
				payload["headline"] = notice.Headline
				payload["untrusted"] = true
			}
			if outcome.SessionName != "" {
				payload["name"] = outcome.SessionName
			}
		}
	}
	if err := o.record(ctx, followers, EventTurnEnd, sessionID, payload); err != nil {
		slog.Warn("peer outbox: turn end not recorded", "session", sessionID, "error", err)
	}
}

// record writes one row per follower and wakes every waiting poll.
func (o *Outbox) record(ctx context.Context, followers []string, kind, sessionID string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s payload: %w", kind, err)
	}
	at := o.stamp()
	var errs []error
	for _, credential := range followers {
		if _, err := o.store.InsertPeerOutbox(ctx, store.InsertPeerOutboxParams{
			CredentialID: credential, Kind: kind, SessionID: sessionID, Payload: string(raw), At: at,
		}); err != nil {
			errs = append(errs, fmt.Errorf("outbox row for %s: %w", credential, err))
		}
	}
	o.broadcast()
	o.maybePrune(ctx)
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("record %s: %w", kind, err)
	}
	return nil
}

// Events answers one follower's poll: everything after since, waiting up to
// wait for something to arrive when there is nothing yet.
func (o *Outbox) Events(ctx context.Context, credential string, since int64, wait time.Duration) (EventsResponse, error) {
	if wait > MaxWait {
		wait = MaxWait
	}
	out, err := o.read(ctx, credential, since)
	if err != nil || len(out.Events) > 0 || wait <= 0 {
		return out, err
	}

	// Take the wake channel before reading again, so a row inserted between
	// the read above and this wait is not slept through.
	o.mu.Lock()
	wake := o.wake
	o.mu.Unlock()
	if out, err = o.read(ctx, credential, since); err != nil || len(out.Events) > 0 {
		return out, err
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-wake:
	case <-timer.C:
		return out, nil
	case <-ctx.Done():
		return out, nil
	}
	return o.read(ctx, credential, since)
}

func (o *Outbox) read(ctx context.Context, credential string, since int64) (EventsResponse, error) {
	rows, err := o.store.ListPeerOutboxSince(ctx, store.ListPeerOutboxSinceParams{
		CredentialID: credential, Since: since, MaxRows: maxEventsPerPoll,
	})
	if err != nil {
		return EventsResponse{}, fmt.Errorf("read outbox: %w", err)
	}
	latest, err := o.store.LatestPeerOutboxSeq(ctx, credential)
	if err != nil {
		return EventsResponse{}, fmt.Errorf("read outbox head: %w", err)
	}
	out := EventsResponse{Events: make([]Event, 0, len(rows)), Latest: latest}
	for _, row := range rows {
		out.Events = append(out.Events, Event{
			Seq: row.Seq, Kind: row.Kind, SessionID: row.SessionID, Payload: json.RawMessage(row.Payload), At: row.At,
		})
	}
	return out, nil
}

func (o *Outbox) broadcast() {
	o.mu.Lock()
	close(o.wake)
	o.wake = make(chan struct{})
	o.mu.Unlock()
}

func (o *Outbox) maybePrune(ctx context.Context) {
	o.mu.Lock()
	now := o.now()
	due := now.Sub(o.lastPrune) >= pruneEvery
	if due {
		o.lastPrune = now
	}
	o.mu.Unlock()
	if !due {
		return
	}
	cutoff := now.Add(-outboxRetention).UTC().Format(time.RFC3339)
	if n, err := o.store.PrunePeerOutbox(ctx, cutoff); err != nil {
		slog.Warn("peer outbox: retention sweep failed", "error", err)
	} else if n > 0 {
		slog.Info("peer outbox: aged out undelivered events", "count", n)
	}
}

func (o *Outbox) stamp() string { return o.now().UTC().Format(time.RFC3339) }
