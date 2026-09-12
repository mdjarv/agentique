package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mustTime parses one of this package's own timestamps for a test clock.
func mustTime(at string) time.Time {
	t, err := time.Parse(timeFormat, at)
	if err != nil {
		panic(err)
	}
	return t
}

// The digest groups by the Needs-you ranking: what holds a process, then what
// broke, then what is owed a decision, then outcomes, then what sessions said.
// One order, the same one the notices and the deck's band use.
func TestDigestGroupsInTheNeedsYouOrder(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := proposalWorld(t)

	for _, entry := range []journalWrite{
		{Kind: JournalSessionFinished, SessionID: "s1", Summary: "the tests pass"},
		{Kind: JournalReport, SessionID: "s1", Summary: "the auth tests were already failing", Untrusted: true},
		{Kind: JournalSessionMerged, SessionID: "s1"},
		{Kind: JournalSessionFailed, SessionID: "s1", Summary: "the build broke", Untrusted: true},
		{Kind: JournalSessionBlocked, SessionID: "s1", Summary: "it wants to run rm", Untrusted: true},
		{Kind: JournalDispatched, SessionID: "s1", Summary: "sent a prompt"},
	} {
		if _, err := svc.appendJournal(ctx, entry); err != nil {
			t.Fatalf("appendJournal(%s) = %v", entry.Kind, err)
		}
	}
	if _, err := svc.Invoke(ctx, VerbArchiveSession, proposalArgs(VerbArchiveSession)); err != nil {
		t.Fatalf("Invoke() = %v", err)
	}

	msg, err := svc.Digest(ctx)
	if err != nil {
		t.Fatalf("Digest() = %v", err)
	}
	if msg.Kind != messageKindDigest {
		t.Errorf("kind = %q, want %q", msg.Kind, messageKindDigest)
	}
	if msg.Role != RoleAssistant {
		t.Errorf("role = %q, want the assistant's own", msg.Role)
	}

	text := msg.Text
	order := []string{"Waiting on you", "Failed", "Waiting for a yes", "Finished", "Merged and archived", "Reported"}
	at := -1
	for _, heading := range order {
		next := strings.Index(text, heading)
		if next < 0 {
			t.Fatalf("the digest has no %q section:\n%s", heading, text)
		}
		if next < at {
			t.Errorf("%q comes out of order:\n%s", heading, text)
		}
		at = next
	}

	// A session is named the way every other surface names it.
	if !strings.Contains(text, "the retry fix in riff") {
		t.Errorf("the digest does not place the session in its project:\n%s", text)
	}
	// Untrusted text is quoted and said to be quoted, in the line itself.
	if !strings.Contains(text, `quoting it: "the auth tests were already failing"`) {
		t.Errorf("a report is not quoted in the digest:\n%s", text)
	}
}

// Empty is one sentence saying so, and it still stamps the window: a digest
// that found nothing answered the question, and leaving the mark behind would
// make the next one repeat a window with nothing in it.
func TestAnEmptyDigestIsOneSentence(t *testing.T) {
	ctx := context.Background()
	svc, queries, _ := newTestService(t)

	msg, err := svc.Digest(ctx)
	if err != nil {
		t.Fatalf("Digest() = %v", err)
	}
	if msg.Text != digestEmptyText {
		t.Errorf("text = %q, want the one sentence", msg.Text)
	}

	state, err := queries.GetAssistantState(ctx)
	if err != nil {
		t.Fatalf("GetAssistantState() = %v", err)
	}
	if state.LastDigestAt == "" {
		t.Error("the digest did not stamp the window it covered")
	}
}

// The second digest covers what happened since the first.
func TestASecondDigestCoversOnlyWhatIsNew(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{at: mustTime("2026-09-12T09:00:00Z")}
	svc, _, _ := newTestService(t, WithClock(clock.now))

	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind: JournalSessionFinished, SessionID: "s1", Summary: "the first thing",
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}
	// A second between the entry and the digest, because the window is
	// inclusive at its boundary: an entry written in the same second as the
	// stamp appears in both digests, which is the direction that repeats news
	// rather than losing it.
	clock.advance(time.Second)
	first, err := svc.Digest(ctx)
	if err != nil {
		t.Fatalf("Digest() = %v", err)
	}
	if !strings.Contains(first.Text, "the first thing") {
		t.Fatalf("the first digest missed its own window:\n%s", first.Text)
	}

	clock.advance(time.Hour)
	if _, err := svc.appendJournal(ctx, journalWrite{
		Kind: JournalSessionFinished, SessionID: "s2", Summary: "the second thing",
	}); err != nil {
		t.Fatalf("appendJournal() = %v", err)
	}

	second, err := svc.Digest(ctx)
	if err != nil {
		t.Fatalf("Digest() = %v", err)
	}
	if !strings.Contains(second.Text, "the second thing") {
		t.Errorf("the second digest missed what is new:\n%s", second.Text)
	}
	if strings.Contains(second.Text, "the first thing") {
		t.Errorf("the second digest repeated the first window:\n%s", second.Text)
	}
}

// The head can post one, and what it gets back tells it not to read the whole
// thing out: the digest is already in front of the operator.
func TestTheDigestVerbPostsAndSaysNotToRepeatIt(t *testing.T) {
	svc, _, _ := newTestService(t)

	payload, err := svc.Invoke(context.Background(), VerbDigest, nil)
	if err != nil {
		t.Fatalf("Invoke(digest) = %v", err)
	}
	if posted, _ := payload["posted"].(bool); !posted {
		t.Fatalf("payload = %v, want a posted digest", payload)
	}
	note, _ := payload["note"].(string)
	if !strings.Contains(note, "do not repeat") {
		t.Errorf("note = %q, want it told not to read the digest out", note)
	}
}
