package msggen

import (
	"context"
	"errors"
	"testing"
	"time"

	claudecli "github.com/allbin/claudecli-go"
)

type mockRunner struct {
	calls   int
	results []*claudecli.BlockingResult
	errs    []error
}

func (m *mockRunner) RunBlocking(_ context.Context, _ string, _ ...claudecli.Option) (*claudecli.BlockingResult, error) {
	i := m.calls
	m.calls++
	if i >= len(m.errs) {
		return &claudecli.BlockingResult{Text: "ok"}, nil
	}
	return m.results[i], m.errs[i]
}

func TestRunWithRetry_Success(t *testing.T) {
	r := &mockRunner{
		errs:    []error{nil},
		results: []*claudecli.BlockingResult{{Text: "ok"}},
	}
	result, err := RunWithRetry(context.Background(), r, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "ok" {
		t.Errorf("Text = %q, want ok", result.Text)
	}
	if r.calls != 1 {
		t.Errorf("calls = %d, want 1", r.calls)
	}
}

func TestRunWithRetry_RateLimitThenSuccess(t *testing.T) {
	r := &mockRunner{
		errs: []error{
			&claudecli.RateLimitError{RetryAfter: 10 * time.Millisecond, Message: "slow"},
			nil,
		},
		results: []*claudecli.BlockingResult{
			nil,
			{Text: "ok"},
		},
	}
	result, err := RunWithRetry(context.Background(), r, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "ok" {
		t.Errorf("Text = %q, want ok", result.Text)
	}
	if r.calls != 2 {
		t.Errorf("calls = %d, want 2", r.calls)
	}
}

func TestRunWithRetry_OverloadedThenSuccess(t *testing.T) {
	r := &mockRunner{
		errs: []error{
			claudecli.ErrOverloaded,
			nil,
		},
		results: []*claudecli.BlockingResult{
			nil,
			{Text: "ok"},
		},
	}

	// Override retryDelay via short context timeout behavior — just verify it retries.
	// The actual delay (5s) is too long for tests, so we use a generous timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := RunWithRetry(ctx, r, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "ok" {
		t.Errorf("Text = %q, want ok", result.Text)
	}
	if r.calls != 2 {
		t.Errorf("calls = %d, want 2", r.calls)
	}
}

func TestRunWithRetry_AuthFailsFast(t *testing.T) {
	r := &mockRunner{
		errs:    []error{claudecli.ErrAuth},
		results: []*claudecli.BlockingResult{nil},
	}
	_, err := RunWithRetry(context.Background(), r, "test")
	if !errors.Is(err, claudecli.ErrAuth) {
		t.Errorf("expected ErrAuth, got %v", err)
	}
	if r.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", r.calls)
	}
}

func TestRunWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := &mockRunner{
		errs: []error{
			&claudecli.RateLimitError{RetryAfter: time.Hour, Message: "slow"},
		},
		results: []*claudecli.BlockingResult{nil},
	}
	_, err := RunWithRetry(ctx, r, "test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRunWithRetry_ExhaustedRetries(t *testing.T) {
	rlErr := &claudecli.RateLimitError{RetryAfter: 10 * time.Millisecond, Message: "slow"}
	r := &mockRunner{
		errs:    []error{rlErr, rlErr, rlErr},
		results: []*claudecli.BlockingResult{nil, nil, nil},
	}
	_, err := RunWithRetry(context.Background(), r, "test")
	if !errors.Is(err, claudecli.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got %v", err)
	}
	if r.calls != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", r.calls)
	}
}

func TestRetryDelay_RateLimitWithRetryAfter(t *testing.T) {
	err := &claudecli.RateLimitError{RetryAfter: 42 * time.Second, Message: "slow"}
	d := retryDelay(err, 0)
	if d != 42*time.Second {
		t.Errorf("delay = %v, want 42s", d)
	}
}

func TestRetryDelay_RateLimitFallback(t *testing.T) {
	err := &claudecli.RateLimitError{Message: "slow"}
	d := retryDelay(err, 0)
	if d != 30*time.Second {
		t.Errorf("delay = %v, want 30s", d)
	}
}

func TestRetryDelay_Overloaded(t *testing.T) {
	d0 := retryDelay(claudecli.ErrOverloaded, 0)
	d1 := retryDelay(claudecli.ErrOverloaded, 1)
	if d0 != 5*time.Second {
		t.Errorf("attempt 0 delay = %v, want 5s", d0)
	}
	if d1 != 10*time.Second {
		t.Errorf("attempt 1 delay = %v, want 10s", d1)
	}
}

func TestRetryDelay_NonRetriable(t *testing.T) {
	for _, err := range []error{claudecli.ErrAuth, claudecli.ErrAPI, errors.New("random")} {
		d := retryDelay(err, 0)
		if d != 0 {
			t.Errorf("retryDelay(%v) = %v, want 0", err, d)
		}
	}
}
