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

func TestAcquire_AssignsFirstFreeSlot(t *testing.T) {
	s := NewStore(twoSlots())

	lease, err := s.Acquire("sess-A")
	if err != nil {
		t.Fatal(err)
	}
	if lease.Slot != "dev1" || lease.Port != 9210 || lease.PublicHost != "dev1.example.com" || lease.SessionID != "sess-A" {
		t.Errorf("first acquire wrong: %+v", lease)
	}

	lease2, err := s.Acquire("sess-B")
	if err != nil {
		t.Fatal(err)
	}
	if lease2.Slot != "dev2" {
		t.Errorf("second acquire should pick dev2, got %s", lease2.Slot)
	}
}

func TestAcquire_AllBusyReturnsError(t *testing.T) {
	s := NewStore(twoSlots())
	_, _ = s.Acquire("sess-A")
	_, _ = s.Acquire("sess-B")

	_, err := s.Acquire("sess-C")
	if !errors.Is(err, ErrAllBusy) {
		t.Errorf("want ErrAllBusy, got %v", err)
	}
}

func TestAcquire_SameSessionReturnsExistingLease(t *testing.T) {
	s := NewStore(twoSlots())
	first, _ := s.Acquire("sess-A")

	again, err := s.Acquire("sess-A")
	if err != nil {
		t.Fatal(err)
	}
	if again.Slot != first.Slot {
		t.Errorf("same session should get same slot: first=%s again=%s", first.Slot, again.Slot)
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

func TestRelease_FreesSlot(t *testing.T) {
	s := NewStore(twoSlots())
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
	if again.Slot != "dev1" {
		t.Errorf("dev1 should now be free for sess-C, got %s", again.Slot)
	}
}

func TestRelease_NoLeaseIsIdempotent(t *testing.T) {
	s := NewStore(twoSlots())
	freed := s.Release("nobody")
	if len(freed) != 0 {
		t.Errorf("want empty, got %v", freed)
	}
}

func TestList_ReturnsCurrentLeases(t *testing.T) {
	s := NewStore(twoSlots())
	_, _ = s.Acquire("sess-A")

	leases := s.List()
	if len(leases) != 1 {
		t.Fatalf("want 1 lease, got %d", len(leases))
	}
	if leases[0].SessionID != "sess-A" {
		t.Errorf("want sess-A, got %s", leases[0].SessionID)
	}
}

func TestSlots_ReportsHolderOrEmpty(t *testing.T) {
	s := NewStore(twoSlots())
	_, _ = s.Acquire("sess-A")

	infos := s.Slots()
	if len(infos) != 2 {
		t.Fatalf("want 2 infos, got %d", len(infos))
	}
	var dev1, dev2 SlotInfo
	for _, i := range infos {
		if i.Slot == "dev1" {
			dev1 = i
		}
		if i.Slot == "dev2" {
			dev2 = i
		}
	}
	if dev1.HolderSessionID != "sess-A" {
		t.Errorf("dev1 should be held by sess-A, got %q", dev1.HolderSessionID)
	}
	if dev2.HolderSessionID != "" {
		t.Errorf("dev2 should be free, got holder %q", dev2.HolderSessionID)
	}
}

func TestEmptyStore(t *testing.T) {
	s := NewStore(nil)
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
	s := NewStore(twoSlots())
	_, err := s.Acquire("")
	if err == nil {
		t.Error("want error for empty session id")
	}
}
