package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
)

// blockingRunner stands in for the provider CLI on a session whose summary
// never lands inside anyone's patience — which is what the field report showed
// happening on every call.
type blockingRunner struct {
	started atomic.Int64
	release chan struct{}
}

func (r *blockingRunner) RunBlocking(ctx context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	r.started.Add(1)
	select {
	case <-r.release:
		return &claudecli.BlockingResult{Text: "It has been rewriting the voice handler."}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Call open reads the cache and nothing else.
//
// The summary is a nice-to-have for the drafter; the microphone is the feature.
// Waiting for a provider-CLI subprocess here is what held the socket open and
// silent for the whole budget, and then for nothing when the budget expired.
func TestCachedNeverWaitsOnTheSummariser(t *testing.T) {
	runner := &blockingRunner{release: make(chan struct{})}
	defer close(runner.release)
	s := newSessionSummarizer(runner, nil, "test-model")

	done := make(chan string, 1)
	go func() { done <- s.Cached("sess-1") }()

	select {
	case got := <-done:
		if got != "" {
			t.Errorf("Cached returned %q with nothing cached, want empty", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Cached blocked — call open must never wait on the summariser")
	}

	if n := runner.started.Load(); n != 0 {
		t.Errorf("Cached ran the provider CLI %d times, want 0", n)
	}
}

// A summary that has landed is free, and that is the only condition on which
// the drafter gets one.
func TestCachedReturnsAFreshSummaryAndForgetsAStaleOne(t *testing.T) {
	s := newSessionSummarizer(&blockingRunner{}, nil, "test-model")

	s.cache["sess-1"] = summaryEntry{text: "Rewriting the voice handler.", at: time.Now()}
	if got := s.Cached("sess-1"); got != "Rewriting the voice handler." {
		t.Errorf("Cached = %q, want the cached paragraph", got)
	}

	s.cache["sess-1"] = summaryEntry{text: "Stale.", at: time.Now().Add(-2 * summaryTTL)}
	if got := s.Cached("sess-1"); got != "" {
		t.Errorf("Cached = %q for an expired entry, want empty", got)
	}
}

// Two askers for the same session share one subprocess.
//
// There are genuinely two now — opening a call warms the summary, and the
// operator asking what a session has been doing goes down the same path a
// moment later — and spawning a second CLI for an answer already on its way is
// the pressure this area was failing under.
func TestSummaryJoinsARunAlreadyUnderWay(t *testing.T) {
	s := newSessionSummarizer(&blockingRunner{}, nil, "test-model")

	first, mine := s.claim("sess-1")
	if !mine {
		t.Fatal("the first claim did not take ownership")
	}
	second, mine := s.claim("sess-1")
	if mine {
		t.Fatal("a second claim took ownership too; that is two subprocesses for one answer")
	}
	if second != first {
		t.Fatal("the joiner waits on a different channel from the one the owner closes")
	}

	// A different session is its own run, not a queue behind this one.
	if _, mine := s.claim("sess-2"); !mine {
		t.Error("a second session had to wait on the first")
	}

	s.release("sess-1", first)
	select {
	case <-first:
	default:
		t.Fatal("release left the joiner waiting")
	}
	// A failed run must wake its joiners too, and leave nothing behind that
	// would make the next asker wait forever.
	if _, mine := s.claim("sess-1"); !mine {
		t.Error("the finished run stayed claimed; the next asker would block on it")
	}
}

// Warming is a background gesture: it returns at once and the work outlives the
// request that asked for it.
func TestWarmReturnsImmediately(t *testing.T) {
	runner := &blockingRunner{release: make(chan struct{})}
	defer close(runner.release)
	s := newSessionSummarizer(runner, nil, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Warm(ctx, "sess-1") }()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Warm blocked; it exists precisely so nothing waits")
	}

	// A nil summariser is the configured-off case and must stay a no-op.
	var off *sessionSummarizer
	off.Warm(context.Background(), "sess-1")
	if got := off.Cached("sess-1"); got != "" {
		t.Errorf("a disabled summariser returned %q", got)
	}
}
