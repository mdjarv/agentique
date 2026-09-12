package assistant

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitForJournal polls because the turn-end listener answers on its own
// goroutine: the runtime's event loop must not block on a database write.
func waitForJournal(t *testing.T, svc *Service, want int) []JournalEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, err := svc.Journal(context.Background(), "", 20)
		if err != nil {
			t.Fatalf("Journal() = %v", err)
		}
		if len(entries) >= want {
			return entries
		}
		if time.Now().After(deadline) {
			t.Fatalf("journal has %d entries, want %d", len(entries), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A turn that belongs to no session is not journaled. The runtime's turn-end
// hook is per process, so the assistant's own head and every discussion
// persona fire it, and an entry about one would be the assistant reporting on
// itself.
func TestTurnEndIgnoresATurnWithNoSession(t *testing.T) {
	dir := &fakeDirectory{sessions: []SessionRow{{ID: "s1", Name: "Reconnect Drops"}}}
	facts := &fakeFacts{outcome: TurnOutcome{ClosingWords: "done"}}
	svc, _, _ := newTestService(t, WithDirectory(dir), WithTurnFacts(facts))

	svc.OnTurnEnd("persona-4f2c")
	svc.OnTurnEnd("s1")
	entries := waitForJournal(t, svc, 1)

	if len(entries) != 1 || entries[0].SessionID != "s1" {
		t.Errorf("journal = %+v, want only the real session's turn", entries)
	}
}

// Blocked outranks failed outranks finished, the same rule
// lib/session/priority.ts uses: the thing still holding a process is more
// urgent than the thing that already stopped.
func TestTurnEndOrdersBlockedBeforeFailed(t *testing.T) {
	facts := &fakeFacts{
		pending: "May I run the migration?",
		outcome: TurnOutcome{Failed: true, ClosingWords: "it broke"},
	}
	svc, _, _ := newTestService(t, WithTurnFacts(facts))
	surface := &fakeSurface{name: SurfaceThread}
	if _, err := svc.RegisterSurface(surface); err != nil {
		t.Fatalf("RegisterSurface() = %v", err)
	}

	svc.OnTurnEnd("s1")
	entries := waitForJournal(t, svc, 1)

	if entries[0].Kind != JournalSessionBlocked {
		t.Fatalf("kind = %q, want blocked even though the session also failed", entries[0].Kind)
	}
	if entries[0].Summary != facts.pending {
		t.Errorf("summary = %q, want what it is waiting on", entries[0].Summary)
	}
	if !entries[0].Untrusted {
		t.Error("a question an agent wrote is agent-written text")
	}
	if surface.countOf(ItemNotice) != 1 {
		t.Errorf("surface got %d notices, want 1", surface.countOf(ItemNotice))
	}
}

func TestTurnEndJournalsFinishedAndFailed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failed bool
		want   JournalKind
	}{
		{name: "clean", want: JournalSessionFinished},
		{name: "failed", failed: true, want: JournalSessionFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := &fakeFacts{outcome: TurnOutcome{
				Failed:       tc.failed,
				ProjectID:    "p1",
				SessionName:  "Reconnect Drops",
				ClosingWords: "the retry is in",
			}}
			svc, _, _ := newTestService(t, WithTurnFacts(facts))

			svc.OnTurnEnd("s1")
			entries := waitForJournal(t, svc, 1)

			if entries[0].Kind != tc.want {
				t.Errorf("kind = %q, want %q", entries[0].Kind, tc.want)
			}
			if entries[0].ProjectID != "p1" {
				t.Errorf("entry does not place the session in its project: %+v", entries[0])
			}
			if name, _ := entries[0].Payload["name"].(string); name != "Reconnect Drops" {
				t.Errorf("payload name = %q, want the session's name", name)
			}
		})
	}
}

// With no turn facts wired there is nothing to say, and saying nothing must not
// cost a goroutine or a row.
func TestTurnEndIsSilentWithNoFacts(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.OnTurnEnd("s1")

	entries, err := svc.Journal(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("journal = %+v, want nothing", entries)
	}
}

// A FOLLOWED run's report is kept whether or not anyone is live, which is the
// whole reason the registry outlives the call.
func TestReportIsJournaledUntrustedAndDelivered(t *testing.T) {
	ctx := context.Background()
	svc, queries, recorder := newTestService(t)
	session := seedSession(t, queries)
	if err := svc.Follow(ctx, session.ID, "operator"); err != nil {
		t.Fatalf("Follow() = %v", err)
	}
	surface := &fakeSurface{name: SurfaceThread}
	if _, err := svc.RegisterSurface(surface); err != nil {
		t.Fatalf("RegisterSurface() = %v", err)
	}

	msg, err := svc.Report(session.ID, "surprise", "the auth tests were already failing on main")
	if err != nil {
		t.Fatalf("Report() = %v", err)
	}
	if !strings.Contains(msg, "journal") {
		t.Errorf("message = %q, want it to say the report was kept", msg)
	}

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != JournalReport {
		t.Fatalf("journal = %+v, want one report", entries)
	}
	if !entries[0].Untrusted {
		t.Error("a report is agent-written text about content nobody here authored")
	}
	if entries[0].SessionID != session.ID {
		t.Errorf("entry names %q, want the reporting session", entries[0].SessionID)
	}
	if surface.countOf(ItemReport) != 1 {
		t.Errorf("surface got %d reports, want 1", surface.countOf(ItemReport))
	}

	var pushes int
	for _, event := range recorder.Events() {
		if event.Type == EventJournal {
			pushes++
		}
	}
	if pushes != 1 {
		t.Errorf("pushed %d journal events, want 1 — every entry is announced", pushes)
	}
}

// The tool is on one shared endpoint, so any session can call it. A report from
// a run nobody is following is refused in words and nothing is written: those
// entries ride every head turn as untrusted news.
func TestReportFromAnUnfollowedSessionIsNotKept(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	msg, err := svc.Report("s1", "surprise", "the tests were already failing")
	if err != nil {
		t.Fatalf("Report() = %v", err)
	}
	if !strings.Contains(msg, "not following") {
		t.Errorf("message = %q, want it to say nobody is following", msg)
	}
	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("journal = %+v, want nothing kept for an unfollowed run", entries)
	}
}

// The budget is spent per REPORT, not per delivery: the journal write is what
// rides every head turn, so it needs the same one ceiling the spoken half has.
func TestReportBudgetBoundsTheJournalToo(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)
	session := seedSession(t, queries)
	if err := svc.Follow(ctx, session.ID, "operator"); err != nil {
		t.Fatalf("Follow() = %v", err)
	}

	for i := range reportBurst + 2 {
		if _, err := svc.Report(session.ID, "milestone", fmt.Sprintf("step %d", i)); err != nil {
			t.Fatalf("Report() = %v", err)
		}
	}

	entries, err := svc.Journal(ctx, "", 50)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != reportBurst {
		t.Errorf("journal has %d reports, want the burst of %d", len(entries), reportBurst)
	}
}

func TestReportRejectsAnUnknownKindWithoutJournaling(t *testing.T) {
	svc, _, _ := newTestService(t)

	if _, err := svc.Report("s1", "progress", "opening a file"); err == nil {
		t.Fatal("Report() accepted a kind outside the closed set")
	}
	entries, err := svc.Journal(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("journal = %+v, want nothing written for a rejected report", entries)
	}
}

// A report reaches a live follower through the registry, the way a call gets
// one, and the worker's answer says it was spoken.
func TestReportReachesAFollower(t *testing.T) {
	svc, _, _ := newTestService(t)

	follower := &recorder{}
	defer svc.Registry().Follow("s1", follower)()

	msg, err := svc.Report("s1", "decision", "went with the middleware")
	if err != nil {
		t.Fatalf("Report() = %v", err)
	}
	if !strings.Contains(msg, "Spoken") {
		t.Errorf("message = %q, want the registry's own answer", msg)
	}
	if follower.count() != 1 {
		t.Errorf("follower got %d reports, want 1", follower.count())
	}
}

// The state push repeats, and a journal is append-only: a merge is one entry
// however many times it is announced.
func TestSessionStateWritesOncePerSessionAndKind(t *testing.T) {
	ctx := context.Background()
	svc, queries := primed(t)
	sess := seedSession(t, queries)

	// The push announces a write that has already happened.
	if err := queries.SetWorktreeMerged(ctx, sess.ID); err != nil {
		t.Fatalf("SetWorktreeMerged() = %v", err)
	}
	state := SessionState{SessionID: sess.ID, WorktreeMerged: true, Version: 4}
	svc.ObserveSessionState(ctx, state)
	state.Version = 5
	svc.ObserveSessionState(ctx, state)

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != JournalSessionMerged {
		t.Fatalf("journal = %+v, want one merge", entries)
	}
	if entries[0].Summary != "" {
		t.Errorf("summary = %q, want none: the kind already says it", entries[0].Summary)
	}

	// A restart loses the memory; the sessions table is what the new process
	// primes from, and it already says merged.
	restarted, err := New(queries, WithLogger(testLogger()))
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if err := restarted.PrimeSessionStates(ctx); err != nil {
		t.Fatalf("PrimeSessionStates() = %v", err)
	}
	restarted.ObserveSessionState(ctx, state)
	entries, err = svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("journal = %+v, want the restart to treat an old merge as old", entries)
	}
}

// The production fault: the first boot with the assistant on saw every
// already-archived session's snapshot and filed each one as news. A snapshot
// is a state; the journal records transitions against what the sessions table
// already held.
func TestSessionStateBaselineIsWhatTheDatabaseAlreadyHeld(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)
	old := seedSession(t, queries)
	fresh := seedSessionIn(t, queries, old.ProjectID)
	if err := queries.SetSessionArchived(ctx, old.ID); err != nil {
		t.Fatalf("SetSessionArchived() = %v", err)
	}
	if err := svc.PrimeSessionStates(ctx); err != nil {
		t.Fatalf("PrimeSessionStates() = %v", err)
	}

	// The boot storm: a snapshot of the session archived long ago.
	svc.ObserveSessionState(ctx, SessionState{SessionID: old.ID, ArchivedAt: "2026-01-01T00:00:00Z", Version: 9})
	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("journal = %+v, want an old archive to be old news", entries)
	}

	// A real archive, after the baseline: news.
	if err := queries.SetSessionArchived(ctx, fresh.ID); err != nil {
		t.Fatalf("SetSessionArchived() = %v", err)
	}
	svc.ObserveSessionState(ctx, SessionState{SessionID: fresh.ID, ArchivedAt: "2026-03-01T00:00:00Z", Version: 2})
	entries, err = svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 || entries[0].SessionID != fresh.ID || entries[0].Kind != JournalSessionArchived {
		t.Fatalf("journal = %+v, want one archive of the fresh session", entries)
	}
}

// Never primed by the wiring, the observer primes itself on the first push and
// still treats what the table held as old. It cannot flood; it can only miss
// the transition that arrived first, which is the fail-closed direction.
func TestSessionStatePrimesItselfLazily(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)
	old := seedSession(t, queries)
	if err := queries.SetSessionArchived(ctx, old.ID); err != nil {
		t.Fatalf("SetSessionArchived() = %v", err)
	}

	svc.ObserveSessionState(ctx, SessionState{SessionID: old.ID, ArchivedAt: "2026-01-01T00:00:00Z", Version: 1})

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("journal = %+v, want nothing from a lazily primed baseline", entries)
	}
}

// Unarchive leaves no residue, and it moves the baseline: filing the same
// session away a second time is a second gesture.
func TestSessionStateArchiveAgainAfterUnarchiveIsNews(t *testing.T) {
	ctx := context.Background()
	svc, queries := primed(t)
	sess := seedSession(t, queries)

	svc.ObserveSessionState(ctx, SessionState{SessionID: sess.ID, ArchivedAt: "2026-02-01T10:00:00Z", Version: 1})
	svc.ObserveSessionState(ctx, SessionState{SessionID: sess.ID, Version: 2})
	svc.ObserveSessionState(ctx, SessionState{SessionID: sess.ID, ArchivedAt: "2026-02-02T10:00:00Z", Version: 3})

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("journal = %+v, want two archives with an unarchive between them", entries)
	}
}

// Merged and archived are two facts with two owners, so one session can carry
// both.
func TestSessionStateWritesMergedAndArchivedSeparately(t *testing.T) {
	ctx := context.Background()
	svc, queries := primed(t)
	sess := seedSession(t, queries)

	svc.ObserveSessionState(ctx, SessionState{
		SessionID:      sess.ID,
		WorktreeMerged: true,
		ArchivedAt:     "2026-02-01T10:00:00Z",
		Version:        2,
	})

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("journal = %+v, want a merge and an archive", entries)
	}
	kinds := map[JournalKind]bool{}
	for _, entry := range entries {
		kinds[entry.Kind] = true
	}
	if !kinds[JournalSessionMerged] || !kinds[JournalSessionArchived] {
		t.Errorf("kinds = %v, want both", kinds)
	}
}

func TestSessionStateIgnoresASnapshotWithNothingToSay(t *testing.T) {
	ctx := context.Background()
	svc, _ := primed(t)

	svc.ObserveSessionState(ctx, SessionState{SessionID: "s1", Version: 1})
	svc.ObserveSessionState(ctx, SessionState{WorktreeMerged: true})

	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("journal = %+v, want nothing", entries)
	}
}

func TestLoopPausedIsTheServersOwnWords(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	if err := svc.LoopPaused(ctx, "s1", "nightly", "three failures in a row"); err != nil {
		t.Fatalf("LoopPaused() = %v", err)
	}
	entries, err := svc.Journal(ctx, "", 10)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 || entries[0].Kind != JournalLoopPaused {
		t.Fatalf("journal = %+v, want a paused loop", entries)
	}
	if entries[0].Untrusted {
		t.Error("a paused loop is the scheduler's fact, not agent-written text")
	}
	if !strings.Contains(entries[0].Summary, "nightly") {
		t.Errorf("summary = %q, want it to name the loop", entries[0].Summary)
	}
}

// TurnNotice is the ONE ranking rule, and it has two callers: the turn-end
// listener above, and the wiring that keeps a live call hearing its runtime
// facts on a server whose assistant is switched off. A session that says "needs
// approval" in one place cannot say something else in your ear, so the rule is
// tested where it lives rather than twice at its call sites.
func TestTurnNoticeRanksTheRuntimesFacts(t *testing.T) {
	ctx := context.Background()

	blocked := &fakeFacts{
		pending: "May I run the migration?",
		outcome: TurnOutcome{Failed: true, ClosingWords: "it broke"},
	}
	notice, outcome, err := TurnNotice(ctx, blocked, "s1")
	if err != nil {
		t.Fatalf("TurnNotice() = %v", err)
	}
	if notice.Kind != NoticeBlocked || notice.Headline != "May I run the migration?" {
		t.Errorf("notice = %+v, want the blocked question", notice)
	}
	// Nothing about a blocked turn has ended, so there is no outcome to report.
	if outcome != (TurnOutcome{}) {
		t.Errorf("outcome = %+v on a blocked turn, want nothing", outcome)
	}

	failed := &fakeFacts{outcome: TurnOutcome{
		Failed: true, ProjectID: "p1", SessionName: "Reconnect Drops", ClosingWords: "it broke",
	}}
	notice, outcome, err = TurnNotice(ctx, failed, "s1")
	if err != nil {
		t.Fatalf("TurnNotice() = %v", err)
	}
	if notice.Kind != NoticeFailed || notice.Headline != "it broke" {
		t.Errorf("notice = %+v, want the failure", notice)
	}
	if outcome.SessionName != "Reconnect Drops" || outcome.ProjectID != "p1" {
		t.Errorf("outcome = %+v, want the session placed", outcome)
	}

	finished := &fakeFacts{outcome: TurnOutcome{ClosingWords: "the retry is in"}}
	if notice, _, err = TurnNotice(ctx, finished, "s1"); err != nil {
		t.Fatalf("TurnNotice() = %v", err)
	}
	if notice.Kind != NoticeFinished {
		t.Errorf("notice.Kind = %q, want finished", notice.Kind)
	}

	// No facts at all is an error rather than an invented "finished": a notice
	// nobody can stand behind is worse than none.
	if _, _, err = TurnNotice(ctx, nil, "s1"); err == nil {
		t.Error("TurnNotice with no facts answered a notice")
	}
}

// The observer spawns a goroutine per qualifying push and the bus delivers
// synchronously on whichever goroutine published, so two snapshots can arrive
// together. Moving the baseline under the lock is the claim: whichever
// goroutine flips it writes, and the other sees a baseline that already agrees.
func TestSessionStateWritesOnceUnderConcurrentPushes(t *testing.T) {
	ctx := context.Background()
	svc, _ := primed(t)

	state := SessionState{SessionID: "s1", WorktreeMerged: true, Version: 4}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.ObserveSessionState(ctx, state)
		}()
	}
	wg.Wait()

	entries, err := svc.Journal(ctx, "", 20)
	if err != nil {
		t.Fatalf("Journal() = %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("journal = %+v, want one merge however many pushes announced it", entries)
	}
}
