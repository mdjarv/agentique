package session

import (
	"context"
	"strings"
	"testing"
)

// injectRecall fires at most once per live session instance, prepends the recall
// block to the prompt, and degrades cleanly when recall is disabled or empty.
func TestInjectRecall(t *testing.T) {
	t.Run("no recall fn leaves the prompt unchanged", func(t *testing.T) {
		sess := &Session{ID: "s1"}
		if got := sess.injectRecall("build the thing"); got != "build the thing" {
			t.Fatalf("prompt = %q, want unchanged", got)
		}
	})

	t.Run("prepends the block on the first turn, then never again", func(t *testing.T) {
		var calls int
		sess := &Session{ID: "s1"}
		sess.SetRecallFn(func(_ context.Context, prompt string) string {
			calls++
			return "> recalled: " + prompt
		})

		got := sess.injectRecall("build the thing")
		if !strings.HasPrefix(got, "> recalled: build the thing") || !strings.HasSuffix(got, "build the thing") {
			t.Fatalf("first turn should prepend the block, got %q", got)
		}
		if !strings.Contains(got, "\n\n") {
			t.Fatalf("block and prompt should be separated by a blank line, got %q", got)
		}

		// Second turn: fire-once gate — the prompt passes through untouched and the
		// recall fn is not called again (cost stays bounded to one lookup).
		if got2 := sess.injectRecall("next message"); got2 != "next message" {
			t.Fatalf("second turn should not inject, got %q", got2)
		}
		if calls != 1 {
			t.Fatalf("recall fn should fire exactly once, fired %d times", calls)
		}
	})

	t.Run("empty block leaves the prompt unchanged but still consumes the one-shot", func(t *testing.T) {
		var calls int
		sess := &Session{ID: "s1"}
		sess.SetRecallFn(func(_ context.Context, _ string) string {
			calls++
			return "   " // nothing relevant recalled
		})

		if got := sess.injectRecall("build the thing"); got != "build the thing" {
			t.Fatalf("empty block should leave prompt unchanged, got %q", got)
		}
		// The lookup happened; a second turn must not pay for another one.
		if got := sess.injectRecall("again"); got != "again" {
			t.Fatalf("second turn should pass through, got %q", got)
		}
		if calls != 1 {
			t.Fatalf("recall fn should fire exactly once even on a miss, fired %d times", calls)
		}
	})
}
