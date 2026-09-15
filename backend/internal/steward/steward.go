// Package steward keeps a machine's health as a closed set of findings
// (docs/peers.md, the steward).
//
// Every machine runs one, and it has no model. It reads what the machine's own
// collectors already know — the CLI's sign-in, the disk, the loops, the
// sessions, the update checker, the backups, the brain's vector backend — and turns that into typed
// findings with a severity, facts, and a remedy. It opens a finding when a
// condition starts holding and resolves it when the condition stops, and it
// says so to whoever listens: the machine's own UI, its assistant if it has
// one, and every paired server following it.
//
// It judges nothing it cannot observe and acts on nothing. A finding is a fact,
// not a sentence: the words are the assistant's to write, and a fix that changes
// something is a proposal a person accepts. The set of kinds is closed on
// purpose, on the REST_GLYPH precedent: a new kind is a code change that chooses
// its resolve rule and its remedy rather than inheriting a blank.
package steward

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// Kind is what a finding is about.
type Kind string

const (
	// KindCLISignedOut: a provider CLI's credential is missing or refused. Only
	// a person at the machine can sign in again.
	KindCLISignedOut Kind = "cli-signed-out"
	// KindDiskLow: free space under the floor below which sessions start
	// failing to write.
	KindDiskLow Kind = "disk-low"
	// KindLoopPaused: a scheduled loop auto-paused on repeated failures and
	// stays paused until a person acts.
	KindLoopPaused Kind = "loop-paused"
	// KindSessionBlockedLong: a session has waited on an approval or a question
	// for longer than a person plausibly means to leave it.
	KindSessionBlockedLong Kind = "session-blocked-long"
	// KindUpdateWaiting: a newer release is published for this machine.
	KindUpdateWaiting Kind = "update-waiting"
	// KindBackupFailing: no database backup has landed for several intervals.
	KindBackupFailing Kind = "backup-failing"
	// KindSemanticRecallDown: the brain's configured vector backend has been
	// unreachable long enough to be an outage, so memory recall is keyword-only
	// and consolidation is paused. The brain retries on its own; bringing Chroma
	// or the embedder back is a hand's job.
	KindSemanticRecallDown Kind = "semantic-recall-down"
)

// Kinds is the closed set, for tests and for a surface that must render every
// one.
var Kinds = []Kind{
	KindCLISignedOut, KindDiskLow, KindLoopPaused, KindSessionBlockedLong, KindUpdateWaiting, KindBackupFailing,
	KindSemanticRecallDown,
}

// Severity is how much a finding claims attention.
type Severity string

const (
	// SeverityWarning: something is not working and will not fix itself.
	SeverityWarning Severity = "warning"
	// SeverityNotice: worth knowing, nothing is broken.
	SeverityNotice Severity = "notice"
)

// Remedy is what fixes a finding, as a kind rather than a sentence.
type Remedy string

const (
	// RemedyHand: only a person at that machine can do it.
	RemedyHand Remedy = "hand"
	// RemedyReclaim: reclaiming finished sessions frees the space — a proposal.
	RemedyReclaim Remedy = "reclaim"
	// RemedyUpdate: applying the update — costs any turn in flight.
	RemedyUpdate Remedy = "update"
)

// Finding is one open condition.
type Finding struct {
	Kind Kind `json:"kind"`
	// Subject distinguishes two findings of one kind: a vendor id, a loop id, a
	// session id. Empty for a machine-wide kind.
	Subject  string         `json:"subject,omitempty"`
	Severity Severity       `json:"severity"`
	Remedy   Remedy         `json:"remedy"`
	Facts    map[string]any `json:"facts,omitempty"`
	// OpenedAt and ResolvedAt are UTC RFC3339 seconds; ResolvedAt is empty
	// while the finding holds.
	OpenedAt   string `json:"openedAt,omitempty"`
	ResolvedAt string `json:"resolvedAt,omitempty"`
}

// Key identifies a finding across evaluations.
func (f Finding) Key() string { return string(f.Kind) + "\x00" + f.Subject }

// Observation is everything the steward is shown in one pass. Observed names
// the kinds whose sensors actually read: a kind missing from it opens and
// resolves nothing, because a sensor that could not read is not a condition
// that stopped holding.
type Observation struct {
	Agents   []AgentAuth
	Disk     Disk
	Loops    []PausedLoop
	Blocked  []BlockedSession
	Update   Update
	Backup   Backup
	Brain    Brain
	Observed map[Kind]bool
}

// AgentAuth is one provider's credential state.
type AgentAuth struct {
	ID, Name string
	// SignedOut: the collector reports an auth problem only the CLI can fix.
	SignedOut bool
	Help      string
}

// Disk is the data directory's free space.
type Disk struct {
	FreeBytes        uint64
	ReclaimableBytes int64
	Path             string
}

// PausedLoop is a loop auto-paused on failures.
type PausedLoop struct {
	ID, Name, SessionID string
	Failures            int64
}

// BlockedSession is a session waiting on a person, with when that started.
type BlockedSession struct {
	ID, Name string
	// Waiting is "approval" or "question".
	Waiting string
	Since   time.Time
}

// Update is the release channel's answer.
type Update struct {
	Behind          bool
	Current, Latest string
}

// Backup is the newest periodic backup.
type Backup struct {
	Enabled  bool
	Newest   time.Time
	Interval time.Duration
}

// Brain is where the brain's semantic recall stands.
type Brain struct {
	// Down: a vector backend is configured and not attached.
	Down bool
	// DownSince is when it was lost — the brain's own clock, so a detached
	// period that outlives many passes is one condition, not one per pass.
	DownSince time.Time
	// Reason is the brain's closed reason: which half failed.
	Reason string
}

// The thresholds. A disk floor, not a percentage: a small disk at 88% is its
// normal state (usage.md). The floor is the frontend's LOW_DISK_BYTES
// (lib/storage/fleet.ts), the one predicate the disk meter and its notch read —
// under 3 GiB the next dependency install in a worktree fails — because a
// finding that opens at a different level from the amber meter reports two
// things about one disk. The others are "longer than a person means".
//
// SemanticDownAfter is longer than any attach a working backend takes and
// than the detach threshold's blip, so a restart of the containers is not
// news; it opens at most once per detached period, because the period's start
// is the brain's DownSince rather than a clock the steward restarts.
const (
	DiskFloorBytes    = 3 << 30
	BlockedAfter      = 30 * time.Minute
	BackupMissedAfter = 3
	SemanticDownAfter = 10 * time.Minute
)

// Evaluate turns an observation into the findings that hold now. Pure: the
// clock is a parameter, and nothing here reads or writes anything.
//
// Only kinds in obs.Observed produce findings, so the reconciler resolves only
// what a sensor actually saw stop.
func Evaluate(obs Observation, now time.Time) []Finding {
	var out []Finding
	if obs.Observed[KindCLISignedOut] {
		for _, a := range obs.Agents {
			if !a.SignedOut {
				continue
			}
			out = append(out, Finding{Kind: KindCLISignedOut, Subject: a.ID, Severity: SeverityWarning,
				Remedy: RemedyHand, Facts: map[string]any{"agent": a.Name, "help": a.Help}})
		}
	}
	if obs.Observed[KindDiskLow] && obs.Disk.FreeBytes < DiskFloorBytes {
		remedy := RemedyHand
		if obs.Disk.ReclaimableBytes > 0 {
			remedy = RemedyReclaim
		}
		out = append(out, Finding{Kind: KindDiskLow, Severity: SeverityWarning, Remedy: remedy,
			Facts: map[string]any{"freeBytes": obs.Disk.FreeBytes, "reclaimableBytes": obs.Disk.ReclaimableBytes,
				"path": obs.Disk.Path}})
	}
	if obs.Observed[KindLoopPaused] {
		for _, l := range obs.Loops {
			out = append(out, Finding{Kind: KindLoopPaused, Subject: l.ID, Severity: SeverityWarning,
				Remedy: RemedyHand, Facts: map[string]any{"loop": l.Name, "sessionId": l.SessionID,
					"failures": l.Failures}})
		}
	}
	if obs.Observed[KindSessionBlockedLong] {
		for _, b := range obs.Blocked {
			if now.Sub(b.Since) < BlockedAfter {
				continue
			}
			out = append(out, Finding{Kind: KindSessionBlockedLong, Subject: b.ID, Severity: SeverityNotice,
				Remedy: RemedyHand, Facts: map[string]any{"session": b.Name, "waiting": b.Waiting,
					"since": b.Since.UTC().Format(time.RFC3339)}})
		}
	}
	if obs.Observed[KindUpdateWaiting] && obs.Update.Behind {
		out = append(out, Finding{Kind: KindUpdateWaiting, Severity: SeverityNotice, Remedy: RemedyUpdate,
			Facts: map[string]any{"current": obs.Update.Current, "latest": obs.Update.Latest}})
	}
	if obs.Observed[KindBackupFailing] && obs.Backup.Enabled && obs.Backup.Interval > 0 {
		missed := time.Duration(BackupMissedAfter) * obs.Backup.Interval
		if obs.Backup.Newest.IsZero() || now.Sub(obs.Backup.Newest) > missed {
			facts := map[string]any{"interval": obs.Backup.Interval.String()}
			if !obs.Backup.Newest.IsZero() {
				facts["newest"] = obs.Backup.Newest.UTC().Format(time.RFC3339)
			}
			out = append(out, Finding{Kind: KindBackupFailing, Severity: SeverityWarning, Remedy: RemedyHand,
				Facts: facts})
		}
	}
	if obs.Observed[KindSemanticRecallDown] && obs.Brain.Down && !obs.Brain.DownSince.IsZero() &&
		now.Sub(obs.Brain.DownSince) >= SemanticDownAfter {
		out = append(out, Finding{Kind: KindSemanticRecallDown, Severity: SeverityWarning, Remedy: RemedyHand,
			Facts: map[string]any{"reason": obs.Brain.Reason,
				"since": obs.Brain.DownSince.UTC().Format(time.RFC3339)}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Store is the findings table.
type Store interface {
	OpenFindings(ctx context.Context) ([]Finding, error)
	Open(ctx context.Context, f Finding) error
	Refresh(ctx context.Context, f Finding) error
	Resolve(ctx context.Context, f Finding) error
}

// Change is one transition the steward made, for whoever listens.
type Change struct {
	Finding Finding
	// Opened is true for a finding that started holding, false for one that
	// stopped.
	Opened bool
}

// Steward runs the passes.
type Steward struct {
	sense   func(ctx context.Context) Observation
	store   Store
	notify  func(ctx context.Context, c Change)
	now     func() time.Time
	blocked map[string]time.Time
}

// New builds a steward. sense gathers an observation; notify hears every
// transition. It does no IO.
func New(sense func(ctx context.Context) Observation, st Store, notify func(ctx context.Context, c Change)) *Steward {
	return &Steward{sense: sense, store: st, notify: notify, now: time.Now, blocked: make(map[string]time.Time)}
}

// Run passes every interval until ctx ends. Started from serve, never a
// constructor.
func (s *Steward) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Pass(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("steward: pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// stampBlocked fills in when each blocked session was first seen blocked. A
// pending approval carries no timestamp of its own, so the steward remembers;
// a restart starts the clock again, which errs towards saying nothing.
func (s *Steward) stampBlocked(obs *Observation, now time.Time) {
	seen := make(map[string]bool, len(obs.Blocked))
	for i := range obs.Blocked {
		b := &obs.Blocked[i]
		seen[b.ID] = true
		if !b.Since.IsZero() {
			continue
		}
		first, ok := s.blocked[b.ID]
		if !ok {
			first = now
			s.blocked[b.ID] = now
		}
		b.Since = first
	}
	for id := range s.blocked {
		if !seen[id] {
			delete(s.blocked, id)
		}
	}
}

// Pass evaluates once and reconciles the table: a finding that holds and is not
// open is opened, one that is open and no longer holds is resolved — but only
// for a kind the observation actually saw.
func (s *Steward) Pass(ctx context.Context) error {
	obs := s.sense(ctx)
	now := s.now()
	s.stampBlocked(&obs, now)
	current := Evaluate(obs, now)

	open, err := s.store.OpenFindings(ctx)
	if err != nil {
		return fmt.Errorf("read open findings: %w", err)
	}
	openByKey := make(map[string]Finding, len(open))
	for _, f := range open {
		openByKey[f.Key()] = f
	}
	stamp := now.UTC().Truncate(time.Second).Format(time.RFC3339)

	holding := make(map[string]bool, len(current))
	for _, f := range current {
		holding[f.Key()] = true
		if existing, ok := openByKey[f.Key()]; ok {
			f.OpenedAt = existing.OpenedAt
			if err := s.store.Refresh(ctx, f); err != nil {
				return fmt.Errorf("refresh %s: %w", f.Kind, err)
			}
			continue
		}
		f.OpenedAt = stamp
		if err := s.store.Open(ctx, f); err != nil {
			return fmt.Errorf("open %s: %w", f.Kind, err)
		}
		if s.notify != nil {
			s.notify(ctx, Change{Finding: f, Opened: true})
		}
	}
	for key, f := range openByKey {
		if holding[key] || !obs.Observed[f.Kind] {
			continue
		}
		f.ResolvedAt = stamp
		if err := s.store.Resolve(ctx, f); err != nil {
			return fmt.Errorf("resolve %s: %w", f.Kind, err)
		}
		if s.notify != nil {
			s.notify(ctx, Change{Finding: f, Opened: false})
		}
	}
	return nil
}
