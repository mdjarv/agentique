package voice

import (
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseReport(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		headline string
		want     string
		wantErr  bool
	}{
		{name: "surprise", kind: "surprise", headline: "the auth tests were already failing", want: "the auth tests were already failing"},
		{name: "decision", kind: "decision", headline: "went with the middleware", want: "went with the middleware"},
		{name: "milestone", kind: "milestone", headline: "tests pass", want: "tests pass"},
		{name: "collapses whitespace", kind: "surprise", headline: "  two   spaces\nand a newline  ", want: "two spaces and a newline"},
		{name: "unknown kind", kind: "progress", headline: "opening a file", wantErr: true},
		{name: "empty headline", kind: "surprise", headline: "   ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReport(tt.kind, tt.headline)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseReport() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Headline != tt.want {
				t.Errorf("headline = %q, want %q", got.Headline, tt.want)
			}
		})
	}
}

// A verbose worker gets truncated, not rejected: it is mid-task, and failing
// its tool call over wordiness helps nobody.
func TestParseReportTruncatesRatherThanRejecting(t *testing.T) {
	long := strings.Repeat("a", maxHeadline*2)
	got, err := ParseReport("milestone", long)
	if err != nil {
		t.Fatalf("ParseReport() = %v, want a truncated report", err)
	}
	if n := len([]rune(got.Headline)); n > maxHeadline+1 {
		t.Errorf("headline kept %d runes, want <= %d plus an ellipsis", n, maxHeadline+1)
	}
	if !strings.HasSuffix(got.Headline, "…") {
		t.Error("a truncated headline should show that it was cut")
	}
}

type recorder struct {
	mu      sync.Mutex
	got     []Report
	notices []Notice
	fail    bool
}

func (r *recorder) Notify(_ string, rep Report) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errFailed
	}
	r.got = append(r.got, rep)
	return nil
}

func (r *recorder) NotifyRuntime(_ string, n Notice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errFailed
	}
	r.notices = append(r.notices, n)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *recorder) noticeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.notices)
}

var errFailed = &reportError{"follower refused"}

type reportError struct{ s string }

func (e *reportError) Error() string { return e.s }

// A report with nobody on the call must say so, so the worker can stop
// spending tool calls on an empty room.
func TestReportWithNoFollowerTellsTheWorker(t *testing.T) {
	r := NewRegistry()
	msg, err := r.Report("s1", "surprise", "nobody home")
	if err != nil {
		t.Fatalf("Report() = %v", err)
	}
	if !strings.Contains(msg, "Nobody is on the call") {
		t.Errorf("message = %q, want it to say nobody is listening", msg)
	}
}

func TestReportReachesFollowers(t *testing.T) {
	r := NewRegistry()
	a, b := &recorder{}, &recorder{}
	defer r.Follow("s1", a)()
	defer r.Follow("s1", b)()
	// A follower on another session must not receive it.
	other := &recorder{}
	defer r.Follow("s2", other)()

	if _, err := r.Report("s1", "surprise", "tests were already red"); err != nil {
		t.Fatalf("Report() = %v", err)
	}
	if a.count() != 1 || b.count() != 1 {
		t.Errorf("followers got %d and %d reports, want 1 each", a.count(), b.count())
	}
	if other.count() != 0 {
		t.Errorf("a follower of another session got %d reports, want 0", other.count())
	}
}

func TestUnfollowStopsDelivery(t *testing.T) {
	r := NewRegistry()
	f := &recorder{}
	release := r.Follow("s1", f)
	release()
	release() // idempotent

	if r.Listening("s1") {
		t.Error("Listening() = true after the only follower left")
	}
	if _, err := r.Report("s1", "milestone", "still here?"); err != nil {
		t.Fatalf("Report() = %v", err)
	}
	if f.count() != 0 {
		t.Errorf("got %d reports after unfollow, want 0", f.count())
	}
}

// The budget is the ceiling that catches a worker ignoring the prompt's
// guidance, and a throttled call must teach rather than fail.
func TestReportBudgetThrottles(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	r.now = func() time.Time { return now }

	f := &recorder{}
	defer r.Follow("s1", f)()

	for i := range reportBurst {
		msg, err := r.Report("s1", "milestone", "burst")
		if err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
		if !strings.Contains(msg, "Spoken") {
			t.Fatalf("report %d was not delivered: %q", i, msg)
		}
	}

	msg, err := r.Report("s1", "milestone", "one too many")
	if err != nil {
		t.Fatalf("throttled report returned an error: %v", err)
	}
	if !strings.Contains(msg, "too often") {
		t.Errorf("throttle message = %q, want it to explain the budget", msg)
	}
	if f.count() != reportBurst {
		t.Errorf("delivered %d reports, want the burst of %d", f.count(), reportBurst)
	}

	// A token comes back after the refill interval.
	now = now.Add(reportRefillEach)
	if _, err := r.Report("s1", "milestone", "after refill"); err != nil {
		t.Fatalf("post-refill report: %v", err)
	}
	if f.count() != reportBurst+1 {
		t.Errorf("delivered %d after refill, want %d", f.count(), reportBurst+1)
	}
}

// The budget is per session — one chatty run must not silence another.
func TestReportBudgetIsPerSession(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	r.now = func() time.Time { return now }

	noisy, quiet := &recorder{}, &recorder{}
	defer r.Follow("noisy", noisy)()
	defer r.Follow("quiet", quiet)()

	for range reportBurst + 2 {
		if _, err := r.Report("noisy", "milestone", "chatter"); err != nil {
			t.Fatalf("noisy report: %v", err)
		}
	}
	msg, err := r.Report("quiet", "surprise", "something real")
	if err != nil {
		t.Fatalf("quiet report: %v", err)
	}
	if !strings.Contains(msg, "Spoken") {
		t.Errorf("quiet session was throttled by the noisy one: %q", msg)
	}
}

// Dropping the last follower must drop the budget with it, or a second call
// inherits a bucket the first one spent.
func TestBudgetResetsWithTheLastFollower(t *testing.T) {
	r := NewRegistry()
	now := time.Now()
	r.now = func() time.Time { return now }

	first := &recorder{}
	release := r.Follow("s1", first)
	for range reportBurst {
		if _, err := r.Report("s1", "milestone", "spend it"); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	release()

	second := &recorder{}
	defer r.Follow("s1", second)()
	msg, err := r.Report("s1", "surprise", "fresh call")
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if !strings.Contains(msg, "Spoken") {
		t.Errorf("a new call inherited the previous call's spent budget: %q", msg)
	}
}

func TestReportRejectsBadInputBeforeSpendingBudget(t *testing.T) {
	r := NewRegistry()
	f := &recorder{}
	defer r.Follow("s1", f)()

	if _, err := r.Report("s1", "progress", "not a kind"); err == nil {
		t.Fatal("Report() accepted an unknown kind")
	}
	// The rejected call must not have cost a token.
	for i := range reportBurst {
		if _, err := r.Report("s1", "milestone", "ok"); err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
	}
	if f.count() != reportBurst {
		t.Errorf("delivered %d, want the full burst of %d", f.count(), reportBurst)
	}
}

func TestReportingInstructionsNameTheTool(t *testing.T) {
	got := ReportingInstructions("mcp__agentique__VoiceReport")
	if !strings.Contains(got, "mcp__agentique__VoiceReport") {
		t.Error("instructions must name the tool the worker has to call")
	}
	for _, want := range []string{"read aloud", "two or three"} {
		if !strings.Contains(strings.ToLower(got), want) {
			t.Errorf("instructions missing %q — that guidance is what keeps reports listenable", want)
		}
	}
}

// testLogger keeps test output quiet without leaving a nil *slog.Logger to
// panic on the first call.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
