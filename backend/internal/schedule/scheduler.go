// Package schedule implements scheduled loops: agentique-owned recurring,
// one-shot, and dynamic prompts fired into sessions as fresh turns, durable
// across restarts and idle eviction. Design: docs/scheduled-loops.md.
package schedule

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/allbin/agentkit/runtime"
	"github.com/allbin/agentkit/sqliteops"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// Run statuses. Lifecycle is one-way: queued → firing → running → terminal;
// ResolveScheduleRun's WHERE clause enforces terminal-once at the DB layer.
const (
	RunQueued       = "queued"
	RunFiring       = "firing"
	RunRunning      = "running"
	RunOK           = "ok"
	RunActionNeeded = "action_needed"
	RunError        = "error"
	RunDeferred     = "deferred"
	RunInterrupted  = "interrupted"
	RunSkipped      = "skipped"
)

// Schedule modes.
const (
	ModeRecurring = "recurring"
	ModeOnce      = "once"
	ModeDynamic   = "dynamic"
)

// Attention states on a schedule row. action_needed clears on viewing (or a
// later ok run); failed clears only on an explicit act (resume/edit/delete).
const (
	AttentionActionNeeded = "action_needed"
	AttentionFailed       = "failed"
)

// Pause reasons.
const (
	PauseUser             = "user"
	PauseCompleted        = "completed"
	PauseExpired          = "expired"
	PauseSessionCompleted = "session-completed"
	PauseAutoFailures     = "auto-failures"
	PauseDynamicEnded     = "dynamic-ended"
	PausePendingApproval  = "pending-approval"
	PauseInvalidSpec      = "invalid-schedule"
)

// timeFormat pins every schedules/schedule_runs timestamp to UTC RFC3339
// seconds precision. SQLite compares TEXT lexicographically, so mixed
// precision or offsets would break next_run_at ordering.
const timeFormat = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeFormat) }

func parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// maxDeliveryAttempts bounds delivery retries per run (busy refusals don't
// consume attempts — only hard delivery errors do).
const maxDeliveryAttempts = 3

// deliveryBackoff maps attempt count (1-based, after the failed attempt) to
// the wait before the next try.
var deliveryBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// catchupIterationCap bounds the missed-slot walk for pathological specs.
const catchupIterationCap = 10000

// runNowReason marks ad-hoc run-now runs: they deliver on paused schedules
// and never consume a one-shot's cadence fire.
const runNowReason = "run now"

// pendingHumanGrace is how long a running fire must be in flight before the
// blocked-on-human check may resolve it action_needed — long enough that a
// just-opened approval the user is actively clicking doesn't flap.
const pendingHumanGrace = time.Minute

// errSchedulePausedRace aborts a fire transaction whose schedule was paused
// between the due-list read and the claim commit. Benign: rolls back the run
// insert so nothing fires on a paused loop.
var errSchedulePausedRace = errors.New("schedule paused mid-tick")

// firingReclaimAfter is how long a run may sit in `firing` with no fired_at
// before the sweep reclaims it to queued. A firing row is transitional for
// milliseconds; one surviving this long lost its deliverer to a write
// failure after the claim, and without reclaim the loop wedges on
// "previous run still running" until the next server restart.
const firingReclaimAfter = 2 * time.Minute

// Gateway is the narrow session-facing surface the scheduler needs.
// Implemented by sessionGateway over session.Service; faked in tests.
type Gateway interface {
	// Deliver starts a fresh turn on the session (lazy-resuming it if
	// stopped/evicted) and returns the turn index plus its outcome channel.
	// A busy session returns an error matching session.ErrBusy.
	Deliver(ctx context.Context, sessionID, prompt string, origin session.QueryOrigin) (int, <-chan session.TurnOutcome, error)
	// SessionFinished reports whether the session is user-finished
	// (completed or merged) — checked at delivery time so a fire cannot
	// silently reopen a session the user considers done.
	SessionFinished(ctx context.Context, sessionID string) (bool, error)
	// PendingHumanInput describes what the session's turn is blocked on
	// ("" when nothing) — approvals, questions.
	PendingHumanInput(sessionID string) string
}

// Options tunes the scheduler; zero values fall back to the documented
// defaults. Loc is the cron-evaluation timezone (server-local in production).
type Options struct {
	TickInterval           time.Duration
	InitialDelay           time.Duration
	MinInterval            time.Duration
	MaxRunDuration         time.Duration
	MaxConsecutiveFailures int
	RunHistory             int
	OnceCatchupWindow      time.Duration
	DynamicMaxDelay        time.Duration
	DynamicFallback        time.Duration
	Loc                    *time.Location
	// Now overrides the clock (tests). Defaults to time.Now.
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.TickInterval <= 0 {
		o.TickInterval = 20 * time.Second
	}
	if o.InitialDelay <= 0 {
		o.InitialDelay = 30 * time.Second
	}
	if o.MinInterval <= 0 {
		o.MinInterval = time.Minute
	}
	if o.MaxRunDuration <= 0 {
		o.MaxRunDuration = 30 * time.Minute
	}
	if o.MaxConsecutiveFailures <= 0 {
		o.MaxConsecutiveFailures = 3
	}
	if o.RunHistory <= 0 {
		o.RunHistory = 200
	}
	if o.OnceCatchupWindow <= 0 {
		o.OnceCatchupWindow = time.Hour
	}
	if o.DynamicMaxDelay <= 0 {
		o.DynamicMaxDelay = 6 * time.Hour
	}
	if o.DynamicFallback <= 0 {
		o.DynamicFallback = 20 * time.Minute
	}
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// Scheduler owns the tick loop, the fire pipeline, and run resolution.
// Construction is side-effect-free; Start launches the loop and BootSweep
// (called separately, before Start, from serve.go) reconciles runs stranded
// by an ungraceful exit.
type Scheduler struct {
	opts      Options
	db        *sql.DB
	q         *store.Queries
	gw        Gateway
	broadcast func(eventType string, payload any)

	// sessionLocks grows one mutex per session ever scheduled against in this
	// process's lifetime — never pruned (a delete would race a holder), and
	// at ~one small allocation per session it is deliberately accepted.
	lockMu       sync.Mutex
	sessionLocks map[string]*sync.Mutex

	// spawnMu gates goroutine launches against Stop: wg.Add concurrent with
	// wg.Wait while the counter is at zero is documented WaitGroup misuse
	// (OnSessionIdle and RunNow arrive on external goroutines at any time).
	spawnMu sync.RWMutex
	stopped bool

	// firingSeen tracks when the sweep first observed each firing-status run,
	// for stuck-firing reclamation. Touched only from the tick goroutine —
	// no lock needed.
	firingSeen map[string]time.Time

	// pendingReports holds agent-volunteered ScheduleReport outcomes for
	// in-flight runs, consumed by resolveOutcome (the report wins over the
	// final-text fallback). In-memory on purpose: the outcome always resolves
	// in this process or the boot sweep supersedes it. Guarded by reportMu.
	reportMu       sync.Mutex
	pendingReports map[string]agentReport

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// spawn launches a tracked goroutine unless the scheduler has stopped.
func (s *Scheduler) spawn(fn func()) {
	s.spawnMu.RLock()
	if s.stopped {
		s.spawnMu.RUnlock()
		return
	}
	s.wg.Add(1)
	s.spawnMu.RUnlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
}

// NewScheduler creates a scheduler. broadcast is the global (Broadcast, not
// per-project Publish) push emitter; nil disables pushes.
func NewScheduler(db *sql.DB, q *store.Queries, gw Gateway, broadcast func(string, any), opts Options) *Scheduler {
	if broadcast == nil {
		broadcast = func(string, any) {}
	}
	return &Scheduler{
		opts:           opts.withDefaults(),
		db:             db,
		q:              q,
		gw:             gw,
		broadcast:      broadcast,
		sessionLocks:   make(map[string]*sync.Mutex),
		firingSeen:     make(map[string]time.Time),
		pendingReports: make(map[string]agentReport),
		done:           make(chan struct{}),
	}
}

// Start launches the tick loop: an initial short-delay pass (so a frequently
// restarted server can't defer overdue fires forever), then the ticker.
func (s *Scheduler) Start() {
	s.spawn(s.loop)
}

// Stop halts the loop and waits for in-flight delivery goroutines to park.
// Outcome waiters exit immediately; unresolved runs are reconciled by the
// next boot's sweep.
func (s *Scheduler) Stop() {
	s.spawnMu.Lock()
	s.stopped = true
	s.spawnMu.Unlock()
	s.stopOnce.Do(func() { close(s.done) })
	s.wg.Wait()
}

func (s *Scheduler) loop() {
	initial := time.NewTimer(s.opts.InitialDelay)
	defer initial.Stop()
	select {
	case <-initial.C:
		s.tick()
	case <-s.done:
		return
	}
	ticker := time.NewTicker(s.opts.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-s.done:
			return
		}
	}
}

func (s *Scheduler) tick() {
	ctx := context.Background()
	now := s.opts.Now().UTC()

	due, err := s.q.ListDueSchedules(ctx, formatTime(now))
	if err != nil {
		slog.Error("scheduler: list due schedules failed", "error", err)
		return
	}
	for _, sched := range due {
		if err := s.fireDue(ctx, sched, now); err != nil {
			slog.Error("scheduler: fire failed", "schedule_id", sched.ID, "error", err)
		}
	}

	s.retryAndOverduePass(ctx, now)
}

// fireDue claims the due slot for one schedule: computes the slot key and the
// advanced next_run_at, writes both atomically, then hands the run to
// delivery. Crash-idempotent: the run insert is ON CONFLICT DO NOTHING on
// (schedule_id, scheduled_for), so a replayed slot is a no-op.
func (s *Scheduler) fireDue(ctx context.Context, sched store.Schedule, now time.Time) error {
	// Expiry is a visible pause, not a silent stop.
	if sched.ExpiresAt != "" && sched.ExpiresAt <= formatTime(now) {
		return s.pauseSchedule(ctx, sched.ID, PauseExpired, "")
	}

	// Unfinished predecessor rules: a still-running previous run skips this
	// slot; a still-queued one (never delivered) is resolved skipped and the
	// new slot takes its place.
	unfinished, err := s.q.ListUnfinishedRunsForSchedule(ctx, sched.ID)
	if err != nil {
		return fmt.Errorf("list unfinished runs: %w", err)
	}
	for _, r := range unfinished {
		switch r.Status {
		case RunQueued:
			s.resolveRun(ctx, r.ID, resolution{status: RunSkipped, reason: "not delivered before the next slot"})
		case RunFiring, RunRunning:
			return s.recordSkippedSlot(ctx, sched, now, "previous run still running")
		}
	}

	var slot time.Time
	var newNext string
	var aggregateSkip string
	skippedCount := 0

	switch sched.Mode {
	case ModeOnce:
		slotT, ok := parseTime(sched.NextRunAt)
		if !ok {
			return s.pauseSchedule(ctx, sched.ID, PauseInvalidSpec, "unparseable fire time")
		}
		if now.Sub(slotT) > s.opts.OnceCatchupWindow {
			// A reminder firing far too late is wrong; surface it instead.
			s.insertRunRow(ctx, sched, slotT, RunSkipped, fmt.Sprintf("missed by more than %s", s.opts.OnceCatchupWindow), now)
			s.setAttention(ctx, sched.ID, AttentionActionNeeded, "")
			return s.pauseSchedule(ctx, sched.ID, PauseCompleted, "")
		}
		slot = slotT
		newNext = "" // parked; resolution marks the schedule completed

	case ModeDynamic:
		slot = now
		// Fallback pre-write: dynamic mode is not creatable until M2's
		// ScheduleNext tool lands (Create derives only recurring/once); this
		// scaffold refires on the fallback cadence. M2 adds ScheduleNext
		// overwriting this and dynamic-ended parking when a loop stops
		// rescheduling.
		newNext = formatTime(now.Add(s.opts.DynamicFallback))

	default: // recurring
		spec, err := ParseSpec(sched.Cron)
		if err != nil {
			return s.pauseSchedule(ctx, sched.ID, PauseInvalidSpec, err.Error())
		}
		slotT, ok := parseTime(sched.NextRunAt)
		if !ok {
			return s.pauseSchedule(ctx, sched.ID, PauseInvalidSpec, "unparseable next_run_at")
		}
		// Catch-up: fire once for the most recent missed occurrence; older
		// missed slots collapse into one honest aggregate skip row.
		slot = slotT
		for i := 0; i < catchupIterationCap; i++ {
			nxt := spec.Next(slot, s.opts.Loc)
			if nxt.IsZero() || nxt.After(now) {
				break
			}
			slot = nxt
			skippedCount++
		}
		next := spec.Next(now, s.opts.Loc)
		if next.IsZero() {
			return s.pauseSchedule(ctx, sched.ID, PauseInvalidSpec, "cron expression has no future occurrence")
		}
		newNext = formatTime(next)
		if skippedCount > 0 {
			aggregateSkip = fmt.Sprintf("server offline or busy: %d earlier slot(s) missed", skippedCount)
		}
	}

	runID := newID()
	claimed := false
	txErr := sqliteops.RetryWrite(func() error {
		return store.RunInTx(s.db, func(q *store.Queries) error {
			rows, err := q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
				ID:           runID,
				ScheduleID:   sched.ID,
				SessionID:    sched.SessionID,
				ScheduledFor: formatTime(slot),
				CreatedAt:    formatTime(now),
				Status:       RunQueued,
				Reason:       "",
			})
			if err != nil {
				return err
			}
			claimed = rows > 0
			if aggregateSkip != "" {
				// Slot key = the original overdue occurrence; content = the gap.
				if _, err := q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
					ID:           newID(),
					ScheduleID:   sched.ID,
					SessionID:    sched.SessionID,
					ScheduledFor: formatTime(mustParse(sched.NextRunAt)),
					CreatedAt:    formatTime(now),
					Status:       RunSkipped,
					Reason:       aggregateSkip,
				}); err != nil {
					return err
				}
			}
			advanced, err := q.AdvanceScheduleNextRunIfEnabled(ctx, store.AdvanceScheduleNextRunIfEnabledParams{
				NextRunAt: newNext,
				LastRunAt: formatTime(now),
				UpdatedAt: formatTime(now),
				ID:        sched.ID,
			})
			if err != nil {
				return err
			}
			if advanced == 0 {
				return errSchedulePausedRace
			}
			return nil
		})
	})
	if errors.Is(txErr, errSchedulePausedRace) {
		return nil // pause landed between the due read and the claim — no-op
	}
	if txErr != nil {
		return fmt.Errorf("claim slot: %w", txErr)
	}
	s.pushSchedule(ctx, sched.ID)
	if !claimed {
		return nil
	}
	s.pushRun(ctx, runID)
	s.pruneRuns(ctx, sched.ID)

	s.spawn(func() { s.attemptDelivery(context.Background(), runID) })
	return nil
}

// recordSkippedSlot writes a skipped run row for the current slot and
// advances next_run_at, without delivering anything.
func (s *Scheduler) recordSkippedSlot(ctx context.Context, sched store.Schedule, now time.Time, reason string) error {
	newNext := ""
	switch sched.Mode {
	case ModeDynamic:
		newNext = formatTime(now.Add(s.opts.DynamicFallback))
	case ModeRecurring:
		spec, err := ParseSpec(sched.Cron)
		if err != nil {
			return s.pauseSchedule(ctx, sched.ID, PauseInvalidSpec, err.Error())
		}
		next := spec.Next(now, s.opts.Loc)
		if next.IsZero() {
			return s.pauseSchedule(ctx, sched.ID, PauseInvalidSpec, "cron expression has no future occurrence")
		}
		newNext = formatTime(next)
	}
	slot, ok := parseTime(sched.NextRunAt)
	if !ok {
		slot = now
	}
	txErr := sqliteops.RetryWrite(func() error {
		return store.RunInTx(s.db, func(q *store.Queries) error {
			if _, err := q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
				ID:           newID(),
				ScheduleID:   sched.ID,
				SessionID:    sched.SessionID,
				ScheduledFor: formatTime(slot),
				CreatedAt:    formatTime(now),
				Status:       RunSkipped,
				Reason:       reason,
			}); err != nil {
				return err
			}
			rows, err := q.AdvanceScheduleNextRunIfEnabled(ctx, store.AdvanceScheduleNextRunIfEnabledParams{
				NextRunAt: newNext,
				LastRunAt: sched.LastRunAt,
				UpdatedAt: formatTime(now),
				ID:        sched.ID,
			})
			if err != nil {
				return err
			}
			if rows == 0 {
				return errSchedulePausedRace
			}
			return nil
		})
	})
	if errors.Is(txErr, errSchedulePausedRace) {
		return nil
	}
	if txErr != nil {
		return fmt.Errorf("record skipped slot: %w", txErr)
	}
	s.pushSchedule(ctx, sched.ID)
	return nil
}

// attemptDelivery delivers one queued run: idle-gated (busy sessions requeue
// for the next tick or idle-boundary callback), single-flight (per-session
// mutex + queued→firing CAS on the run row), finished-session guarded.
func (s *Scheduler) attemptDelivery(ctx context.Context, runID string) {
	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil {
		slog.Error("scheduler: load run failed", "run_id", runID, "error", err)
		return
	}
	if run.Status != RunQueued {
		return
	}
	sched, err := s.q.GetSchedule(ctx, run.ScheduleID)
	if err != nil {
		return // schedule deleted; FK cascade removes the runs
	}
	if sched.Enabled == 0 && run.Reason != runNowReason {
		// A queued run that raced past pauseSchedule's sweep must not strand
		// queued forever. Run-now runs stay deliverable on a paused schedule.
		s.resolveRun(ctx, run.ID, resolution{status: RunSkipped, reason: "schedule paused"})
		return
	}
	s.deliverClaimed(ctx, run, sched)
}

// deliverClaimed is the delivery core shared by cadence fires and run-now
// (which skips the enabled gate — running a paused schedule by hand is valid).
func (s *Scheduler) deliverClaimed(ctx context.Context, run store.ScheduleRun, sched store.Schedule) {
	runID := run.ID
	lock := s.sessionLock(run.SessionID)
	lock.Lock()
	defer lock.Unlock()
	select {
	case <-s.done:
		return
	default:
	}

	var rows int64
	if err := sqliteops.RetryWrite(func() error {
		var werr error
		rows, werr = s.q.ClaimScheduleRun(ctx, runID)
		return werr
	}); err != nil || rows == 0 {
		return
	}
	// Re-read after winning the claim: the row loaded before the per-session
	// lock may be stale (another deliverer bumped attempts/backoff meanwhile).
	run, runErr := s.q.GetScheduleRun(ctx, runID)
	if runErr != nil {
		return
	}
	if run.NextAttemptAt != "" && run.NextAttemptAt > formatTime(s.opts.Now().UTC()) {
		// Delivery backoff still pending — hand the claim back.
		s.requeueRun(ctx, runID, run.Attempts, run.NextAttemptAt)
		return
	}

	// Re-read the schedule under the claim: a Delete/Update racing the
	// pre-lock load must not fire a stale prompt (a deleted schedule
	// cascade-removed this run; nothing to resolve).
	sched, schedErr := s.q.GetSchedule(ctx, run.ScheduleID)
	if schedErr != nil {
		return
	}

	finished, err := s.gw.SessionFinished(ctx, run.SessionID)
	if err == nil && finished {
		s.resolveRun(ctx, runID, resolution{status: RunSkipped, reason: "session completed"})
		if perr := s.pauseSchedule(ctx, run.ScheduleID, PauseSessionCompleted, ""); perr != nil {
			slog.Error("scheduler: pause on finished session failed", "schedule_id", run.ScheduleID, "error", perr)
		}
		return
	}

	origin := session.QueryOrigin{
		Kind:         "schedule",
		ScheduleID:   sched.ID,
		RunID:        runID,
		ScheduleName: sched.Name,
	}
	turnIndex, outcome, err := s.gw.Deliver(ctx, run.SessionID, sched.Prompt+reportFooter(runID), origin)
	now := s.opts.Now().UTC()
	if err != nil {
		if errors.Is(err, session.ErrSessionFinished) {
			// The atomic turn-start check caught a mark-done/merge that raced
			// the pre-delivery DB read — same treatment as the DB check.
			s.resolveRun(ctx, runID, resolution{status: RunSkipped, reason: "session completed"})
			if perr := s.pauseSchedule(ctx, run.ScheduleID, PauseSessionCompleted, ""); perr != nil {
				slog.Error("scheduler: pause on finished session failed", "schedule_id", run.ScheduleID, "error", perr)
			}
			return
		}
		if errors.Is(err, session.ErrBusy) || errors.Is(err, session.ErrNotLive) {
			// Transient: back to queued without consuming an attempt; the
			// idle-boundary callback or the next tick retries.
			s.requeueRun(ctx, runID, run.Attempts, "")
			return
		}
		attempts := run.Attempts + 1
		if attempts >= maxDeliveryAttempts {
			s.resolveRun(ctx, runID, resolution{status: RunError, errText: firstLine(fmt.Sprintf("delivery failed after %d attempts: %v", attempts, err), 500)})
			s.countFailure(ctx, sched.ID, runID)
			return
		}
		backoff := deliveryBackoff[min(int(attempts)-1, len(deliveryBackoff)-1)]
		s.requeueRun(ctx, runID, attempts, formatTime(now.Add(backoff)))
		s.pushRun(ctx, runID)
		return
	}

	if err := sqliteops.RetryWrite(func() error {
		return s.q.MarkScheduleRunFired(ctx, store.MarkScheduleRunFiredParams{
			FiredAt:   formatTime(now),
			TurnIndex: int64(turnIndex),
			Attempts:  run.Attempts + 1,
			ID:        runID,
		})
	}); err != nil {
		// Turn delivered but the row stayed `firing` with no fired_at: the
		// outcome waiter still resolves it (ResolveScheduleRun accepts
		// firing), and the stuck-firing reclaim's redelivery converges via
		// the busy refusal. Rare^2; log and continue.
		slog.Error("scheduler: mark fired failed", "run_id", runID, "error", err)
	}
	s.pushRun(ctx, runID)

	s.spawn(func() { s.waitForOutcome(runID, sched.ID, outcome) })
}

func (s *Scheduler) waitForOutcome(runID, scheduleID string, outcome <-chan session.TurnOutcome) {
	select {
	case out := <-outcome:
		s.resolveOutcome(context.Background(), runID, scheduleID, out)
	case <-s.done:
		// Server stopping mid-run: the next boot's sweep reconciles.
	}
}

// OnSessionIdle delivers any queued runs for the session at its idle
// boundary. Wired to session.Manager.OnSessionIdle.
func (s *Scheduler) OnSessionIdle(sessionID string) {
	runs, err := s.q.ListQueuedRunsBySession(context.Background(), sessionID)
	if err != nil {
		slog.Error("scheduler: list queued runs failed", "session_id", sessionID, "error", err)
		return
	}
	now := formatTime(s.opts.Now().UTC())
	for _, r := range runs {
		if r.NextAttemptAt != "" && r.NextAttemptAt > now {
			continue // delivery backoff still pending
		}
		id := r.ID
		s.spawn(func() { s.attemptDelivery(context.Background(), id) })
	}
}

// OnSessionFinished pauses the session's schedules on user-intent completion
// (mark-done, completing merge). Wired to session.Service.SetOnSessionFinished.
func (s *Scheduler) OnSessionFinished(sessionID string) {
	s.PauseSchedulesForSession(context.Background(), sessionID, PauseSessionCompleted)
}

// PauseSchedulesForSession pauses every enabled schedule targeting the
// session and resolves their queued runs as skipped.
func (s *Scheduler) PauseSchedulesForSession(ctx context.Context, sessionID, reason string) {
	scheds, err := s.q.ListEnabledSchedulesBySession(ctx, sessionID)
	if err != nil {
		slog.Error("scheduler: list session schedules failed", "session_id", sessionID, "error", err)
		return
	}
	for _, sched := range scheds {
		if err := s.pauseSchedule(ctx, sched.ID, reason, ""); err != nil {
			slog.Error("scheduler: pause failed", "schedule_id", sched.ID, "error", err)
		}
	}
}

// retryAndOverduePass retries deliverable queued runs and flags overdue
// running runs — resolving them action_needed when the turn is blocked on a
// human rather than counting a fake failure.
func (s *Scheduler) retryAndOverduePass(ctx context.Context, now time.Time) {
	runs, err := s.q.ListUnfinishedScheduleRuns(ctx)
	if err != nil {
		slog.Error("scheduler: list unfinished runs failed", "error", err)
		return
	}
	// Stuck-firing reclamation bookkeeping: rebuild the observation set each
	// pass so entries for runs that moved on don't accumulate.
	currentFiring := make(map[string]struct{})
	for _, r := range runs {
		if r.Status == RunFiring {
			currentFiring[r.ID] = struct{}{}
		}
	}
	for id := range s.firingSeen {
		if _, still := currentFiring[id]; !still {
			delete(s.firingSeen, id)
		}
	}

	nowStr := formatTime(now)
	for _, r := range runs {
		switch r.Status {
		case RunFiring:
			// A firing row is transitional for milliseconds; one that
			// survives multiple sweeps lost its deliverer to a write failure
			// after the claim. Reclaim to queued so the loop doesn't wedge on
			// "previous run still running" until a restart. Delivered runs
			// (fired_at set) are owned by their outcome waiter — skip.
			if r.FiredAt != "" {
				continue
			}
			first, seen := s.firingSeen[r.ID]
			if !seen {
				s.firingSeen[r.ID] = now
				continue
			}
			if now.Sub(first) < firingReclaimAfter {
				continue
			}
			delete(s.firingSeen, r.ID)
			s.requeueRun(ctx, r.ID, r.Attempts, "")
		case RunQueued:
			if r.NextAttemptAt != "" && r.NextAttemptAt > nowStr {
				continue
			}
			id := r.ID
			s.spawn(func() { s.attemptDelivery(context.Background(), id) })
		case RunRunning:
			fired, ok := parseTime(r.FiredAt)
			if r.FiredAt == "" || !ok {
				continue
			}
			elapsed := now.Sub(fired)
			// Blocked-on-human detection runs after a short grace, not the
			// full run-duration bound — a fire that opens an approval in its
			// first minute must surface action_needed promptly, not sit
			// "running" for 30 minutes.
			if elapsed >= pendingHumanGrace {
				if pending := s.gw.PendingHumanInput(r.SessionID); pending != "" {
					// Blocked on a human, not failing: resolve honestly. A late
					// completion lands in late_report via the terminal-once guard.
					s.resolveRun(ctx, r.ID, resolution{
						status:     RunActionNeeded,
						reason:     "waiting on " + pending,
						durationMS: elapsed.Milliseconds(),
					})
					s.setAttention(ctx, r.ScheduleID, AttentionActionNeeded, r.ID)
					continue
				}
			}
			if r.Overdue != 0 || elapsed < s.opts.MaxRunDuration {
				continue
			}
			if err := sqliteops.RetryWrite(func() error {
				return s.q.SetScheduleRunOverdue(ctx, r.ID)
			}); err != nil {
				slog.Error("scheduler: set overdue failed", "run_id", r.ID, "error", err)
				continue
			}
			s.setAttention(ctx, r.ScheduleID, AttentionActionNeeded, r.ID)
			s.pushRun(ctx, r.ID)
		}
	}
}

// resolveOutcome maps a turn outcome onto the run lifecycle and the
// schedule's failure accounting.
func (s *Scheduler) resolveOutcome(ctx context.Context, runID, scheduleID string, out session.TurnOutcome) {
	sched, err := s.q.GetSchedule(ctx, scheduleID)
	if err != nil {
		slog.Error("scheduler: load schedule for outcome failed", "schedule_id", scheduleID, "error", err)
		return
	}
	run, err := s.q.GetScheduleRun(ctx, runID)
	if err != nil {
		slog.Error("scheduler: load run for outcome failed", "run_id", runID, "error", err)
		return
	}
	now := s.opts.Now().UTC()

	var res resolution
	countsAsFailure := false
	switch {
	case out.SessionClosed:
		// Stop/shutdown mid-run — not the loop's fault; excluded from
		// auto-pause like the boot sweep's restart errors.
		res = resolution{status: RunError, errText: "session stopped before the turn completed"}

	case out.Status == runtime.TurnStatusInterrupted:
		res = resolution{status: RunInterrupted, reason: "interrupted by user", durationMS: out.Duration.Milliseconds()}

	case out.ErrorKind == session.ErrorKindRateLimit || out.ErrorKind == session.ErrorKindOverloaded:
		// Transient provider condition: defer, never fail. Reschedule at the
		// reset time (when known) or the minimum interval.
		next := now.Add(s.opts.MinInterval)
		if out.RateLimitResetsAt > 0 {
			if reset := time.Unix(out.RateLimitResetsAt, 0).UTC(); reset.After(next) {
				next = reset
			}
		}
		res = resolution{
			status:     RunDeferred,
			reason:     "provider " + out.ErrorKind + "; rescheduled",
			errKind:    out.ErrorKind,
			durationMS: out.Duration.Milliseconds(),
		}
		if resolved, resErr := s.resolveRun(ctx, runID, res); resErr == nil && resolved {
			// Enabled-guarded: a pause landing after the fire must keep the
			// schedule parked — a deferred reschedule must not re-arm it.
			// (A racing cadence advance can still overwrite this anchor;
			// last-writer-wins between two legitimate future fire times is
			// accepted — both are sane, the loop stays alive either way.)
			if _, err := s.q.AdvanceScheduleNextRunIfEnabled(ctx, store.AdvanceScheduleNextRunIfEnabledParams{
				NextRunAt: formatTime(next),
				LastRunAt: sched.LastRunAt,
				UpdatedAt: formatTime(now),
				ID:        scheduleID,
			}); err != nil {
				slog.Error("scheduler: defer reschedule failed", "schedule_id", scheduleID, "error", err)
			}
			s.pushSchedule(ctx, scheduleID)
		}
		return

	case out.Status == runtime.TurnStatusFailed:
		res = resolution{status: RunError, errText: firstLine(out.FinalText, 500), errKind: out.ErrorKind, durationMS: out.Duration.Milliseconds()}
		countsAsFailure = true

	case out.Status == runtime.TurnStatusMaxTurns:
		res = resolution{status: RunActionNeeded, reason: "turn hit the max-turns bound", durationMS: out.Duration.Milliseconds()}

	default: // completed
		res = resolution{status: RunOK, summary: firstLine(out.FinalText, 240), durationMS: out.Duration.Milliseconds()}
	}

	// An agent-volunteered ScheduleReport wins over the ok/error text
	// fallback — never over transient deferral, a human interrupt, or a
	// closed session (those classify infrastructure, not the work's outcome).
	if rep, ok := s.takeReport(runID); ok {
		switch res.status {
		case RunOK, RunError, RunActionNeeded:
			res.status = rep.status
			res.summary = rep.summary
			res.reason = "reported by agent"
			res.errText = ""
			if rep.status == RunError {
				res.errText = rep.summary
			}
			countsAsFailure = rep.status == RunError
		}
	}

	resolved, resErr := s.resolveRun(ctx, runID, res)
	if resErr != nil {
		// The write itself failed — the run is NOT terminal; annotating it as
		// a late completion would be a lie. The stuck-firing/overdue sweeps
		// and the boot sweep own reconciliation from here.
		return
	}
	if !resolved {
		// Terminal-once guard hit: the run was already resolved (overdue →
		// action_needed, or the boot sweep). Record the late outcome as an
		// annotation without touching status or counters.
		late := fmt.Sprintf("late completion (%s): %s", res.status, firstLine(out.FinalText, 240))
		if err := sqliteops.RetryWrite(func() error {
			return s.q.AppendScheduleRunLateReport(ctx, store.AppendScheduleRunLateReportParams{LateReport: late, ID: runID})
		}); err != nil {
			slog.Error("scheduler: append late report failed", "run_id", runID, "error", err)
		}
		return
	}

	switch res.status {
	case RunOK:
		s.resetFailures(ctx, sched)
	case RunActionNeeded:
		s.resetFailures(ctx, sched)
		s.setAttention(ctx, scheduleID, AttentionActionNeeded, runID)
	case RunError:
		if countsAsFailure {
			s.countFailure(ctx, scheduleID, runID)
		}
	}

	// Only the cadence fire consumes a one-shot — an ad-hoc run-now on a
	// pending reminder must not destroy it.
	if sched.Mode == ModeOnce && run.Reason != runNowReason {
		if err := s.pauseSchedule(ctx, scheduleID, PauseCompleted, ""); err != nil {
			slog.Error("scheduler: park once-schedule failed", "schedule_id", scheduleID, "error", err)
		}
	}
	s.pushSchedule(ctx, scheduleID)
}

type resolution struct {
	status     string
	summary    string
	reason     string
	errText    string
	errKind    string
	durationMS int64
}

// resolveRun writes a terminal status through the write-retry layer. The
// bool is true when THIS call resolved the run; false with a nil error means
// the run was already terminal (the DB-level one-way guard). A non-nil error
// means the write itself failed — callers must not treat that as "already
// terminal" (the late-report path would annotate a still-running run).
func (s *Scheduler) resolveRun(ctx context.Context, runID string, res resolution) (bool, error) {
	var rows int64
	err := sqliteops.RetryWrite(func() error {
		var werr error
		rows, werr = s.q.ResolveScheduleRun(ctx, store.ResolveScheduleRunParams{
			Status:     res.status,
			FinishedAt: formatTime(s.opts.Now().UTC()),
			Summary:    res.summary,
			Reason:     res.reason,
			Error:      res.errText,
			ErrorKind:  res.errKind,
			DurationMs: res.durationMS,
			ID:         runID,
		})
		return werr
	})
	if err != nil {
		slog.Error("scheduler: resolve run failed", "run_id", runID, "error", err)
		return false, err
	}
	if rows > 0 {
		s.pushRun(ctx, runID)
	}
	return rows > 0, nil
}

// requeueRun writes a run back to queued through the write-retry layer.
func (s *Scheduler) requeueRun(ctx context.Context, runID string, attempts int64, nextAttemptAt string) {
	if err := sqliteops.RetryWrite(func() error {
		return s.q.RequeueScheduleRun(ctx, store.RequeueScheduleRunParams{
			Attempts:      attempts,
			NextAttemptAt: nextAttemptAt,
			ID:            runID,
		})
	}); err != nil {
		slog.Error("scheduler: requeue failed", "run_id", runID, "error", err)
	}
}

func (s *Scheduler) countFailure(ctx context.Context, scheduleID, runID string) {
	sched, err := s.q.GetSchedule(ctx, scheduleID)
	if err != nil {
		return
	}
	failures := sched.ConsecutiveFailures + 1
	now := formatTime(s.opts.Now().UTC())
	if err := s.q.SetScheduleFailures(ctx, store.SetScheduleFailuresParams{
		ConsecutiveFailures: failures,
		UpdatedAt:           now,
		ID:                  scheduleID,
	}); err != nil {
		slog.Error("scheduler: bump failures failed", "schedule_id", scheduleID, "error", err)
		return
	}
	if int(failures) >= s.opts.MaxConsecutiveFailures {
		// A broken loop degrades loudly to paused, never to a retry storm.
		// pauseSchedule (not a bare disable) so queued runs resolve skipped
		// like every other pause path.
		if err := s.pauseSchedule(ctx, scheduleID, PauseAutoFailures, ""); err != nil {
			slog.Error("scheduler: auto-pause failed", "schedule_id", scheduleID, "error", err)
		}
		s.setAttention(ctx, scheduleID, AttentionFailed, runID)
	}
	s.pushSchedule(ctx, scheduleID)
}

func (s *Scheduler) resetFailures(ctx context.Context, sched store.Schedule) {
	now := formatTime(s.opts.Now().UTC())
	if sched.ConsecutiveFailures != 0 {
		if err := s.q.SetScheduleFailures(ctx, store.SetScheduleFailuresParams{
			ConsecutiveFailures: 0,
			UpdatedAt:           now,
			ID:                  sched.ID,
		}); err != nil {
			slog.Error("scheduler: reset failures failed", "schedule_id", sched.ID, "error", err)
		}
	}
	// A later healthy run self-heals a view-clearable attention.
	if err := s.q.ClearScheduleActionAttention(ctx, store.ClearScheduleActionAttentionParams{
		UpdatedAt: now,
		ID:        sched.ID,
	}); err != nil {
		slog.Error("scheduler: clear attention failed", "schedule_id", sched.ID, "error", err)
	}
}

func (s *Scheduler) pauseSchedule(ctx context.Context, scheduleID, reason, detail string) error {
	now := formatTime(s.opts.Now().UTC())
	if err := s.q.SetScheduleEnabled(ctx, store.SetScheduleEnabledParams{
		Enabled:     0,
		PauseReason: reason,
		NextRunAt:   "",
		UpdatedAt:   now,
		ID:          scheduleID,
	}); err != nil {
		return fmt.Errorf("pause schedule: %w", err)
	}
	if reason == PauseInvalidSpec {
		s.setAttention(ctx, scheduleID, AttentionFailed, "")
		if detail != "" {
			slog.Warn("scheduler: schedule paused as invalid", "schedule_id", scheduleID, "detail", detail)
		}
	}
	// Pausing resolves any still-queued runs — nothing may sit "waiting for
	// idle" on a paused loop.
	runs, err := s.q.ListUnfinishedRunsForSchedule(ctx, scheduleID)
	if err == nil {
		for _, r := range runs {
			if r.Status == RunQueued {
				s.resolveRun(ctx, r.ID, resolution{status: RunSkipped, reason: "schedule paused"})
			}
		}
	}
	s.pushSchedule(ctx, scheduleID)
	return nil
}

func (s *Scheduler) setAttention(ctx context.Context, scheduleID, attention, runID string) {
	if err := s.q.SetScheduleAttention(ctx, store.SetScheduleAttentionParams{
		Attention:      attention,
		AttentionRunID: runID,
		UpdatedAt:      formatTime(s.opts.Now().UTC()),
		ID:             scheduleID,
	}); err != nil {
		slog.Error("scheduler: set attention failed", "schedule_id", scheduleID, "error", err)
	}
	s.pushSchedule(ctx, scheduleID)
}

// BootSweep reconciles runs stranded by an ungraceful exit. Called from
// serve.go strictly before Start (never from a constructor): delivered runs
// (`firing`/`running`) lost their CLI with the server — resolved as errors
// but excluded from auto-pause so a crash-loop can't kill every schedule;
// never-delivered `queued` runs are left in place for the first tick.
func (s *Scheduler) BootSweep(ctx context.Context) {
	runs, err := s.q.ListUnfinishedScheduleRuns(ctx)
	if err != nil {
		slog.Error("scheduler: boot sweep list failed", "error", err)
		return
	}
	for _, r := range runs {
		if r.Status != RunFiring && r.Status != RunRunning {
			continue
		}
		s.resolveRun(ctx, r.ID, resolution{status: RunError, errText: "server restarted mid-run"})
		// A one-shot whose delivered run died with the server would otherwise
		// strand as an enabled-but-parked zombie (next_run_at was cleared at
		// fire time; only resolveOutcome parks it) — park it here.
		if sched, err := s.q.GetSchedule(ctx, r.ScheduleID); err == nil &&
			sched.Mode == ModeOnce && sched.Enabled != 0 && sched.NextRunAt == "" {
			if perr := s.pauseSchedule(ctx, r.ScheduleID, PauseCompleted, ""); perr != nil {
				slog.Error("scheduler: park once-schedule in sweep failed", "schedule_id", r.ScheduleID, "error", perr)
			}
		}
	}
}

func (s *Scheduler) pruneRuns(ctx context.Context, scheduleID string) {
	if err := s.q.PruneScheduleRuns(ctx, store.PruneScheduleRunsParams{
		ScheduleID:   scheduleID,
		ScheduleID_2: scheduleID,
		Limit:        int64(s.opts.RunHistory),
	}); err != nil {
		slog.Error("scheduler: prune runs failed", "schedule_id", scheduleID, "error", err)
	}
}

func (s *Scheduler) sessionLock(sessionID string) *sync.Mutex {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	l, ok := s.sessionLocks[sessionID]
	if !ok {
		l = &sync.Mutex{}
		s.sessionLocks[sessionID] = l
	}
	return l
}

// insertRunRow writes a standalone (already-terminal) run row outside the
// fire transaction — used for the stale-once skip.
func (s *Scheduler) insertRunRow(ctx context.Context, sched store.Schedule, slot time.Time, status, reason string, now time.Time) {
	if _, err := s.q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
		ID:           newID(),
		ScheduleID:   sched.ID,
		SessionID:    sched.SessionID,
		ScheduledFor: formatTime(slot),
		CreatedAt:    formatTime(now),
		Status:       status,
		Reason:       reason,
	}); err != nil {
		slog.Error("scheduler: insert run row failed", "schedule_id", sched.ID, "error", err)
	}
}

func mustParse(s string) time.Time {
	t, _ := parseTime(s)
	return t
}

// reportFooter is appended to every fired prompt so the agent can volunteer
// a structured outcome via the ScheduleReport MCP tool. The [scheduled-run:…]
// marker is machine-recognizable for future frontend peeling; the runId makes
// attribution exact even when human turns interleave on the shared session.
func reportFooter(runID string) string {
	return fmt.Sprintf("\n\n[scheduled-run:%s] When this run's work is done, call the ScheduleReport tool with runId %q, status ok|action-needed|failed, and a one-line summary of the outcome.", runID, runID)
}

// firstLine compresses text into a single-line summary of at most maxLen
// runes: whitespace collapsed, taken from the head (the lead sentence is
// where the outcome lives).
func firstLine(text string, maxLen int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen-1]) + "…"
}
