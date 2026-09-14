package steward

import (
	"context"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func allObserved() map[Kind]bool {
	out := map[Kind]bool{}
	for _, k := range Kinds {
		out[k] = true
	}
	return out
}

func kindsOf(fs []Finding) map[Kind]int {
	out := map[Kind]int{}
	for _, f := range fs {
		out[f.Kind]++
	}
	return out
}

func TestEvaluateEachKind(t *testing.T) {
	obs := Observation{
		Observed: allObserved(),
		Agents: []AgentAuth{
			{ID: "claude", Name: "Claude", SignedOut: true, Help: "Run `claude auth login` to restore usage."},
			{ID: "codex", Name: "Codex"},
		},
		Disk:    Disk{FreeBytes: 1 << 30, ReclaimableBytes: 5 << 30},
		Loops:   []PausedLoop{{ID: "l1", Name: "nightly", Failures: 3}},
		Blocked: []BlockedSession{{ID: "s1", Waiting: "an approval", Since: t0.Add(-time.Hour)}, {ID: "s2", Since: t0.Add(-time.Minute)}},
		Update:  Update{Behind: true, Current: "v0.6.0", Latest: "v0.7.1"},
		Backup:  Backup{Enabled: true, Interval: 15 * time.Minute, Newest: t0.Add(-2 * time.Hour)},
	}
	got := kindsOf(Evaluate(obs, t0))
	want := map[Kind]int{KindCLISignedOut: 1, KindDiskLow: 1, KindLoopPaused: 1, KindSessionBlockedLong: 1,
		KindUpdateWaiting: 1, KindBackupFailing: 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: got %d findings, want %d (all: %v)", k, got[k], n, got)
		}
	}
	for _, f := range Evaluate(obs, t0) {
		if f.Kind == KindDiskLow && f.Remedy != RemedyReclaim {
			t.Errorf("disk with reclaimable space: remedy = %q, want reclaim", f.Remedy)
		}
	}
}

// Nothing a sensor did not read produces a finding, and ordinary levels do not
// either: a high but ample disk, a fresh backup, a short wait.
func TestEvaluateIsQuietWhenHealthyOrUnobserved(t *testing.T) {
	healthy := Observation{
		Observed: allObserved(),
		Agents:   []AgentAuth{{ID: "claude"}},
		Disk:     Disk{FreeBytes: 40 << 30},
		Blocked:  []BlockedSession{{ID: "s1", Since: t0.Add(-5 * time.Minute)}},
		Backup:   Backup{Enabled: true, Interval: 15 * time.Minute, Newest: t0.Add(-20 * time.Minute)},
	}
	if got := Evaluate(healthy, t0); len(got) != 0 {
		t.Fatalf("healthy = %+v", got)
	}
	blind := Observation{Disk: Disk{FreeBytes: 0}, Agents: []AgentAuth{{ID: "claude", SignedOut: true}}}
	if got := Evaluate(blind, t0); len(got) != 0 {
		t.Fatalf("unobserved sensors produced findings: %+v", got)
	}
}

type memStore struct {
	mu   sync.Mutex
	open map[string]Finding
}

func (m *memStore) OpenFindings(context.Context) ([]Finding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Finding, 0, len(m.open))
	for _, f := range m.open {
		out = append(out, f)
	}
	return out, nil
}

func (m *memStore) Open(_ context.Context, f Finding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.open[f.Key()] = f
	return nil
}

func (m *memStore) Refresh(_ context.Context, f Finding) error { return m.Open(context.Background(), f) }

func (m *memStore) Resolve(_ context.Context, f Finding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.open, f.Key())
	return nil
}

// A pass opens what starts holding once, keeps it open while it holds, resolves
// it when it stops — and never resolves a kind whose sensor did not read.
func TestPassReconciles(t *testing.T) {
	st := &memStore{open: map[string]Finding{}}
	var changes []Change
	obs := Observation{Observed: allObserved(), Disk: Disk{FreeBytes: 1 << 20}}
	s := New(func(context.Context) Observation { return obs }, st, func(_ context.Context, c Change) { changes = append(changes, c) })
	now := t0
	s.now = func() time.Time { return now }
	ctx := context.Background()

	_ = s.Pass(ctx)
	_ = s.Pass(ctx)
	if len(changes) != 1 || !changes[0].Opened || changes[0].Finding.Kind != KindDiskLow {
		t.Fatalf("changes after two passes = %+v, want one open", changes)
	}

	// The disk sensor fails: the finding stays open rather than resolving.
	obs = Observation{Observed: map[Kind]bool{}}
	_ = s.Pass(ctx)
	if len(changes) != 1 || len(st.open) != 1 {
		t.Fatalf("an unread sensor resolved its finding: %+v", changes)
	}

	obs = Observation{Observed: allObserved(), Disk: Disk{FreeBytes: 50 << 30}}
	_ = s.Pass(ctx)
	if len(changes) != 2 || changes[1].Opened || changes[1].Finding.ResolvedAt == "" {
		t.Fatalf("changes = %+v, want the resolution", changes)
	}
}

// A blocked session's clock starts when the steward first sees it, and a
// session that stops waiting forgets its start.
func TestBlockedClockIsRemembered(t *testing.T) {
	st := &memStore{open: map[string]Finding{}}
	blocked := []BlockedSession{{ID: "s1", Name: "Plugin Testing", Waiting: "an approval"}}
	s := New(func(context.Context) Observation {
		return Observation{Observed: allObserved(), Disk: Disk{FreeBytes: 50 << 30},
			Blocked: append([]BlockedSession(nil), blocked...)}
	}, st, nil)
	now := t0
	s.now = func() time.Time { return now }
	ctx := context.Background()

	_ = s.Pass(ctx)
	now = now.Add(BlockedAfter - time.Minute)
	_ = s.Pass(ctx)
	if len(st.open) != 0 {
		t.Fatal("opened before the wait was long")
	}
	now = now.Add(2 * time.Minute)
	_ = s.Pass(ctx)
	if len(st.open) != 1 {
		t.Fatalf("open = %+v, want the long wait", st.open)
	}

	blocked = nil
	_ = s.Pass(ctx)
	blocked = []BlockedSession{{ID: "s1"}}
	now = now.Add(time.Minute)
	_ = s.Pass(ctx)
	if len(st.open) != 0 {
		t.Fatal("a new wait inherited the old clock")
	}
}
