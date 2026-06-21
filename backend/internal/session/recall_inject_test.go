package session

import (
	"context"
	"strings"
	"testing"
)

// injectRecall fires every turn, prepends the recall block, passes the already-seen
// ids for delta dedup, and degrades cleanly when recall is disabled or returns nothing.
func TestInjectRecall(t *testing.T) {
	t.Run("no recall fn leaves the prompt unchanged", func(t *testing.T) {
		sess := &Session{ID: "s1"}
		if got := sess.injectRecall("build the thing"); got != "build the thing" {
			t.Fatalf("prompt = %q, want unchanged", got)
		}
	})

	t.Run("fires every turn and dedups via the exclude set", func(t *testing.T) {
		var calls int
		var lastExclude map[string]struct{}
		sess := &Session{ID: "s1"}
		// First call surfaces id "a"; later calls must see "a" in the exclude set and
		// return nothing new.
		sess.SetRecallFn(func(_ context.Context, prompt string, exclude map[string]struct{}) (string, []string) {
			calls++
			lastExclude = exclude
			if _, seen := exclude["a"]; seen {
				return "", nil
			}
			return "> recalled: " + prompt, []string{"a"}
		})

		got := sess.injectRecall("first task")
		if !strings.HasPrefix(got, "> recalled: first task") || !strings.HasSuffix(got, "first task") {
			t.Fatalf("first turn should prepend the block, got %q", got)
		}
		if !strings.Contains(got, "\n\n") {
			t.Fatalf("block and prompt should be separated by a blank line, got %q", got)
		}

		// Second turn: the fn fires again (per-turn), but "a" is now excluded → no inject.
		if got2 := sess.injectRecall("second task"); got2 != "second task" {
			t.Fatalf("second turn should pass through (a already seen), got %q", got2)
		}
		if calls != 2 {
			t.Fatalf("recall fn should fire every turn, fired %d", calls)
		}
		if _, ok := lastExclude["a"]; !ok {
			t.Fatalf("second turn should pass the already-seen id in the exclude set")
		}
	})

	t.Run("empty block leaves the prompt unchanged and records nothing", func(t *testing.T) {
		var calls int
		sess := &Session{ID: "s1"}
		sess.SetRecallFn(func(_ context.Context, _ string, _ map[string]struct{}) (string, []string) {
			calls++
			return "", nil
		})
		if got := sess.injectRecall("build the thing"); got != "build the thing" {
			t.Fatalf("empty block should leave prompt unchanged, got %q", got)
		}
		if got := sess.injectRecall("again"); got != "again" {
			t.Fatalf("second turn should pass through, got %q", got)
		}
		if calls != 2 {
			t.Fatalf("recall fn should fire every turn even on misses, fired %d", calls)
		}
	})
}
