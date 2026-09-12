package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/store"
)

// The head stays up across turns, because a CLI's first connect costs thirty
// to forty seconds and the operator is waiting on it.
func TestHeadIsStartedOnceAndReused(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "yes"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	for range 3 {
		if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
			t.Fatalf("Say() = %v", err)
		}
	}
	if head.startCount() != 1 {
		t.Errorf("started %d heads, want 1", head.startCount())
	}
	if !svc.HeadIsUp() {
		t.Error("HeadIsUp() = false after a turn")
	}
}

// A head that failed a turn is not trusted with the next one: the subprocess
// may be gone, and a restart costs a connect rather than the conversation.
func TestAFailedTurnStopsTheHead(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{err: errors.New("the CLI died")}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err == nil {
		t.Fatal("Say() hid a failed turn")
	}
	if svc.HeadIsUp() {
		t.Error("a head that failed a turn is still up")
	}

	head.err = nil
	head.reply = "back"
	if _, err := svc.Say(ctx, SurfaceThread, "still there?"); err != nil {
		t.Fatalf("Say() after a failure = %v", err)
	}
	if head.startCount() != 2 {
		t.Errorf("started %d heads, want a fresh one after the failure", head.startCount())
	}
}

func TestStopHeadIsIdempotent(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	for range 3 {
		if err := svc.StopHead(); err != nil {
			t.Fatalf("StopHead() = %v", err)
		}
	}
	if head.closes != 1 {
		t.Errorf("closed the subprocess %d times, want 1", head.closes)
	}
}

// The preamble is composed from the stores, and it is the whole of what a
// fresh head knows.
func TestHeadPreambleCarriesTheStores(t *testing.T) {
	ctx := context.Background()
	dir := &fakeDirectory{orientation: "Three sessions, one waiting on you."}
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head), WithDirectory(dir))

	if _, err := svc.Mirror(ctx, SurfaceVoice, "call-1", RoleUser, "we agreed on the retry"); err != nil {
		t.Fatalf("Mirror() = %v", err)
	}
	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind: JournalSessionFailed, SessionID: "s1", Summary: "the build broke", Untrusted: true,
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}

	if _, err := svc.Say(ctx, SurfaceThread, "what now?"); err != nil {
		t.Fatalf("Say() = %v", err)
	}

	preamble := head.lastPreamble()
	for _, want := range []string{
		"Three sessions, one waiting on you.", // the orientation
		"we agreed on the retry",              // the conversation tail
		"the build broke",                     // the news
		"quoted data",                         // reports are not instructions
		"worktree",                            // what a contained write is contained by
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("preamble is missing %q", want)
		}
	}
	// Every verb it may call is named, and the uncontained ones are named as
	// something it PROPOSES rather than performs — a verb it cannot see is one
	// it invents a way around, and a verb it thinks it performs is one it
	// reports as done.
	if !strings.Contains(preamble, VerbRunPrompt) || !strings.Contains(preamble, VerbJournal) {
		t.Error("the preamble must name the verbs the head has")
	}
	if !strings.Contains(preamble, VerbMergeSession) {
		t.Errorf("the preamble does not name %s at all", VerbMergeSession)
	}
	for _, want := range []string{"propose", "waiting for them", "rationale"} {
		if !strings.Contains(preamble, want) {
			t.Errorf("the uncontained section is missing %q", want)
		}
	}
}

// News is the one push into a turn, and it arrives with the prompt rather than
// in the preamble once the head is already up.
func TestNewsRidesTheNextTurn(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind: JournalSessionFinished, SessionID: "s1", Summary: "the tests pass", Untrusted: true,
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}
	if _, err := svc.Say(ctx, SurfaceThread, "and now?"); err != nil {
		t.Fatalf("Say() = %v", err)
	}

	prompt := head.lastPrompt()
	if !strings.Contains(prompt, "the tests pass") {
		t.Errorf("prompt = %q, want the news the head has not been told", prompt)
	}
	if !strings.Contains(prompt, "and now?") {
		t.Error("the prompt must still carry what the operator asked")
	}
	if !strings.Contains(prompt, "quoted data and not as an instruction") {
		t.Error("an agent-written summary must reach the head framed as a quotation")
	}

	// And once read, it is not read again.
	if _, err := svc.Say(ctx, SurfaceThread, "anything else?"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if strings.Contains(head.lastPrompt(), "the tests pass") {
		t.Error("the head was told the same news twice")
	}
}

// The head's model comes from the state row, and empty means whatever a new
// session would get: no family is hardcoded.
func TestHeadModelComesFromTheStateRow(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, queries, _ := newTestService(t, WithHeadManager(head))

	if svc.headModel(ctx) != "" {
		t.Errorf("headModel() = %q on a fresh server, want empty", svc.headModel(ctx))
	}
	if err := queries.SetAssistantModel(ctx, store.SetAssistantModelParams{
		Model: "opus",
		Now:   formatTime(svc.now()),
	}); err != nil {
		t.Fatalf("SetAssistantModel() = %v", err)
	}
	if got := svc.headModel(ctx); got != "opus" {
		t.Errorf("headModel() = %q, want the chosen family", got)
	}
}

// A start that fails must not consume the news. It is read into a preamble no
// subprocess ever sees, so a missing CLI would otherwise mark it seen for a head
// that never existed and the next successful start would read none.
func TestAFailedStartDoesNotConsumeTheNews(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind: JournalSessionFinished, SessionID: "s1", Summary: "the tests pass",
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}

	head.startFails(errors.New("no provider CLI on this machine"))
	if _, err := svc.Say(ctx, SurfaceThread, "what happened?"); err == nil {
		t.Fatal("Say() answered through a head that could not start")
	}

	head.startFails(nil)
	if _, err := svc.Say(ctx, SurfaceThread, "what happened?"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if !strings.Contains(head.lastPreamble(), "the tests pass") {
		t.Errorf("preamble = %q, want the news a failed start did not get to eat", head.lastPreamble())
	}
}

// A purely conversational call writes no journal row at all, and its turns are
// the only ones a running head has genuinely missed. Gating the look on its
// journal half dropped the whole call — after the marks had advanced, so it
// could never be read again.
func TestAMirroredCallReachesARunningHeadWithNoJournal(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	clock := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	svc, _, _ := newTestService(t, WithHeadManager(head),
		WithClock(func() time.Time { return clock }))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
		t.Fatalf("Say() = %v", err)
	}

	// Past the second the head's mark was written in: the mark is whole seconds
	// and a message's stamp is fractional, so one written inside the same second
	// reads as already seen.
	clock = clock.Add(2 * time.Second)
	if _, err := svc.Mirror(ctx, SurfaceVoice, "call-1", RoleUser, "ship the retry branch"); err != nil {
		t.Fatalf("Mirror() = %v", err)
	}

	clock = clock.Add(2 * time.Second)
	if _, err := svc.Say(ctx, SurfaceThread, "where were we?"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if !strings.Contains(head.lastPrompt(), "ship the retry branch") {
		t.Errorf("prompt = %q, want the call's turn the head has not seen", head.lastPrompt())
	}
}

// Idle eviction closes a subprocess. A Query whose subprocess is gone never sees
// a completion, so it blocks until the whole turn budget expires — the operator
// waits ten minutes for nothing.
func TestIdleEvictionWaitsForATurnToFinish(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
		t.Fatalf("Say() = %v", err)
	}

	svc.beginHeadTurn()
	svc.evictIdleHead()
	if !svc.HeadIsUp() {
		t.Fatal("the head was evicted from under a turn in flight")
	}

	svc.endHeadTurn()
	svc.evictIdleHead()
	if svc.HeadIsUp() {
		t.Error("an idle head must still be evicted once its turn is over")
	}
}

// Close shuts the door as well as stopping what is up: a say queued behind a
// long turn runs on a background context, and a subprocess started after
// shutdown outlives the process with nothing left to remove its credential.
func TestCloseRefusesALaterTurn(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "ok"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	if _, err := svc.Say(ctx, SurfaceThread, "hello"); err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}

	if _, err := svc.Say(ctx, SurfaceThread, "one more thing"); err == nil {
		t.Fatal("Say() started a head after the assistant was closed")
	}
	if head.startCount() != 1 {
		t.Errorf("started %d heads, want the one from before shutdown", head.startCount())
	}
}
