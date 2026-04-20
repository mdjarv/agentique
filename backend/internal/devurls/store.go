// Package devurls manages a bounded pool of publicly-routable dev frontend
// URLs that sessions can lease while iterating on the UI. Slot config is
// loaded from agentique config; lease state is in-memory and dies with the
// server (sessions die with it too, so leases never outlive their holder).
package devurls

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/mdjarv/agentique/backend/internal/config"
)

// ErrAllBusy is returned by Acquire when every configured slot is leased.
var ErrAllBusy = errors.New("all dev URL slots are currently in use")

// Lease is a single acquired slot held by a session.
type Lease struct {
	Slot       string
	SessionID  string
	PublicHost string
	Port       int
	URL        string
	AcquiredAt time.Time
}

// SlotConflict describes a slot that Acquire bypassed because its TCP port was
// already bound by a process not tracked by this Store (an orphan from a prior
// server run, or an unrelated service that happened to grab the port).
type SlotConflict struct {
	Slot  string
	Port  int
	Owner *PortOwner // may be nil if /proc lookup failed
}

// AcquireResult is returned by Acquire. Lease is non-nil on success. Skipped
// lists slots that were bypassed because their ports were externally bound,
// so callers can surface cleanup suggestions even on the happy path.
type AcquireResult struct {
	Lease   *Lease
	Skipped []SlotConflict
}

// SlotInfo describes a configured slot and its current holder, if any.
type SlotInfo struct {
	Slot            string
	Port            int
	PublicHost      string
	URL             string
	HolderSessionID string
	AcquiredAt      time.Time
	// PortBusy is true when the TCP port is actually bound right now.
	// Combined with HolderSessionID this distinguishes healthy leases
	// (held + busy), free slots (no holder + free), and externally-held
	// ports that need cleanup (no holder + busy).
	PortBusy bool
	// ExternalOwner is populated when PortBusy is true and no lease
	// tracks the port, i.e. the owner is outside this Store's control.
	ExternalOwner *PortOwner
}

// Store tracks lease state for a fixed set of slots.
type Store struct {
	mu    sync.Mutex
	slots []config.DevURLSlot
	held  map[string]*Lease // slot → lease
	now   func() time.Time

	// probe is injectable for tests. Returns nil if port is free, error if busy.
	probe func(port int) error
	// findOwner is injectable for tests. May return nil,nil when port is free.
	findOwner func(port int) (*PortOwner, error)
}

// NewStore creates a Store with the given slot config. A nil/empty slot list
// is allowed — every Acquire will then fail with ErrAllBusy.
func NewStore(slots []config.DevURLSlot) *Store {
	return NewStoreWithProbes(slots, ProbePort, FindPortOwner)
}

// NewStoreWithProbes is like NewStore but lets callers inject the port-probe
// and owner-lookup functions. Intended for tests — production code should use
// NewStore.
func NewStoreWithProbes(slots []config.DevURLSlot, probe func(port int) error, findOwner func(port int) (*PortOwner, error)) *Store {
	return &Store{
		slots:     slots,
		held:      make(map[string]*Lease),
		now:       time.Now,
		probe:     probe,
		findOwner: findOwner,
	}
}

// Acquire leases the first free slot to the given session. A slot is eligible
// when no lease holds it AND its TCP port is not currently bound. Slots that
// are free in the lease map but whose ports are externally bound are added to
// Skipped so callers can report the conflict. If the session already holds a
// slot, the existing lease is returned (idempotent, no probing).
func (s *Store) Acquire(sessionID string) (*AcquireResult, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("acquire: sessionID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Idempotent: same session, return current lease.
	for _, lease := range s.held {
		if lease.SessionID == sessionID {
			return &AcquireResult{Lease: lease}, nil
		}
	}

	var skipped []SlotConflict
	for _, slot := range s.slots {
		if _, taken := s.held[slot.Slot]; taken {
			continue
		}
		if err := s.probe(slot.Port); err != nil {
			owner, _ := s.findOwner(slot.Port)
			skipped = append(skipped, SlotConflict{
				Slot:  slot.Slot,
				Port:  slot.Port,
				Owner: owner,
			})
			continue
		}
		lease := &Lease{
			Slot:       slot.Slot,
			SessionID:  sessionID,
			PublicHost: slot.PublicHost,
			Port:       slot.Port,
			URL:        publicURL(slot.PublicHost),
			AcquiredAt: s.now(),
		}
		s.held[slot.Slot] = lease
		return &AcquireResult{Lease: lease, Skipped: skipped}, nil
	}
	return &AcquireResult{Skipped: skipped}, ErrAllBusy
}

// Release frees any slots held by the given session. Returns the slot names
// that were freed (empty if the session held nothing).
func (s *Store) Release(sessionID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var freed []string
	for slot, lease := range s.held {
		if lease.SessionID == sessionID {
			freed = append(freed, slot)
			delete(s.held, slot)
		}
	}
	sort.Strings(freed)
	return freed
}

// ReleaseSlot clears any lease on the given slot, regardless of which session
// holds it. Intended for admin/UI use after killing an externally-held port.
// Returns true if a lease was cleared.
func (s *Store) ReleaseSlot(slot string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.held[slot]; !ok {
		return false
	}
	delete(s.held, slot)
	return true
}

// List returns a snapshot of all currently-held leases.
func (s *Store) List() []*Lease {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Lease, 0, len(s.held))
	for _, lease := range s.held {
		copy := *lease
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// Slots returns the configured slots, their current holder (if any), and
// whether the underlying TCP port is actually bound. Slot order matches the
// config. Port probing happens outside the store lock so slow /proc reads
// don't block Acquire/Release.
func (s *Store) Slots() []SlotInfo {
	s.mu.Lock()
	slots := make([]config.DevURLSlot, len(s.slots))
	copy(slots, s.slots)
	held := make(map[string]*Lease, len(s.held))
	for k, v := range s.held {
		held[k] = v
	}
	s.mu.Unlock()

	out := make([]SlotInfo, 0, len(slots))
	for _, slot := range slots {
		info := SlotInfo{
			Slot:       slot.Slot,
			Port:       slot.Port,
			PublicHost: slot.PublicHost,
			URL:        publicURL(slot.PublicHost),
		}
		if lease, ok := held[slot.Slot]; ok {
			info.HolderSessionID = lease.SessionID
			info.AcquiredAt = lease.AcquiredAt
		}
		if err := s.probe(slot.Port); err != nil {
			info.PortBusy = true
			if info.HolderSessionID == "" {
				owner, _ := s.findOwner(slot.Port)
				info.ExternalOwner = owner
			}
		}
		out = append(out, info)
	}
	return out
}

// FindSlot looks up a configured slot by name. Returns zero value + false if unknown.
func (s *Store) FindSlot(name string) (config.DevURLSlot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, slot := range s.slots {
		if slot.Slot == name {
			return slot, true
		}
	}
	return config.DevURLSlot{}, false
}

func publicURL(host string) string {
	return "https://" + host
}
