package assistant

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewRefusesWithoutAStore(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Error("New(nil) built a service with nowhere to write")
	}
}

// The conversation is created once and found again, and the second call is the
// cached one rather than a second channel.
func TestEnsureConversationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)

	first, err := svc.EnsureConversation(ctx)
	if err != nil {
		t.Fatalf("EnsureConversation() = %v", err)
	}
	second, err := svc.EnsureConversation(ctx)
	if err != nil {
		t.Fatalf("EnsureConversation() = %v", err)
	}
	if first != second || first == "" {
		t.Fatalf("got %q then %q, want one conversation", first, second)
	}

	state, err := queries.GetAssistantState(ctx)
	if err != nil {
		t.Fatalf("GetAssistantState() = %v", err)
	}
	if state.ChannelID != first {
		t.Errorf("state names channel %q, want %q", state.ChannelID, first)
	}

	ch, err := queries.GetAssistantChannel(ctx)
	if err != nil {
		t.Fatalf("GetAssistantChannel() = %v", err)
	}
	if ch.Kind != "assistant" {
		t.Errorf("kind = %q, want assistant — that is what keeps it out of channel lists", ch.Kind)
	}
	if ch.ProjectID.Valid {
		t.Error("the conversation must be project-less")
	}
}

// A fresh service on an existing database finds the conversation from the
// state row rather than starting a second one.
func TestConversationSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)

	first, err := svc.EnsureConversation(ctx)
	if err != nil {
		t.Fatalf("EnsureConversation() = %v", err)
	}

	next, err := New(queries, WithLogger(testLogger()))
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	again, err := next.EnsureConversation(ctx)
	if err != nil {
		t.Fatalf("EnsureConversation() = %v", err)
	}
	if again != first {
		t.Errorf("a restarted assistant opened %q, want the existing %q", again, first)
	}
}

// Say stores both turns, in order, and pushes them.
func TestSayStoresBothTurnsAndStreams(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "I will look at that."}
	svc, _, recorder := newTestService(t, WithHeadManager(head))

	surface := &fakeSurface{name: SurfaceThread, cards: true}
	release, err := svc.RegisterSurface(surface)
	if err != nil {
		t.Fatalf("RegisterSurface() = %v", err)
	}
	defer release()

	reply, err := svc.Say(ctx, SurfaceThread, "  what is going on?  ")
	if err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if reply.Role != RoleAssistant || reply.Text != "I will look at that." {
		t.Fatalf("reply = %+v", reply)
	}

	page, err := svc.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	history := page.Messages
	if len(history) != 2 {
		t.Fatalf("history has %d messages, want the ask and the answer", len(history))
	}
	if history[0].Role != RoleUser || history[0].Text != "what is going on?" {
		t.Errorf("first message = %+v, want the trimmed ask first", history[0])
	}
	if history[0].Surface != SurfaceThread {
		t.Errorf("surface = %q, want the surface that said it", history[0].Surface)
	}
	if history[1].Role != RoleAssistant {
		t.Errorf("second message = %+v, want the reply", history[1])
	}

	var messages, deltas int
	for _, event := range recorder.Events() {
		switch event.Type {
		case EventMessage:
			messages++
		case EventDelta:
			deltas++
		}
	}
	if messages != 2 {
		t.Errorf("pushed %d messages, want 2", messages)
	}
	if deltas == 0 {
		t.Error("the reply must stream: a channel message is whole, so the delta push is the only streaming there is")
	}
	if surface.countOf(ItemMessage) != 1 {
		t.Errorf("surface got %d messages, want the reply", surface.countOf(ItemMessage))
	}
}

// The socket's own path: the ask is stored and pushed before the turn, and the
// reply arrives on its own.
func TestSayAsyncAnswersWithTheAskAndRepliesLater(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: "on it"}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	ask, err := svc.SayAsync(ctx, SurfaceThread, "have a look at the reconnect")
	if err != nil {
		t.Fatalf("SayAsync() = %v", err)
	}
	if ask.Role != RoleUser || ask.Text != "have a look at the reconnect" {
		t.Fatalf("ask = %+v, want the operator's own turn", ask)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := svc.History(ctx, "", 10)
		if err != nil {
			t.Fatalf("History() = %v", err)
		}
		if len(page.Messages) == 2 {
			if page.Messages[1].Role != RoleAssistant || page.Messages[1].Text != "on it" {
				t.Errorf("reply = %+v, want the head's answer", page.Messages[1])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("history = %+v, want the reply to have landed", page.Messages)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSayRefusesNothingAndAnUnnamedSurface(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t, WithHeadManager(&fakeHead{reply: "ok"}))

	if _, err := svc.Say(ctx, SurfaceThread, "   "); err == nil {
		t.Error("Say() accepted an empty message")
	}
	if _, err := svc.Say(ctx, "Thread Two!", "hello"); err == nil {
		t.Error("Say() accepted a surface name that is not a surface name")
	}
}

// With no head wired there is nobody to answer, and the ask is still recorded:
// a message the operator sent must not vanish because a subprocess could not
// start.
//
// The failure is recorded too, as the assistant's own sentence. A surface waits
// for a stored assistant turn — the thread arms its composer at the send and
// releases it on the reply — so a turn that ends in silence is a shut composer
// and nothing to read.
func TestSayKeepsTheAskAndSaysTheTurnFailed(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	if _, err := svc.Say(ctx, SurfaceThread, "are you there?"); err == nil {
		t.Fatal("Say() answered without a head")
	}

	page, err := svc.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	history := page.Messages
	if len(history) != 2 {
		t.Fatalf("history = %+v, want the ask and a word about the failure", history)
	}
	if history[0].Role != RoleUser {
		t.Errorf("first message = %+v, want the ask kept", history[0])
	}
	if history[1].Role != RoleAssistant || history[1].Text != turnFailedText {
		t.Errorf("second message = %+v, want the server's own sentence", history[1])
	}
}

// A head that answers nothing at all still owes the surface a turn: the head may
// have done nothing but call verbs, and a silent reply is indistinguishable from
// a turn still running.
func TestASilentTurnStillStoresAReply(t *testing.T) {
	ctx := context.Background()
	head := &fakeHead{reply: ""}
	svc, _, _ := newTestService(t, WithHeadManager(head))

	stored, err := svc.Say(ctx, SurfaceThread, "just file that away")
	if err != nil {
		t.Fatalf("Say() = %v", err)
	}
	if stored.Role != RoleAssistant || stored.Text != turnSilentText {
		t.Fatalf("Say() = %+v, want the server's own sentence", stored)
	}

	page, err := svc.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("history = %+v, want the ask and a reply", page.Messages)
	}
}

// Reading the conversation must not create it. `assistant.history` is on the
// socket's read lane, whose membership is the claim that the handler mutates
// nothing a later request could observe out of order.
func TestHistoryDoesNotCreateTheConversation(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)

	page, err := svc.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	if len(page.Messages) != 0 {
		t.Errorf("history = %+v, want nothing on a fresh machine", page.Messages)
	}
	if _, err := queries.GetAssistantChannel(ctx); err == nil {
		t.Error("reading the conversation created the channel row")
	}
	if _, err := queries.GetAssistantState(ctx); err == nil {
		t.Error("reading the conversation stamped the state row")
	}
}

// A call's turns land in the same conversation, marked with the surface and
// the call, so what was agreed on a drive is in the thread.
func TestMirrorWritesACallsTurns(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	if _, err := svc.Mirror(ctx, SurfaceVoice, "call-1", RoleUser, "add a retry"); err != nil {
		t.Fatalf("Mirror() = %v", err)
	}
	if _, err := svc.Mirror(ctx, SurfaceVoice, "call-1", RoleAssistant, "sent it"); err != nil {
		t.Fatalf("Mirror() = %v", err)
	}
	if _, err := svc.Mirror(ctx, SurfaceVoice, "call-1", "narrator", "..."); err == nil {
		t.Error("Mirror() accepted a role that is not a role")
	}

	page, err := svc.History(ctx, "", 10)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	history := page.Messages
	if len(history) != 2 {
		t.Fatalf("history has %d messages, want both call turns", len(history))
	}
	for _, msg := range history {
		if msg.Surface != SurfaceVoice || msg.CallID != "call-1" {
			t.Errorf("message = %+v, want it marked as the call's", msg)
		}
	}
	if history[0].Role != RoleUser || history[1].Role != RoleAssistant {
		t.Error("a mirrored turn must land under the speaker who said it")
	}
}

// The first look is the news; the second look is empty, because looking is
// what stamps it.
// The rail row's notch: a count before the look, nothing after it. Look is the
// stamp alone, so what the thread rendered from the pure journal read is what
// it acknowledges.
func TestLookClearsTheUnseenCount(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	for _, s := range []string{"first", "second"} {
		if _, err := svc.appendJournal(ctx, journalWrite{Kind: JournalNote, Summary: s}); err != nil {
			t.Fatalf("appendJournal() = %v", err)
		}
	}

	n, err := svc.UnseenCount(ctx, SurfaceThread)
	if err != nil {
		t.Fatalf("UnseenCount() = %v", err)
	}
	if n != 2 {
		t.Fatalf("unseen = %d, want 2 before any look", n)
	}
	// Counting is a read: it must not have stamped anything.
	if n2, _ := svc.UnseenCount(ctx, SurfaceThread); n2 != 2 {
		t.Fatalf("a second count saw %d, want 2: counting must not be a look", n2)
	}

	if err := svc.Look(ctx, SurfaceThread); err != nil {
		t.Fatalf("Look() = %v", err)
	}
	n, err = svc.UnseenCount(ctx, SurfaceThread)
	if err != nil {
		t.Fatalf("UnseenCount() = %v", err)
	}
	if n != 0 {
		t.Errorf("unseen = %d after a look, want 0", n)
	}
	// Per surface: the thread's look is not the call's.
	if v, _ := svc.UnseenCount(ctx, SurfaceVoice); v != 2 {
		t.Errorf("voice unseen = %d, want 2: a look is per surface", v)
	}
	// Surfaces are an open set (a gateway adds one without a migration) and
	// the guard is the name pattern, because the name is interpolated into a
	// JSON path: a dot or a quote would address a different key.
	if err := svc.Look(ctx, "Kiosk.1"); err == nil {
		t.Error("Look() accepted a surface name that is not one")
	}
}

func TestSinceLastIsNewsThenNothing(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind: JournalNote, Summary: "the rail should be quieter", Notable: true,
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}

	first, err := svc.SinceLast(ctx, SurfaceThread)
	if err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}
	if len(first.Journal) != 1 {
		t.Fatalf("first look saw %d entries, want 1", len(first.Journal))
	}
	if first.Since != "" {
		t.Errorf("Since = %q, want empty on a surface that had never looked", first.Since)
	}
	if first.LookedAt == "" {
		t.Error("a look must leave a mark behind")
	}

	second, err := svc.SinceLast(ctx, SurfaceThread)
	if err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}
	if len(second.Journal) != 0 {
		t.Errorf("second look saw %d entries, want none", len(second.Journal))
	}
	if second.Since == "" {
		t.Error("the second look must measure against the first look's mark")
	}
}

// A look is bounded and the journal is not. Stamping only the rows returned
// left the backlog behind it unseen, so the next look answered with the next
// fifty OLDER entries — announcing last week after today, for as many looks as
// it took to drain. A look means "caught up to here".
func TestSinceLastCatchesUpPastOnePage(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	for i := range maxSinceLastJournal + 10 {
		if _, err := svc.appendJournal(ctx, journalWrite{
			Kind: JournalNote, Summary: fmt.Sprintf("thing %d", i),
		}); err != nil {
			t.Fatalf("appendJournal() = %v", err)
		}
	}

	first, err := svc.SinceLast(ctx, SurfaceThread)
	if err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}
	if len(first.Journal) != maxSinceLastJournal {
		t.Fatalf("first look saw %d entries, want the page cap of %d",
			len(first.Journal), maxSinceLastJournal)
	}

	second, err := svc.SinceLast(ctx, SurfaceThread)
	if err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}
	if len(second.Journal) != 0 {
		t.Errorf("second look saw %d entries, want none — the backlog is behind the first look, "+
			"not ahead of it", len(second.Journal))
	}
}

// Two surfaces have two marks: the thread reading the news must not consume the
// call's.
func TestSinceLastIsPerSurface(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	if _, err := svc.appendJournal(ctx, journalWrite{Kind: JournalNote, Summary: "something"}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}
	if _, err := svc.SinceLast(ctx, SurfaceThread); err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}

	voice, err := svc.SinceLast(ctx, SurfaceVoice)
	if err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}
	if len(voice.Journal) != 1 {
		t.Errorf("the call saw %d entries, want the one the thread read", len(voice.Journal))
	}
}

// What was said on a call while the thread was away is part of what the thread
// has missed.
func TestSinceLastCarriesTheConversation(t *testing.T) {
	ctx := context.Background()
	// A movable clock rather than a sleep. The mark is whole seconds and a
	// message's stamp is fractional, so a message written inside the same second
	// as the mark reads as already seen — deliberately, and the reason this test
	// steps the clock rather than writing twice in one second.
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	svc, _, _ := newTestService(t, WithClock(func() time.Time { return now }))

	if _, err := svc.SinceLast(ctx, SurfaceThread); err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}
	now = now.Add(2 * time.Second)
	if _, err := svc.Mirror(ctx, SurfaceVoice, "call-1", RoleUser, "ship it"); err != nil {
		t.Fatalf("Mirror() = %v", err)
	}

	update, err := svc.SinceLast(ctx, SurfaceThread)
	if err != nil {
		t.Fatalf("SinceLast() = %v", err)
	}
	if len(update.Messages) != 1 || update.Messages[0].Text != "ship it" {
		t.Errorf("messages = %+v, want what was said on the call", update.Messages)
	}
}

func TestFollowIsDurableAndUnfollowIsForgiving(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)
	session := seedSession(t, queries)

	if err := svc.Follow(ctx, session.ID, "operator"); err != nil {
		t.Fatalf("Follow() = %v", err)
	}
	if err := svc.Follow(ctx, session.ID, "dispatch"); err != nil {
		t.Fatalf("Follow() twice = %v", err)
	}
	follows, err := svc.Follows(ctx)
	if err != nil {
		t.Fatalf("Follows() = %v", err)
	}
	if len(follows) != 1 || follows[0].Source != "dispatch" {
		t.Fatalf("follows = %+v, want one row naming the latest asker", follows)
	}

	if err := svc.MarkBriefed(ctx, session.ID, true); err != nil {
		t.Fatalf("MarkBriefed() = %v", err)
	}
	if follows, _ := svc.Follows(ctx); follows[0].Briefed != 1 {
		t.Error("briefed did not stick — a greeting would re-brief on every call")
	}

	if err := svc.Unfollow(ctx, session.ID); err != nil {
		t.Fatalf("Unfollow() = %v", err)
	}
	if err := svc.Unfollow(ctx, session.ID); err != nil {
		t.Errorf("Unfollow() on a session that is not followed = %v, want the state asked for", err)
	}
	if svc.Following(ctx, session.ID) {
		t.Error("Following() = true after unfollow")
	}
}

func TestRegisterSurfaceRefusesANameThatIsNotOne(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.RegisterSurface(&fakeSurface{name: "thread.two"}); err == nil {
		t.Error("a surface name is a JSON path component; a dot must be refused")
	}
	if _, err := svc.RegisterSurface(nil); err == nil {
		t.Error("RegisterSurface(nil) must refuse")
	}
}

// A surface that cannot render is that surface's problem: delivery failures
// are logged and never propagated to the runtime or an MCP handler.
func TestDeliveryToleratesABrokenSurface(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	broken := &fakeSurface{name: "broken", fail: true}
	good := &fakeSurface{name: SurfaceThread}
	if _, err := svc.RegisterSurface(broken); err != nil {
		t.Fatalf("RegisterSurface() = %v", err)
	}
	release, err := svc.RegisterSurface(good)
	if err != nil {
		t.Fatalf("RegisterSurface() = %v", err)
	}

	svc.deliver(ctx, Item{Kind: ItemNotice, Notice: &Notice{Kind: NoticeFinished, Headline: "done"}})
	if good.countOf(ItemNotice) != 1 {
		t.Error("a broken surface must not stop delivery to a working one")
	}

	release()
	release() // idempotent
	svc.deliver(ctx, Item{Kind: ItemNotice, Notice: &Notice{Kind: NoticeFinished, Headline: "again"}})
	if good.countOf(ItemNotice) != 1 {
		t.Error("a released surface still received an item")
	}
}

// History pages backwards and reads forwards.
func TestHistoryPagesOldestFirst(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestService(t)

	for _, text := range []string{"one", "two", "three"} {
		if _, err := svc.Mirror(ctx, SurfaceThread, "", RoleUser, text); err != nil {
			t.Fatalf("Mirror() = %v", err)
		}
	}

	page, err := svc.History(ctx, "", 2)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	if len(page.Messages) != 2 || page.Messages[0].Text != "two" || page.Messages[1].Text != "three" {
		t.Fatalf("page = %+v, want the newest two in reading order", page.Messages)
	}
	if page.Before == "" {
		t.Fatal("a full page must offer a cursor for the page behind it")
	}

	earlier, err := svc.History(ctx, page.Before, 2)
	if err != nil {
		t.Fatalf("History() = %v", err)
	}
	if len(earlier.Messages) != 1 || earlier.Messages[0].Text != "one" {
		t.Errorf("earlier = %+v, want the page before the cursor", earlier.Messages)
	}
	if earlier.Before != "" {
		t.Error("the start of the conversation must not offer another cursor")
	}

	// A cursor a client mangled must not silently answer with the newest page,
	// which is how a client loops forever without reaching the start.
	if _, err := svc.History(ctx, "not-a-cursor", 2); err == nil {
		t.Error("History() accepted a malformed cursor")
	}
}

func TestCloseStopsNothingItNeverStarted(t *testing.T) {
	svc, _, _ := newTestService(t)
	if err := svc.Close(); err != nil {
		t.Errorf("Close() on a service with no head = %v", err)
	}
	if svc.HeadIsUp() {
		t.Error("HeadIsUp() = true without a Say")
	}
}

// An unknown journal kind is refused rather than written: the set is closed so
// that the digest and compaction can rely on it.
func TestJournalRefusesAKindOutsideTheSet(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.appendJournal(context.Background(), journalWrite{Kind: "vibes"}); err == nil {
		t.Error("appendJournal() accepted a kind outside the closed set")
	}
}
