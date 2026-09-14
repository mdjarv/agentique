package server

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/store"
)

type pollCatalog struct {
	mu       sync.Mutex
	machines map[string]store.Machine
}

func (c *pollCatalog) ListMachines(context.Context) ([]store.Machine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]store.Machine, 0, len(c.machines))
	for _, m := range c.machines {
		out = append(out, m)
	}
	return out, nil
}

func (c *pollCatalog) GetMachine(_ context.Context, id string) (store.Machine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.machines[id], nil
}

func (c *pollCatalog) SetMachinePeerCursor(_ context.Context, arg store.SetMachinePeerCursorParams) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.machines[arg.MachineID]
	m.PeerCursor = arg.PeerCursor
	c.machines[arg.MachineID] = m
	return 1, nil
}

func (c *pollCatalog) cursor(id string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.machines[id].PeerCursor
}

type pollEvents struct {
	mu     sync.Mutex
	log    []peer.Event
	err    error
	asked  []int64
	polled chan struct{}
}

func (e *pollEvents) Events(_ context.Context, _ string, since int64, _ time.Duration) (peer.EventsResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.asked = append(e.asked, since)
	if e.polled != nil {
		select {
		case e.polled <- struct{}{}:
		default:
		}
	}
	if e.err != nil {
		return peer.EventsResponse{}, e.err
	}
	out := peer.EventsResponse{Events: []peer.Event{}}
	for _, ev := range e.log {
		if ev.Seq > out.Latest {
			out.Latest = ev.Seq
		}
		if ev.Seq > since {
			out.Events = append(out.Events, ev)
		}
	}
	return out, nil
}

type pollSink struct {
	mu      sync.Mutex
	reports []string
	notices []string
}

func (s *pollSink) PeerReport(_ context.Context, machine, sessionID string, r assistant.Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, machine+"/"+sessionID+"/"+r.Headline)
}

func (s *pollSink) PeerFinding(_ context.Context, f assistant.Finding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notices = append(s.notices, "finding/"+f.Machine+"/"+f.Kind)
}

func (s *pollSink) PeerTurnEnd(_ context.Context, machine, sessionID, name string, n assistant.Notice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notices = append(s.notices, machine+"/"+sessionID+"/"+name+"/"+string(n.Kind))
}

func event(seq int64, kind, session string, payload map[string]any) peer.Event {
	raw, _ := json.Marshal(payload)
	return peer.Event{Seq: seq, Kind: kind, SessionID: session, Payload: raw}
}

func newTestPoller(machines ...store.Machine) (*peerPoller, *pollCatalog, *pollEvents, *pollSink) {
	catalog := &pollCatalog{machines: map[string]store.Machine{}}
	for _, m := range machines {
		catalog.machines[m.MachineID] = m
	}
	events := &pollEvents{}
	sink := &pollSink{}
	p := newPeerPoller(catalog, events, sink, "self")
	return p, catalog, events, sink
}

// A machine read for the first time starts from now: nothing already in its log
// is applied, and the cursor lands on its head.
func TestPollerFirstContactStartsFromNow(t *testing.T) {
	zb := store.Machine{MachineID: "zbook-id", Label: "zbook", PeerCursor: -1}
	p, catalog, events, sink := newTestPoller(zb)
	events.log = []peer.Event{event(7, peer.EventReport, "s1", map[string]any{"kind": "surprise", "headline": "old news"})}

	if err := p.pollOnce(context.Background(), "zbook-id"); err != nil {
		t.Fatal(err)
	}
	if catalog.cursor("zbook-id") != 7 || len(sink.reports) != 0 {
		t.Fatalf("cursor=%d reports=%v", catalog.cursor("zbook-id"), sink.reports)
	}
	if events.asked[0] != math.MaxInt64 {
		t.Fatalf("first read asked since=%d, want past every seq", events.asked[0])
	}
}

func TestPollerAppliesInOrderAndAdvances(t *testing.T) {
	zb := store.Machine{MachineID: "zbook-id", Label: "zbook", PeerCursor: 2}
	p, catalog, events, sink := newTestPoller(zb)
	var ended []string
	p.onTurnEnd = func(machineID, sessionID string) { ended = append(ended, machineID+"/"+sessionID) }
	events.log = []peer.Event{
		event(2, peer.EventReport, "s1", map[string]any{"kind": "surprise", "headline": "already applied"}),
		event(3, peer.EventReport, "s1", map[string]any{"kind": "surprise", "headline": "tests were already failing"}),
		event(4, "a-kind-from-the-future", "", map[string]any{"kind": "x"}),
		event(5, peer.EventTurnEnd, "s1", map[string]any{"kind": "finished", "headline": "done", "name": "Plugin Testing"}),
		event(6, peer.EventTurnEnd, "s1", map[string]any{"kind": "exploded"}),
		event(7, peer.EventReport, "s1", map[string]any{"kind": "not-a-kind", "headline": "x"}),
	}

	if err := p.pollOnce(context.Background(), "zbook-id"); err != nil {
		t.Fatal(err)
	}
	if len(sink.reports) != 1 || sink.reports[0] != "zbook/s1/tests were already failing" {
		t.Fatalf("reports = %v", sink.reports)
	}
	if len(sink.notices) != 1 || sink.notices[0] != "zbook/s1/Plugin Testing/finished" {
		t.Fatalf("notices = %v", sink.notices)
	}
	if len(ended) != 1 || catalog.cursor("zbook-id") != 7 {
		t.Fatalf("ended=%v cursor=%d, want every seq consumed including the skipped ones", ended, catalog.cursor("zbook-id"))
	}
}

func TestPollerFollowsARestartedLog(t *testing.T) {
	zb := store.Machine{MachineID: "zbook-id", PeerCursor: 90}
	p, catalog, events, _ := newTestPoller(zb)
	events.log = []peer.Event{event(3, peer.EventReport, "s", map[string]any{"kind": "surprise", "headline": "x"})}
	if err := p.pollOnce(context.Background(), "zbook-id"); err != nil {
		t.Fatal(err)
	}
	if catalog.cursor("zbook-id") != 3 {
		t.Fatalf("cursor = %d, want the restarted log's head", catalog.cursor("zbook-id"))
	}
}

// An older release waits the long interval, a transient failure backs off, and
// a machine that leaves the catalog stops being polled.
func TestPollerRunLifecycle(t *testing.T) {
	zb := store.Machine{MachineID: "zbook-id", PeerCursor: 0}
	p, catalog, events, _ := newTestPoller(zb)
	events.err = peerlink.ErrNoPeerSurface
	events.polled = make(chan struct{}, 1)

	var mu sync.Mutex
	var slept []time.Duration
	release := make(chan struct{})
	p.sleep = func(ctx context.Context, d time.Duration) bool {
		mu.Lock()
		slept = append(slept, d)
		mu.Unlock()
		select {
		case <-release:
			return true
		case <-ctx.Done():
			return false
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	select {
	case <-events.polled:
	case <-time.After(2 * time.Second):
		t.Fatal("machine never polled")
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, d := range slept {
			if d == p.noSurface {
				return true
			}
		}
		return false
	})

	catalog.mu.Lock()
	delete(catalog.machines, "zbook-id")
	catalog.mu.Unlock()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestPollerBacksOffTransientFailures(t *testing.T) {
	p, _, events, _ := newTestPoller(store.Machine{MachineID: "zbook-id", PeerCursor: 0})
	events.err = errors.New("dial tcp: i/o timeout")
	var slept []time.Duration
	calls := 0
	p.sleep = func(_ context.Context, d time.Duration) bool {
		slept = append(slept, d)
		calls++
		return calls < 4
	}
	p.follow(context.Background(), "zbook-id")
	if len(slept) != 4 || slept[0] != p.backoff || slept[1] != 2*p.backoff || slept[2] != 4*p.backoff {
		t.Fatalf("slept = %v", slept)
	}
}

func TestPollerRelaysFindings(t *testing.T) {
	zb := store.Machine{MachineID: "zbook-id", Label: "zbook", PeerCursor: 0}
	p, _, events, sink := newTestPoller(zb)
	events.log = []peer.Event{
		event(1, peer.EventFinding, "", map[string]any{"kind": "disk-low", "severity": "warning", "remedy": "hand",
			"facts": map[string]any{"freeBytes": 1024, "path": "/data", "nested": map[string]any{"x": 1}}, "opened": true}),
		event(2, peer.EventFinding, "", map[string]any{"severity": "warning"}), // no kind: skipped
	}
	if err := p.pollOnce(context.Background(), "zbook-id"); err != nil {
		t.Fatal(err)
	}
	if len(sink.notices) != 1 || sink.notices[0] != "finding/zbook/disk-low" {
		t.Fatalf("notices = %v", sink.notices)
	}
}
