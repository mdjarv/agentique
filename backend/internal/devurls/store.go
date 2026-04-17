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

// SlotInfo describes a configured slot and its current holder, if any.
type SlotInfo struct {
	Slot            string
	Port            int
	PublicHost      string
	URL             string
	HolderSessionID string
	AcquiredAt      time.Time
}

// Store tracks lease state for a fixed set of slots.
type Store struct {
	mu    sync.Mutex
	slots []config.DevURLSlot
	held  map[string]*Lease // slot → lease
	now   func() time.Time
}

// NewStore creates a Store with the given slot config. A nil/empty slot list
// is allowed — every Acquire will then fail with ErrAllBusy.
func NewStore(slots []config.DevURLSlot) *Store {
	return &Store{
		slots: slots,
		held:  make(map[string]*Lease),
		now:   time.Now,
	}
}

// Acquire leases the first free slot to the given session. If the session
// already holds a slot, the existing lease is returned (idempotent).
func (s *Store) Acquire(sessionID string) (*Lease, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("acquire: sessionID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Idempotent: same session, return current lease.
	for _, lease := range s.held {
		if lease.SessionID == sessionID {
			return lease, nil
		}
	}

	for _, slot := range s.slots {
		if _, taken := s.held[slot.Slot]; taken {
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
		return lease, nil
	}
	return nil, ErrAllBusy
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

// Slots returns the configured slots and their current holder (if any).
// Slot order matches the config.
func (s *Store) Slots() []SlotInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]SlotInfo, 0, len(s.slots))
	for _, slot := range s.slots {
		info := SlotInfo{
			Slot:       slot.Slot,
			Port:       slot.Port,
			PublicHost: slot.PublicHost,
			URL:        publicURL(slot.PublicHost),
		}
		if lease, ok := s.held[slot.Slot]; ok {
			info.HolderSessionID = lease.SessionID
			info.AcquiredAt = lease.AcquiredAt
		}
		out = append(out, info)
	}
	return out
}

func publicURL(host string) string {
	return "https://" + host
}
