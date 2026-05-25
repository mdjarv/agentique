package session

import (
	"sort"
	"testing"
	"time"

	"github.com/allbin/agentkit/runtime"
)

// TestDrainSyntheticApprovals_DeniesAndClears verifies the helper used by
// Close and Interrupt: every pending synthetic approval is signalled deny,
// removed from the map, and its ID returned for broadcast purposes.
func TestDrainSyntheticApprovals_DeniesAndClears(t *testing.T) {
	sess := newPermTestSession("manual", "default")
	defer sess.cancelCtx()

	ch1 := make(chan *runtime.Decision, 1)
	ch2 := make(chan *runtime.Decision, 1)
	sess.syntheticApprovals["a1"] = &syntheticApproval{id: "a1", ch: ch1}
	sess.syntheticApprovals["a2"] = &syntheticApproval{id: "a2", ch: ch2}

	ids := sess.drainSyntheticApprovals("test reason")
	sort.Strings(ids)
	want := []string{"a1", "a2"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("drain ids = %v, want %v", ids, want)
	}
	if len(sess.syntheticApprovals) != 0 {
		t.Errorf("syntheticApprovals not cleared, %d entries remain", len(sess.syntheticApprovals))
	}
	for name, ch := range map[string]chan *runtime.Decision{"ch1": ch1, "ch2": ch2} {
		select {
		case resp := <-ch:
			if resp == nil {
				t.Errorf("%s: nil response", name)
				continue
			}
			if resp.Allow {
				t.Errorf("%s: drained approval should be denied", name)
			}
			if resp.DenyMessage != "test reason" {
				t.Errorf("%s: deny message = %q, want %q", name, resp.DenyMessage, "test reason")
			}
		default:
			t.Errorf("%s: drain did not signal channel", name)
		}
	}
}

// TestDrainSyntheticApprovals_Empty is a no-op when nothing is pending.
func TestDrainSyntheticApprovals_Empty(t *testing.T) {
	sess := newPermTestSession("manual", "default")
	defer sess.cancelCtx()

	ids := sess.drainSyntheticApprovals("unused")
	if ids != nil {
		t.Errorf("expected nil for empty pending, got %v", ids)
	}
}

// TestDrainSyntheticApprovals_NonBlocking_NoReceiver verifies that a stale
// approval whose channel buffer is full (no receiver ready) is still cleared
// — Interrupt must not hang on it.
func TestDrainSyntheticApprovals_NonBlocking_NoReceiver(t *testing.T) {
	sess := newPermTestSession("manual", "default")
	defer sess.cancelCtx()

	full := make(chan *runtime.Decision, 1)
	full <- &runtime.Decision{Allow: true} // pre-fill so drain's send is dropped
	sess.syntheticApprovals["stuck"] = &syntheticApproval{id: "stuck", ch: full}

	done := make(chan struct{})
	go func() {
		sess.drainSyntheticApprovals("interrupted")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drain blocked on full channel")
	}
	if _, present := sess.syntheticApprovals["stuck"]; present {
		t.Error("stuck approval still in map after drain")
	}
}
