package assistant

import (
	"testing"
	"time"
)

// The ordering must match lib/session/priority.ts: the thing still holding a
// process outranks the thing that already stopped. One rule, both surfaces.
func TestNoticePriorityMatchesTheAttentionRule(t *testing.T) {
	t.Parallel()
	if NoticeBlocked.Priority() >= NoticeFailed.Priority() {
		t.Error("blocked must outrank failed — it is still holding a process")
	}
	if NoticeFailed.Priority() >= NoticeFinished.Priority() {
		t.Error("failed must outrank finished")
	}
}

func TestEndsWork(t *testing.T) {
	t.Parallel()
	if !NoticeFinished.EndsWork() || !NoticeFailed.EndsWork() {
		t.Error("a run that finished or failed is over")
	}
	// Blocked still holds the process, so the call stays in its working phase
	// and the short conversational idle rule must not come back.
	if NoticeBlocked.EndsWork() {
		t.Error("blocked must not end work — the run is stuck, not done")
	}
}

func TestNoticeReachesFollowers(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	f, other := &recorder{}, &recorder{}
	defer r.Follow("s1", f)()
	defer r.Follow("s2", other)()

	r.Notice("s1", Notice{Kind: NoticeFinished, Headline: "all tests pass"})

	if f.noticeCount() != 1 {
		t.Errorf("follower got %d notices, want 1", f.noticeCount())
	}
	if other.noticeCount() != 0 {
		t.Errorf("a follower of another session got %d notices, want 0", other.noticeCount())
	}
}

// The report budget must never apply to a notice. "You are reporting too
// often" is a sensible thing to tell a chatty agent and a nonsensical thing to
// say about "the run failed".
func TestNoticesAreNeverThrottled(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	now := time.Now()
	r.now = func() time.Time { return now }

	f := &recorder{}
	defer r.Follow("s1", f)()

	// Spend the whole report budget first.
	for range reportBurst + 2 {
		if _, err := r.Report("s1", "milestone", "chatter"); err != nil {
			t.Fatalf("report: %v", err)
		}
	}

	const notices = 6
	for range notices {
		r.Notice("s1", Notice{Kind: NoticeFailed, Headline: "it broke"})
	}
	if f.noticeCount() != notices {
		t.Errorf("delivered %d notices, want all %d — notices are not rate limited", f.noticeCount(), notices)
	}
}

// A notice with nobody listening is a no-op, not an error: unlike a report,
// nothing is waiting on the answer.
func TestNoticeWithNoFollowerIsSilent(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Notice("nobody", Notice{Kind: NoticeFinished, Headline: "done"})
}
