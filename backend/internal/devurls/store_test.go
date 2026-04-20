package devurls

import (
	"errors"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/config"
)

func twoSlots() []config.DevURLSlot {
	return []config.DevURLSlot{
		{Slot: "dev1", Port: 9210, PublicHost: "dev1.example.com"},
		{Slot: "dev2", Port: 9211, PublicHost: "dev2.example.com"},
	}
}

// newTestStore creates a Store with probe/findOwner stubs so tests don't touch
// real TCP state. busyPorts lists ports the stub probe reports as already bound.
func newTestStore(slots []config.DevURLSlot, busyPorts ...int) *Store {
	busy := make(map[int]bool, len(busyPorts))
	for _, p := range busyPorts {
		busy[p] = true
	}
	probe := func(port int) error {
		if busy[port] {
			return errors.New("address in use")
		}
		return nil
	}
	findOwner := func(port int) (*PortOwner, error) {
		if busy[port] {
			return &PortOwner{PID: 12345, Cmdline: "fake process"}, nil
		}
		return nil, nil
	}
	return NewStoreWithProbes(slots, probe, findOwner)
}

func TestAcquire_AssignsFirstFreeSlot(t *testing.T) {
	s := newTestStore(twoSlots())

	res, err := s.Acquire("sess-A")
	if err != nil {
		t.Fatal(err)
	}
	lease := res.Lease
	if lease.Slot != "dev1" || lease.Port != 9210 || lease.PublicHost != "dev1.example.com" || lease.SessionID != "sess-A" {
		t.Errorf("first acquire wrong: %+v", lease)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("no conflicts expected, got %+v", res.Skipped)
	}

	res2, err := s.Acquire("sess-B")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Lease.Slot != "dev2" {
		t.Errorf("second acquire should pick dev2, got %s", res2.Lease.Slot)
	}
}

func TestAcquire_AllBusyReturnsError(t *testing.T) {
	s := newTestStore(twoSlots())
	_, _ = s.Acquire("sess-A")
	_, _ = s.Acquire("sess-B")

	_, err := s.Acquire("sess-C")
	if !errors.Is(err, ErrAllBusy) {
		t.Errorf("want ErrAllBusy, got %v", err)
	}
}

func TestAcquire_SameSessionReturnsExistingLease(t *testing.T) {
	s := newTestStore(twoSlots())
	first, _ := s.Acquire("sess-A")

	again, err := s.Acquire("sess-A")
	if err != nil {
		t.Fatal(err)
	}
	if again.Lease.Slot != first.Lease.Slot {
		t.Errorf("same session should get same slot: first=%s again=%s", first.Lease.Slot, again.Lease.Slot)
	}

	// And no other slot was consumed.
	_, err = s.Acquire("sess-B")
	if err != nil {
		t.Fatalf("expected dev2 to be free: %v", err)
	}
	_, err = s.Acquire("sess-C")
	if !errors.Is(err, ErrAllBusy) {
		t.Errorf("third session should fail: got %v", err)
	}
}

func TestAcquire_SkipsExternallyBoundPorts(t *testing.T) {
	// dev1 port externally bound (orphan process), dev2 free.
	s := newTestStore(twoSlots(), 9210)

	res, err := s.Acquire("sess-A")
	if err != nil {
		t.Fatalf("should fall through to dev2: %v", err)
	}
	if res.Lease.Slot != "dev2" {
		t.Errorf("want dev2, got %s", res.Lease.Slot)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Slot != "dev1" {
		t.Errorf("want one skipped=dev1, got %+v", res.Skipped)
	}
	if res.Skipped[0].Owner == nil || res.Skipped[0].Owner.PID != 12345 {
		t.Errorf("expected owner pid=12345, got %+v", res.Skipped[0].Owner)
	}
}

func TestAcquire_AllPortsExternallyBusy(t *testing.T) {
	s := newTestStore(twoSlots(), 9210, 9211)

	res, err := s.Acquire("sess-A")
	if !errors.Is(err, ErrAllBusy) {
		t.Fatalf("want ErrAllBusy, got %v", err)
	}
	if len(res.Skipped) != 2 {
		t.Errorf("want 2 skipped conflicts, got %d", len(res.Skipped))
	}
}

func TestRelease_FreesSlot(t *testing.T) {
	s := newTestStore(twoSlots())
	_, _ = s.Acquire("sess-A")
	_, _ = s.Acquire("sess-B")

	freed := s.Release("sess-A")
	if len(freed) != 1 || freed[0] != "dev1" {
		t.Errorf("want freed=[dev1], got %v", freed)
	}

	again, err := s.Acquire("sess-C")
	if err != nil {
		t.Fatal(err)
	}
	if again.Lease.Slot != "dev1" {
		t.Errorf("dev1 should now be free for sess-C, got %s", again.Lease.Slot)
	}
}

func TestRelease_NoLeaseIsIdempotent(t *testing.T) {
	s := newTestStore(twoSlots())
	freed := s.Release("nobody")
	if len(freed) != 0 {
		t.Errorf("want empty, got %v", freed)
	}
}

func TestReleaseSlot_ClearsLeaseRegardlessOfHolder(t *testing.T) {
	s := newTestStore(twoSlots())
	_, _ = s.Acquire("sess-A")

	if !s.ReleaseSlot("dev1") {
		t.Error("expected ReleaseSlot to report cleared")
	}
	if s.ReleaseSlot("dev1") {
		t.Error("second call should no-op")
	}
	// Slot should now be free for a different session.
	res, err := s.Acquire("sess-B")
	if err != nil || res.Lease.Slot != "dev1" {
		t.Errorf("dev1 should be free: lease=%+v err=%v", res.Lease, err)
	}
}

func TestList_ReturnsCurrentLeases(t *testing.T) {
	s := newTestStore(twoSlots())
	_, _ = s.Acquire("sess-A")

	leases := s.List()
	if len(leases) != 1 {
		t.Fatalf("want 1 lease, got %d", len(leases))
	}
	if leases[0].SessionID != "sess-A" {
		t.Errorf("want sess-A, got %s", leases[0].SessionID)
	}
}

func TestSlots_ReportsHolderAndPortState(t *testing.T) {
	// dev2 externally bound (no holder, busy), dev1 held by sess-A (busy once bound
	// — but stub probe marks dev1 as free, so we flag it as a stale lease).
	s := newTestStore(twoSlots(), 9211)
	_, _ = s.Acquire("sess-A")

	infos := s.Slots()
	if len(infos) != 2 {
		t.Fatalf("want 2 infos, got %d", len(infos))
	}
	var dev1, dev2 SlotInfo
	for _, i := range infos {
		switch i.Slot {
		case "dev1":
			dev1 = i
		case "dev2":
			dev2 = i
		}
	}
	if dev1.HolderSessionID != "sess-A" {
		t.Errorf("dev1 holder: got %q", dev1.HolderSessionID)
	}
	if dev1.PortBusy {
		t.Errorf("dev1 port should be free in stub")
	}
	if dev2.HolderSessionID != "" {
		t.Errorf("dev2 holder should be empty, got %q", dev2.HolderSessionID)
	}
	if !dev2.PortBusy {
		t.Errorf("dev2 should be port-busy")
	}
	if dev2.ExternalOwner == nil || dev2.ExternalOwner.PID != 12345 {
		t.Errorf("dev2 external owner missing: %+v", dev2.ExternalOwner)
	}
}

func TestFindSlot(t *testing.T) {
	s := newTestStore(twoSlots())
	if _, ok := s.FindSlot("dev1"); !ok {
		t.Error("dev1 should be known")
	}
	if _, ok := s.FindSlot("nope"); ok {
		t.Error("unknown slot should return false")
	}
}

func TestEmptyStore(t *testing.T) {
	s := newTestStore(nil)
	_, err := s.Acquire("sess-A")
	if !errors.Is(err, ErrAllBusy) {
		t.Errorf("empty store should fail with ErrAllBusy, got %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("empty store List should be empty")
	}
	if len(s.Slots()) != 0 {
		t.Error("empty store Slots should be empty")
	}
}

func TestAcquire_RejectsEmptySessionID(t *testing.T) {
	s := newTestStore(twoSlots())
	_, err := s.Acquire("")
	if err == nil {
		t.Error("want error for empty session id")
	}
}
