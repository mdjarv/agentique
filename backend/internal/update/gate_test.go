package update

import (
	"errors"
	"testing"
	"time"
)

// The drain gate: arm while busy, fire on the next turn end, give up on a
// deadline, and forget everything on a restart (docs/upgrades.md).

func TestArmWaitsForIdleThenFires(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("the new binary"))
	h := newHarness(t, fr, "v0.4.1")
	h.busy = []string{"session-a"}

	armed, err := h.applier.Arm(KindRelease, "v9.9.9", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if armed.Target != "v9.9.9" || armed.DeadlineAt == "" {
		t.Fatalf("arming = %+v", armed)
	}

	// Still busy: nothing must have happened.
	time.Sleep(50 * time.Millisecond)
	assertFile(t, h.target, "the old binary")
	if h.applier.Arming() == nil {
		t.Fatal("should still be armed while busy")
	}

	// The last turn ends.
	h.busy = nil
	h.applier.OnTurnEnd("session-a")

	h.waitPhase(t, PhaseRestarting)
	assertFile(t, h.target, "the new binary")
	if h.applier.Arming() != nil {
		t.Fatal("a fired one-shot must not stay armed")
	}
}

func TestArmOnAnIdleMachineFiresImmediately(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1") // not busy

	if _, err := h.applier.Arm(KindRelease, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	// No turn will ever end on an idle machine — arming must not wait for one.
	h.waitPhase(t, PhaseRestarting)
	assertFile(t, h.target, "new")
}

func TestArmedUpgradeIsCancellable(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	h.busy = []string{"session-a"}

	if _, err := h.applier.Arm(KindRelease, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := h.applier.Disarm(); err != nil {
		t.Fatal(err)
	}
	if h.applier.Arming() != nil {
		t.Fatal("still armed after disarm")
	}

	// The turn ending must now do nothing at all.
	h.busy = nil
	h.applier.OnTurnEnd("session-a")
	time.Sleep(200 * time.Millisecond)
	assertFile(t, h.target, "the old binary")
}

func TestDisarmWithNothingArmed(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	if err := h.applier.Disarm(); !errors.Is(err, ErrNotArmed) {
		t.Fatalf("got %v, want ErrNotArmed", err)
	}
}

func TestArmingTwiceIsRefused(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	h.busy = []string{"session-a"}

	if _, err := h.applier.Arm(KindRelease, "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := h.applier.Arm(KindRelease, "", time.Hour); !errors.Is(err, ErrAlreadyArmed) {
		t.Fatalf("got %v, want ErrAlreadyArmed", err)
	}
}

func TestArmedUpgradeExpires(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	h.busy = []string{"session-a"}

	// A deadline already in the past: the next evaluation must give up.
	if _, err := h.applier.Arm(KindRelease, "", time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	h.busy = nil
	h.applier.OnTurnEnd("session-a")

	deadline := time.After(5 * time.Second)
	for h.applier.Arming() != nil {
		select {
		case <-deadline:
			t.Fatal("an expired arming must disarm itself")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// And it must SAY so rather than going quiet.
	p := h.applier.Progress()
	if p == nil || p.Phase != PhaseFailed || p.Error == "" {
		t.Fatalf("expiry must be reported, got %+v", p)
	}
	assertFile(t, h.target, "the old binary")
}

func TestArmRefusedWhenTheMachineCannotUpgrade(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	h.applier.deps.ServiceInstalled = func() bool { return false }

	// No point arming something that could never fire.
	if _, err := h.applier.Arm(KindRelease, "", time.Hour); err == nil {
		t.Fatal("arming must run the same preflight as applying")
	}
	if h.applier.Arming() != nil {
		t.Fatal("a refused arm must leave nothing armed")
	}
}

func TestArmingIsForgottenByAFreshApplier(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")
	h.busy = []string{"session-a"}
	if _, err := h.applier.Arm(KindRelease, "", time.Hour); err != nil {
		t.Fatal(err)
	}

	// Armed state is in-memory only: an upgrade armed on Tuesday must not fire
	// on Thursday because the server happened to restart. A new Applier over
	// the same machine knows nothing about it.
	fresh := NewApplier(h.applier.checker, h.applier.deps)
	if fresh.Arming() != nil {
		t.Fatal("arming must not survive a restart")
	}
}

func TestTurnEndOnAnUnarmedApplierDoesNothing(t *testing.T) {
	fr := newFakeRelease(t, "v9.9.9", []byte("new"))
	h := newHarness(t, fr, "v0.4.1")

	h.applier.OnTurnEnd("session-a")
	time.Sleep(200 * time.Millisecond)
	assertFile(t, h.target, "the old binary")
	if h.applier.Progress() != nil {
		t.Fatal("an unarmed machine must not start anything on a turn end")
	}
}
