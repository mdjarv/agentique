package schedule

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// --- test fixture ---

type fakeDelivery struct {
	sessionID string
	prompt    string
	origin    session.QueryOrigin
	outcome   chan session.TurnOutcome
	turnIndex int
}

type fakeGateway struct {
	mu         sync.Mutex
	deliveries []*fakeDelivery
	deliverErr error
	finished   bool
	pending    string
	nextTurn   int
}

func (g *fakeGateway) Deliver(_ context.Context, sessionID, prompt string, origin session.QueryOrigin) (int, <-chan session.TurnOutcome, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.deliverErr != nil {
		return 0, nil, g.deliverErr
	}
	g.nextTurn++
	d := &fakeDelivery{
		sessionID: sessionID,
		prompt:    prompt,
		origin:    origin,
		outcome:   make(chan session.TurnOutcome, 1),
		turnIndex: g.nextTurn,
	}
	g.deliveries = append(g.deliveries, d)
	return d.turnIndex, d.outcome, nil
}

func (g *fakeGateway) SessionFinished(context.Context, string) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.finished, nil
}

func (g *fakeGateway) PendingHumanInput(string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pending
}

func (g *fakeGateway) deliveryCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.deliveries)
}

func (g *fakeGateway) lastDelivery() *fakeDelivery {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.deliveries) == 0 {
		return nil
	}
	return g.deliveries[len(g.deliveries)-1]
}

type fixture struct {
	t     *testing.T
	db    *sql.DB
	q     *store.Queries
	gw    *fakeGateway
	sched *Scheduler
	// now is the mutable test clock.
	nowMu sync.Mutex
	now   time.Time
}

func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatal(err)
	}
	q := store.New(db)
	f := &fixture{t: t, db: db, q: q, gw: &fakeGateway{}, now: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)}
	opts.Loc = time.UTC
	opts.Now = f.clock
	f.sched = NewScheduler(db, q, f.gw, nil, opts)
	t.Cleanup(f.sched.Stop)

	ctx := context.Background()
	if _, err := q.CreateProject(ctx, store.CreateProjectParams{ID: "p1", Name: "p", Path: "/tmp/p", Slug: "p"}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateSession(ctx, store.CreateSessionParams{
		ID: "s1", ProjectID: "p1", Name: "s", WorkDir: "/tmp/p", State: "idle",
		Model: "opus", PermissionMode: "default", AutoApproveMode: "manual", Provider: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) clock() time.Time {
	f.nowMu.Lock()
	defer f.nowMu.Unlock()
	return f.now
}

func (f *fixture) advance(d time.Duration) {
	f.nowMu.Lock()
	f.now = f.now.Add(d)
	f.nowMu.Unlock()
}

func (f *fixture) createSchedule(t *testing.T, mode, cron, nextRunAt string) store.Schedule {
	t.Helper()
	row, err := f.q.CreateSchedule(context.Background(), store.CreateScheduleParams{
		ID: newID(), ProjectID: "p1", SessionID: "s1", Name: "loop", Prompt: "do the thing",
		Cron: cron, Mode: mode, Enabled: 1, NextRunAt: nextRunAt,
		CreatedBy: "user", CreatedAt: formatTime(f.clock()), UpdatedAt: formatTime(f.clock()),
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// waitFor polls until cond returns true or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *fixture) runByStatus(status string) *store.ScheduleRun {
	// QueryRow (not Query) — store.Open pins a single SQLite connection, so an
	// open cursor would deadlock the follow-up GetScheduleRun.
	var id string
	err := f.db.QueryRow("SELECT id FROM schedule_runs WHERE status = ?", status).Scan(&id)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	run, err := f.q.GetScheduleRun(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return &run
}

// --- tests ---

func TestFire_RecurringDeliversAndResolvesOK(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })

	d := f.gw.lastDelivery()
	if d.sessionID != "s1" || !strings.HasPrefix(d.prompt, "do the thing") {
		t.Errorf("unexpected delivery %+v", d)
	}
	if !strings.Contains(d.prompt, "[scheduled-run:") {
		t.Error("fired prompt must carry the ScheduleReport footer")
	}
	if d.origin.Kind != "schedule" || d.origin.ScheduleID != sched.ID || d.origin.RunID == "" {
		t.Errorf("unexpected origin %+v", d.origin)
	}

	waitFor(t, "run running", func() bool { return f.runByStatus(RunRunning) != nil })
	d.outcome <- session.TurnOutcome{
		TurnIndex: d.turnIndex,
		Status:    runtime.TurnStatusCompleted,
		FinalText: "PR is green, nothing to do.\nDetails follow.",
		Duration:  4 * time.Second,
	}
	waitFor(t, "run ok", func() bool { return f.runByStatus(RunOK) != nil })

	run := f.runByStatus(RunOK)
	if run.Summary != "PR is green, nothing to do. Details follow." {
		t.Errorf("summary = %q", run.Summary)
	}
	if run.TurnIndex != int64(d.turnIndex) || run.DurationMs != 4000 {
		t.Errorf("run identity/duration: %+v", run)
	}

	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.NextRunAt != "2026-07-30T13:00:00Z" {
		t.Errorf("next_run_at = %q, want 13:00Z", got.NextRunAt)
	}
	if got.LastRunAt == "" {
		t.Error("last_run_at not stamped")
	}
}

func TestFire_CatchUpFiresMostRecentMissedSlot(t *testing.T) {
	f := newFixture(t, Options{})
	f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T08:00:00Z") // 4 slots behind (08,09,10,11)

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })

	var fired, skipped store.ScheduleRun
	waitFor(t, "rows", func() bool {
		fr := f.runByStatus(RunRunning)
		sk := f.runByStatus(RunSkipped)
		if fr == nil || sk == nil {
			return false
		}
		fired, skipped = *fr, *sk
		return true
	})
	// now is exactly 12:00, so the 12:00 occurrence is due (not missed) and
	// is the freshest slot to satisfy; 08:00–11:00 collapse into one skip row.
	if fired.ScheduledFor != "2026-07-30T12:00:00Z" {
		t.Errorf("fired slot = %q, want the freshest due occurrence (12:00Z)", fired.ScheduledFor)
	}
	if skipped.ScheduledFor != "2026-07-30T08:00:00Z" || skipped.Reason == "" {
		t.Errorf("aggregate skip row wrong: %+v", skipped)
	}
}

func TestFire_BusyRequeuesThenIdleDelivers(t *testing.T) {
	f := newFixture(t, Options{})
	f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.gw.mu.Lock()
	f.gw.deliverErr = session.ErrBusy
	f.gw.mu.Unlock()

	f.sched.tick()
	// The run flaps queued→firing→queued while the busy refusal bounces, so
	// capture the row inside the wait rather than re-reading after it.
	var r store.ScheduleRun
	waitFor(t, "requeued", func() bool {
		got := f.runByStatus(RunQueued)
		if got == nil {
			return false
		}
		r = *got
		return true
	})
	if r.Attempts != 0 {
		t.Errorf("busy refusal must not consume attempts, got %d", r.Attempts)
	}

	f.gw.mu.Lock()
	f.gw.deliverErr = nil
	f.gw.mu.Unlock()
	f.sched.OnSessionIdle("s1")
	waitFor(t, "delivered at idle", func() bool { return f.gw.deliveryCount() == 1 })
}

func TestFire_HardDeliveryErrorRetriesThenFails(t *testing.T) {
	f := newFixture(t, Options{MaxConsecutiveFailures: 99})
	f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.gw.mu.Lock()
	f.gw.deliverErr = context.DeadlineExceeded
	f.gw.mu.Unlock()

	f.sched.tick() // attempt 1 → backoff
	waitFor(t, "attempt 1", func() bool {
		r := f.runByStatus(RunQueued)
		return r != nil && r.Attempts == 1 && r.NextAttemptAt != ""
	})

	f.advance(time.Minute) // past the 30s backoff
	f.sched.tick()         // attempt 2 → backoff
	waitFor(t, "attempt 2", func() bool {
		r := f.runByStatus(RunQueued)
		return r != nil && r.Attempts == 2
	})

	f.advance(3 * time.Minute)
	f.sched.tick() // attempt 3 → resolve error
	waitFor(t, "resolved error", func() bool { return f.runByStatus(RunError) != nil })
}

func TestFire_PreviousRunningSkipsSlot(t *testing.T) {
	f := newFixture(t, Options{})
	// Exactly due (no catch-up walk) so the later skip row is unambiguous.
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T12:00:00Z")

	f.sched.tick()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	// Next slot due while the run is still in flight.
	f.advance(time.Hour + time.Minute)
	f.sched.tick()
	waitFor(t, "skipped slot", func() bool {
		r := f.runByStatus(RunSkipped)
		return r != nil && r.Reason == "previous run still running"
	})
	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.NextRunAt != "2026-07-30T14:00:00Z" {
		t.Errorf("next_run_at = %q after skip, want 14:00Z", got.NextRunAt)
	}
}

func TestResolve_ErrorAutoPausesAfterThreshold(t *testing.T) {
	f := newFixture(t, Options{MaxConsecutiveFailures: 2})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	for i := 0; i < 2; i++ {
		f.sched.tick()
		waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == i+1 })
		d := f.gw.lastDelivery()
		waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })
		d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusFailed, FinalText: "boom"}
		waitFor(t, "resolved", func() bool { return f.runByStatus(RunRunning) == nil })
		f.advance(2 * time.Hour)
	}

	// Pause and attention are two writes; poll for the settled state.
	waitFor(t, "auto-pause with failed attention", func() bool {
		got, _ := f.q.GetSchedule(context.Background(), sched.ID)
		return got.Enabled == 0 && got.PauseReason == PauseAutoFailures && got.Attention == AttentionFailed
	})
}

func TestResolve_DeferredOnRateLimitDoesNotCountFailure(t *testing.T) {
	f := newFixture(t, Options{MaxConsecutiveFailures: 1})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	reset := f.clock().Add(90 * time.Minute).Unix()
	d.outcome <- session.TurnOutcome{
		TurnIndex:         d.turnIndex,
		Status:            runtime.TurnStatusFailed,
		ErrorKind:         session.ErrorKindRateLimit,
		RateLimitResetsAt: reset,
	}
	waitFor(t, "deferred", func() bool { return f.runByStatus(RunDeferred) != nil })

	// Resolving the run and re-anchoring the schedule are two writes, and the
	// run status lands first — so a bare read here can still see the cadence's
	// next slot. Poll for the settled state.
	want := formatTime(time.Unix(reset, 0))
	waitFor(t, "reschedule at the rate-limit reset", func() bool {
		got, err := f.q.GetSchedule(context.Background(), sched.ID)
		return err == nil && got.NextRunAt == want
	})

	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.Enabled != 1 || got.ConsecutiveFailures != 0 {
		t.Errorf("deferred must not fail the loop: %+v", got)
	}
}

func TestResolve_InterruptedDoesNotCountFailure(t *testing.T) {
	f := newFixture(t, Options{MaxConsecutiveFailures: 1})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusInterrupted}
	waitFor(t, "interrupted", func() bool { return f.runByStatus(RunInterrupted) != nil })

	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.Enabled != 1 {
		t.Error("interrupt must not pause the loop")
	}
}

func TestOverdue_PendingHumanResolvesActionNeeded(t *testing.T) {
	f := newFixture(t, Options{MaxRunDuration: 10 * time.Minute})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	f.gw.mu.Lock()
	f.gw.pending = "approval: Bash"
	f.gw.mu.Unlock()
	f.advance(11 * time.Minute)
	f.sched.tick()

	waitFor(t, "action_needed", func() bool { return f.runByStatus(RunActionNeeded) != nil })
	run := f.runByStatus(RunActionNeeded)
	if run.Reason != "waiting on approval: Bash" {
		t.Errorf("reason = %q", run.Reason)
	}
	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.Attention != AttentionActionNeeded || got.ConsecutiveFailures != 0 {
		t.Errorf("schedule after action_needed: %+v", got)
	}

	// The turn eventually completes → late report, status untouched.
	d := f.gw.lastDelivery()
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "done late"}
	waitFor(t, "late report", func() bool {
		r := f.runByStatus(RunActionNeeded)
		return r != nil && r.LateReport != ""
	})
}

func TestOverdue_NoPendingFlagsRun(t *testing.T) {
	f := newFixture(t, Options{MaxRunDuration: 10 * time.Minute})
	f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })
	f.advance(11 * time.Minute)
	f.sched.tick()

	waitFor(t, "overdue flag", func() bool {
		r := f.runByStatus(RunRunning)
		return r != nil && r.Overdue == 1
	})
}

func TestBootSweep_ErrorsDeliveredRunsKeepsQueued(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")
	ctx := context.Background()

	for i, status := range []string{RunRunning, RunQueued} {
		if _, err := f.q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
			ID: newID(), ScheduleID: sched.ID, SessionID: "s1",
			ScheduledFor: formatTime(f.clock().Add(time.Duration(-i) * time.Hour)),
			CreatedAt:    formatTime(f.clock()), Status: RunQueued,
		}); err != nil {
			t.Fatal(err)
		}
		if status == RunRunning {
			r := f.runByStatus(RunQueued)
			if _, err := f.q.ClaimScheduleRun(ctx, r.ID); err != nil {
				t.Fatal(err)
			}
			if err := f.q.MarkScheduleRunFired(ctx, store.MarkScheduleRunFiredParams{
				FiredAt: formatTime(f.clock()), TurnIndex: 1, Attempts: 1, ID: r.ID,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	f.sched.BootSweep(ctx)

	errRun := f.runByStatus(RunError)
	if errRun == nil || errRun.Error != "server restarted mid-run" {
		t.Fatalf("delivered run not swept: %+v", errRun)
	}
	if f.runByStatus(RunQueued) == nil {
		t.Fatal("queued run must survive the sweep for the boot pass to deliver")
	}
	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.ConsecutiveFailures != 0 {
		t.Error("sweep errors must not count toward auto-pause")
	}
}

func TestFinishedSession_SkipsAndPauses(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.gw.mu.Lock()
	f.gw.finished = true
	f.gw.mu.Unlock()

	f.sched.tick()
	waitFor(t, "skipped", func() bool {
		r := f.runByStatus(RunSkipped)
		return r != nil && r.Reason == "session completed"
	})
	waitFor(t, "paused", func() bool {
		got, _ := f.q.GetSchedule(context.Background(), sched.ID)
		return got.Enabled == 0 && got.PauseReason == PauseSessionCompleted
	})
	if f.gw.deliveryCount() != 0 {
		t.Error("no delivery may reach a finished session")
	}
}

func TestOnce_FiresAndParksCompleted(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeOnce, "", "2026-07-30T11:55:00Z")

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "reminded"}
	waitFor(t, "parked", func() bool {
		got, _ := f.q.GetSchedule(context.Background(), sched.ID)
		return got.Enabled == 0 && got.PauseReason == PauseCompleted && got.NextRunAt == ""
	})
}

func TestOnce_StaleSkipsWithAttention(t *testing.T) {
	f := newFixture(t, Options{OnceCatchupWindow: time.Hour})
	sched := f.createSchedule(t, ModeOnce, "", "2026-07-30T09:00:00Z") // 3h stale

	f.sched.tick()
	waitFor(t, "skipped", func() bool { return f.runByStatus(RunSkipped) != nil })
	if f.gw.deliveryCount() != 0 {
		t.Error("stale once must not fire")
	}
	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.Enabled != 0 || got.Attention != AttentionActionNeeded {
		t.Errorf("stale once: %+v", got)
	}
}

func TestAPI_CreateValidation(t *testing.T) {
	f := newFixture(t, Options{})
	ctx := context.Background()

	cases := []struct {
		name string
		p    CreateParams
	}{
		{"both cron and at", CreateParams{ProjectID: "p1", SessionID: "s1", Name: "x", Prompt: "y", Cron: "* * * * *", At: "2026-08-01T10:00:00Z"}},
		{"neither", CreateParams{ProjectID: "p1", SessionID: "s1", Name: "x", Prompt: "y"}},
		{"bad cron", CreateParams{ProjectID: "p1", SessionID: "s1", Name: "x", Prompt: "y", Cron: "not a cron"}},
		{"below cadence floor", CreateParams{ProjectID: "p1", SessionID: "s1", Name: "x", Prompt: "y", Cron: "* * * * *"}},
		{"past at", CreateParams{ProjectID: "p1", SessionID: "s1", Name: "x", Prompt: "y", At: "2020-01-01T00:00:00Z"}},
		{"empty name", CreateParams{ProjectID: "p1", SessionID: "s1", Prompt: "y", Cron: "0 * * * *"}},
	}
	f.sched.opts.MinInterval = 5 * time.Minute
	for _, tc := range cases {
		if _, err := f.sched.Create(ctx, tc.p); err == nil {
			t.Errorf("%s: expected validation error", tc.name)
		}
	}

	info, err := f.sched.Create(ctx, CreateParams{ProjectID: "p1", SessionID: "s1", Name: "ok", Prompt: "y", Cron: "0 9 * * 1-5"})
	if err != nil {
		t.Fatal(err)
	}
	if info.NextRunAt == "" || info.Mode != ModeRecurring || !info.Enabled {
		t.Errorf("created: %+v", info)
	}
}

func TestAPI_PendingApprovalCreateAndApprove(t *testing.T) {
	f := newFixture(t, Options{})
	ctx := context.Background()

	info, err := f.sched.Create(ctx, CreateParams{
		ProjectID: "p1", SessionID: "s1", Name: "agent loop", Prompt: "y",
		Cron: "0 * * * *", CreatedBy: "agent", Paused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.Enabled || info.PauseReason != PausePendingApproval {
		t.Fatalf("pending-approval create: %+v", info)
	}

	approved, err := f.sched.Approve(ctx, info.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !approved.Enabled || approved.NextRunAt == "" {
		t.Errorf("approved: %+v", approved)
	}
	if _, err := f.sched.Approve(ctx, info.ID, false); err == nil {
		t.Error("double-approve must fail")
	}
}

func TestAPI_RunNowOnPausedSchedule(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-31T00:00:00Z")
	if _, err := f.sched.Pause(context.Background(), sched.ID); err != nil {
		t.Fatal(err)
	}

	run, err := f.sched.RunNow(context.Background(), sched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunQueued {
		t.Errorf("run-now status = %q", run.Status)
	}
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })

	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.Enabled != 0 {
		t.Error("run-now must not re-enable a paused schedule")
	}
}

func TestAPI_MarkViewedClearsActionAttention(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-31T00:00:00Z")
	ctx := context.Background()

	f.sched.setAttention(ctx, sched.ID, AttentionActionNeeded, "r1")
	if err := f.sched.MarkViewed(ctx, sched.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.q.GetSchedule(ctx, sched.ID)
	if got.Attention != "" || got.LastViewedAt == "" {
		t.Errorf("after mark-viewed: %+v", got)
	}

	// failed attention does NOT clear on viewing.
	f.sched.setAttention(ctx, sched.ID, AttentionFailed, "r2")
	if err := f.sched.MarkViewed(ctx, sched.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = f.q.GetSchedule(ctx, sched.ID)
	if got.Attention != AttentionFailed {
		t.Error("failed attention must survive viewing")
	}
}

func TestRunNow_OnPausedScheduleSurvivesBusyRetry(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-31T00:00:00Z")
	if _, err := f.sched.Pause(context.Background(), sched.ID); err != nil {
		t.Fatal(err)
	}

	f.gw.mu.Lock()
	f.gw.deliverErr = session.ErrBusy
	f.gw.mu.Unlock()

	if _, err := f.sched.RunNow(context.Background(), sched.ID); err != nil {
		t.Fatal(err)
	}
	// Busy → requeued. The tick retry must NOT skip it as "schedule paused" —
	// run-now runs stay deliverable on paused schedules.
	waitFor(t, "requeued", func() bool { r := f.runByStatus(RunQueued); return r != nil })
	f.sched.tick()
	if r := f.runByStatus(RunSkipped); r != nil {
		t.Fatalf("run-now run must not be skipped on a paused schedule: %+v", r)
	}

	f.gw.mu.Lock()
	f.gw.deliverErr = nil
	f.gw.mu.Unlock()
	f.sched.tick()
	waitFor(t, "delivered", func() bool { return f.gw.deliveryCount() == 1 })
}

func TestPause_StrayQueuedRunResolvesSkipped(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-31T00:00:00Z")
	ctx := context.Background()

	// A cadence run that raced past pauseSchedule's sweep.
	if _, err := f.q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
		ID: newID(), ScheduleID: sched.ID, SessionID: "s1",
		ScheduledFor: "2026-07-30T11:00:00Z", CreatedAt: formatTime(f.clock()), Status: RunQueued,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.q.SetScheduleEnabled(ctx, store.SetScheduleEnabledParams{
		Enabled: 0, PauseReason: PauseUser, NextRunAt: "", UpdatedAt: formatTime(f.clock()), ID: sched.ID,
	}); err != nil {
		t.Fatal(err)
	}

	f.sched.tick()
	waitFor(t, "stray run skipped", func() bool {
		r := f.runByStatus(RunSkipped)
		return r != nil && r.Reason == "schedule paused"
	})
	if f.gw.deliveryCount() != 0 {
		t.Fatal("paused schedule's cadence run must not deliver")
	}
}

func TestRunNow_DoesNotConsumePendingOneShot(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeOnce, "", "2026-07-31T09:00:00Z") // future reminder

	if _, err := f.sched.RunNow(context.Background(), sched.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "delivered", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "ad-hoc"}
	waitFor(t, "resolved", func() bool { return f.runByStatus(RunOK) != nil })

	got, _ := f.q.GetSchedule(context.Background(), sched.ID)
	if got.Enabled != 1 || got.NextRunAt != "2026-07-31T09:00:00Z" {
		t.Fatalf("run-now consumed the pending one-shot: %+v", got)
	}
}

func TestDeliver_SessionFinishedSentinelSkipsAndPauses(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.gw.mu.Lock()
	f.gw.deliverErr = session.ErrSessionFinished
	f.gw.mu.Unlock()

	f.sched.tick()
	waitFor(t, "skipped + paused", func() bool {
		r := f.runByStatus(RunSkipped)
		got, _ := f.q.GetSchedule(context.Background(), sched.ID)
		return r != nil && r.Reason == "session completed" &&
			got.Enabled == 0 && got.PauseReason == PauseSessionCompleted
	})
}

func TestFireDue_PauseRaceRollsBack(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")
	ctx := context.Background()

	stale, err := f.q.GetSchedule(ctx, sched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.sched.Pause(ctx, sched.ID); err != nil {
		t.Fatal(err)
	}

	// Simulates a pause landing between the tick's due read and fireDue.
	if err := f.sched.fireDue(ctx, stale, f.clock().UTC()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM schedule_runs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("fire on a paused-mid-tick schedule must roll back, found %d runs", count)
	}
	got, _ := f.q.GetSchedule(ctx, sched.ID)
	if got.NextRunAt != "" {
		t.Fatalf("paused schedule re-armed by racing fire: %q", got.NextRunAt)
	}
}

func TestSweep_ReclaimsStuckFiringRun(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-31T00:00:00Z")
	ctx := context.Background()

	// A run whose deliverer died right after the claim: firing, no fired_at.
	if _, err := f.q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
		ID: newID(), ScheduleID: sched.ID, SessionID: "s1",
		ScheduledFor: "2026-07-30T11:00:00Z", CreatedAt: formatTime(f.clock()), Status: RunQueued,
	}); err != nil {
		t.Fatal(err)
	}
	r := f.runByStatus(RunQueued)
	if _, err := f.q.ClaimScheduleRun(ctx, r.ID); err != nil {
		t.Fatal(err)
	}

	f.sched.tick() // first observation
	if got := f.runByStatus(RunFiring); got == nil {
		t.Fatal("run must still be firing after first observation")
	}
	f.advance(3 * time.Minute)
	f.sched.tick() // past firingReclaimAfter → reclaim
	waitFor(t, "reclaimed to queued", func() bool { return f.runByStatus(RunQueued) != nil })
}

func TestRequeue_NeverResurrectsTerminalRun(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-31T00:00:00Z")
	ctx := context.Background()

	// A run the sweep observed as firing, resolved to a terminal error by its
	// outcome waiter before the reclaim's requeue landed.
	if _, err := f.q.CreateScheduleRun(ctx, store.CreateScheduleRunParams{
		ID: newID(), ScheduleID: sched.ID, SessionID: "s1",
		ScheduledFor: "2026-07-30T11:00:00Z", CreatedAt: formatTime(f.clock()), Status: RunQueued,
	}); err != nil {
		t.Fatal(err)
	}
	r := f.runByStatus(RunQueued)
	if _, err := f.q.ClaimScheduleRun(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.ResolveScheduleRun(ctx, store.ResolveScheduleRunParams{
		Status: RunError, FinishedAt: formatTime(f.clock()), Error: "boom", ID: r.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// The stale requeue must be a no-op: flipping error back to queued would
	// redeliver the run and double-count it toward auto-pause.
	if err := f.q.RequeueScheduleRun(ctx, store.RequeueScheduleRunParams{Attempts: r.Attempts, ID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if got := f.runByStatus(RunError); got == nil {
		t.Fatal("terminal run was resurrected to queued by a stale requeue")
	}

	// A late mark-fired must not flip it to running either.
	if err := f.q.MarkScheduleRunFired(ctx, store.MarkScheduleRunFiredParams{
		FiredAt: formatTime(f.clock()), TurnIndex: 1, Attempts: 1, ID: r.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.runByStatus(RunError); got == nil {
		t.Fatal("terminal run was marked running by a stale mark-fired")
	}
}

func TestAPI_LengthCapsAndPendingProposalCap(t *testing.T) {
	f := newFixture(t, Options{})
	ctx := context.Background()

	long := strings.Repeat("x", maxNameLen+1)
	if _, err := f.sched.Create(ctx, CreateParams{ProjectID: "p1", SessionID: "s1", Name: long, Prompt: "y", Cron: "0 * * * *"}); err == nil {
		t.Error("over-long name must be rejected")
	}
	if _, err := f.sched.Create(ctx, CreateParams{ProjectID: "p1", SessionID: "s1", Name: "n", Prompt: strings.Repeat("p", maxPromptLen+1), Cron: "0 * * * *"}); err == nil {
		t.Error("over-long prompt must be rejected")
	}

	for i := 0; i < maxPendingProposals; i++ {
		if _, err := f.sched.AgentCreate(ctx, "s1", fmt.Sprintf("prop-%d", i), "p", "0 * * * *", "", false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.sched.AgentCreate(ctx, "s1", "one too many", "p", "0 * * * *", "", false); err == nil {
		t.Error("pending-proposal flood must be capped")
	}
}

func TestAPI_UpdateAbsentMeansKeep(t *testing.T) {
	f := newFixture(t, Options{})
	ctx := context.Background()
	info, err := f.sched.Create(ctx, CreateParams{
		ProjectID: "p1", SessionID: "s1", Name: "keep", Prompt: "p",
		Cron: "0 9 * * *", ExpiresAt: "2026-12-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := f.sched.Update(ctx, UpdateParams{ID: info.ID, Name: "keep2", Prompt: "p2"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Cron != "0 9 * * *" || updated.ExpiresAt != "2026-12-01T00:00:00Z" {
		t.Fatalf("omitted fields must keep stored values: %+v", updated)
	}
}

func TestAgentReport_WinsOverTextFallback(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	if !strings.Contains(d.prompt, "[scheduled-run:"+d.origin.RunID+"]") {
		t.Errorf("fired prompt must carry the report footer, got tail %q", d.prompt[len(d.prompt)-80:])
	}
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	// Agent reports action-needed mid-turn; the turn later completes "ok".
	if _, err := f.sched.AgentReport(context.Background(), "s1", d.origin.RunID, "action-needed", "PR needs a human rebase"); err != nil {
		t.Fatal(err)
	}
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "did things"}

	waitFor(t, "report wins", func() bool {
		r := f.runByStatus(RunActionNeeded)
		return r != nil && r.Summary == "PR needs a human rebase" && r.Reason == "reported by agent"
	})
	// Attention is a separate write after resolution — poll, don't snapshot.
	waitFor(t, "attention raised", func() bool {
		got, _ := f.q.GetSchedule(context.Background(), sched.ID)
		return got.Attention == AttentionActionNeeded
	})
}

func TestAgentReport_FailedCountsTowardAutoPause(t *testing.T) {
	f := newFixture(t, Options{MaxConsecutiveFailures: 1})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })
	if _, err := f.sched.AgentReport(context.Background(), "s1", d.origin.RunID, "failed", "could not reach the API"); err != nil {
		t.Fatal(err)
	}
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "gave up"}

	waitFor(t, "auto-paused via reported failure", func() bool {
		got, _ := f.q.GetSchedule(context.Background(), sched.ID)
		return got.Enabled == 0 && got.PauseReason == PauseAutoFailures
	})
}

func TestAgentReport_LateAndScopeValidation(t *testing.T) {
	f := newFixture(t, Options{})
	f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")
	ctx := context.Background()

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	// Wrong session is refused.
	if _, err := f.sched.AgentReport(ctx, "someone-else", d.origin.RunID, "ok", "x"); err == nil {
		t.Error("cross-session report must be refused")
	}
	// Bad status is refused.
	if _, err := f.sched.AgentReport(ctx, "s1", d.origin.RunID, "amazing", "x"); err == nil {
		t.Error("unknown status must be refused")
	}

	// Resolve the run, then report late → annotation, status untouched.
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "fine"}
	waitFor(t, "resolved ok", func() bool { return f.runByStatus(RunOK) != nil })
	if _, err := f.sched.AgentReport(ctx, "s1", d.origin.RunID, "failed", "actually it broke"); err != nil {
		t.Fatal(err)
	}
	run := f.runByStatus(RunOK)
	if run == nil || run.LateReport == "" || !strings.Contains(run.LateReport, "actually it broke") {
		t.Fatalf("late report must annotate without rewriting the terminal: %+v", run)
	}
}

func (f *fixture) createDynamic(t *testing.T) ScheduleInfo {
	t.Helper()
	info, err := f.sched.Create(context.Background(), CreateParams{
		ProjectID: "p1", SessionID: "s1", Name: "self-paced", Prompt: "watch things", Dynamic: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestDynamic_PaceWritesNextRunAndStampsReason(t *testing.T) {
	f := newFixture(t, Options{MinInterval: time.Minute, DynamicMaxDelay: 6 * time.Hour})
	info := f.createDynamic(t) // next_run_at = now → due immediately
	ctx := context.Background()

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	if !strings.Contains(d.prompt, "ScheduleNext") {
		t.Error("dynamic fire footer must mention ScheduleNext")
	}
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	msg, err := f.sched.AgentPace(ctx, "s1", d.origin.RunID, 1500, "waiting for CI run", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "25m") {
		t.Errorf("pace ack = %q", msg)
	}
	got, _ := f.q.GetSchedule(ctx, info.ID)
	if got.NextRunAt != formatTime(f.clock().Add(25*time.Minute)) {
		t.Errorf("next_run_at not written immediately: %q", got.NextRunAt)
	}

	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "checked"}
	waitFor(t, "reason stamped", func() bool {
		r := f.runByStatus(RunOK)
		return r != nil && r.Reason == "next in 25m0s — waiting for CI run"
	})

	// Clamping: 5s → floor.
	f.sched.tick() // nothing due yet; just exercise
	if _, err := f.sched.AgentPace(ctx, "s1", d.origin.RunID, 5, "x", false); err == nil {
		t.Error("pacing a resolved run must be refused")
	}
}

func TestDynamic_StopParksLoop(t *testing.T) {
	f := newFixture(t, Options{})
	info := f.createDynamic(t)
	ctx := context.Background()

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	if _, err := f.sched.AgentPace(ctx, "s1", d.origin.RunID, 0, "", true); err != nil {
		t.Fatal(err)
	}
	got, _ := f.q.GetSchedule(ctx, info.ID)
	if got.Enabled != 0 || got.PauseReason != PauseDynamicEnded {
		t.Fatalf("stop must park the loop: %+v", got)
	}
	d.outcome <- session.TurnOutcome{TurnIndex: d.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "done"}
	waitFor(t, "stopped reason", func() bool {
		r := f.runByStatus(RunOK)
		return r != nil && r.Reason == "stopped by agent"
	})
}

func TestDynamic_UnpacedTwiceParksAsEnded(t *testing.T) {
	f := newFixture(t, Options{DynamicFallback: 20 * time.Minute})
	info := f.createDynamic(t)
	ctx := context.Background()

	// Fire 1: no ScheduleNext call → fallback pre-write stands.
	f.sched.tick()
	waitFor(t, "delivery 1", func() bool { return f.gw.deliveryCount() == 1 })
	d1 := f.gw.lastDelivery()
	waitFor(t, "running 1", func() bool { return f.runByStatus(RunRunning) != nil })
	d1.outcome <- session.TurnOutcome{TurnIndex: d1.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "r1"}
	waitFor(t, "resolved 1", func() bool { return f.runByStatus(RunRunning) == nil })

	got, _ := f.q.GetSchedule(ctx, info.ID)
	if got.Enabled != 1 {
		t.Fatal("one unpaced run must only consume the fallback, not park")
	}

	// Fire 2 (the fallback): still no ScheduleNext → park dynamic-ended.
	f.advance(21 * time.Minute)
	f.sched.tick()
	waitFor(t, "delivery 2", func() bool { return f.gw.deliveryCount() == 2 })
	d2 := f.gw.lastDelivery()
	waitFor(t, "running 2", func() bool { return f.runByStatus(RunRunning) != nil })
	d2.outcome <- session.TurnOutcome{TurnIndex: d2.turnIndex, Status: runtime.TurnStatusCompleted, FinalText: "r2"}

	waitFor(t, "parked dynamic-ended", func() bool {
		got, _ := f.q.GetSchedule(ctx, info.ID)
		return got.Enabled == 0 && got.PauseReason == PauseDynamicEnded
	})
}

func TestDynamic_PaceValidation(t *testing.T) {
	f := newFixture(t, Options{})
	sched := f.createSchedule(t, ModeRecurring, "0 * * * *", "2026-07-30T11:00:00Z")

	f.sched.tick()
	waitFor(t, "delivery", func() bool { return f.gw.deliveryCount() == 1 })
	d := f.gw.lastDelivery()
	waitFor(t, "running", func() bool { return f.runByStatus(RunRunning) != nil })

	// Pacing a fixed-cadence schedule is refused.
	if _, err := f.sched.AgentPace(context.Background(), "s1", d.origin.RunID, 600, "x", false); err == nil {
		t.Error("pacing a recurring schedule must be refused")
	}
	_ = sched
}

func TestStandingConsent_ActivatesWithoutApprovalUpToCap(t *testing.T) {
	f := newFixture(t, Options{})
	ctx := context.Background()

	// Grant standing consent via approve-with-always-allow on a proposal.
	first, err := f.sched.AgentCreate(ctx, "s1", "loop-0", "p", "0 * * * *", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "awaiting the user's approval") {
		t.Fatalf("without consent, proposals must await approval: %q", first)
	}
	pending, _ := f.q.ListSchedulesBySession(ctx, "s1")
	if _, err := f.sched.Approve(ctx, pending[0].ID, true); err != nil {
		t.Fatal(err)
	}
	sess, _ := f.q.GetSession(ctx, "s1")
	if !session.ParsePresets(sess.BehaviorPresets).SelfSchedule {
		t.Fatal("always-allow approve must persist the selfSchedule preset")
	}

	// Subsequent proposals activate immediately.
	msg, err := f.sched.AgentCreate(ctx, "s1", "loop-1", "p", "30 * * * *", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "ACTIVE") {
		t.Fatalf("standing consent must activate directly: %q", msg)
	}

	// Beyond the active cap, fall back to pending-approval.
	for i := 2; i < maxStandingActive+1; i++ {
		if _, err := f.sched.AgentCreate(ctx, "s1", fmt.Sprintf("loop-%d", i), "p", fmt.Sprintf("%d * * * *", i), "", false); err != nil {
			t.Fatal(err)
		}
	}
	over, err := f.sched.AgentCreate(ctx, "s1", "one over cap", "p", "45 * * * *", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(over, "awaiting the user's approval") {
		t.Fatalf("beyond the active cap, proposals must fall back to approval: %q", over)
	}
}

func TestExpiry_PausesVisibly(t *testing.T) {
	f := newFixture(t, Options{})
	ctx := context.Background()
	row, err := f.q.CreateSchedule(ctx, store.CreateScheduleParams{
		ID: newID(), ProjectID: "p1", SessionID: "s1", Name: "exp", Prompt: "y",
		Cron: "0 * * * *", Mode: ModeRecurring, Enabled: 1,
		NextRunAt: "2026-07-30T11:00:00Z", ExpiresAt: "2026-07-30T11:30:00Z",
		CreatedBy: "user", CreatedAt: formatTime(f.clock()), UpdatedAt: formatTime(f.clock()),
	})
	if err != nil {
		t.Fatal(err)
	}

	f.sched.tick()
	waitFor(t, "expired pause", func() bool {
		got, _ := f.q.GetSchedule(ctx, row.ID)
		return got.Enabled == 0 && got.PauseReason == PauseExpired
	})
	if f.gw.deliveryCount() != 0 {
		t.Error("expired schedule must not fire")
	}
}
