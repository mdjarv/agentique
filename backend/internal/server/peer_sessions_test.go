package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// fakeClock is a settable now.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestPeers(machines []store.Machine, fetch peerFetchFunc) (*peerSessions, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
	return &peerSessions{
		machines:   func(context.Context) ([]store.Machine, error) { return machines, nil },
		localKey:   func(context.Context) map[string]store.Project { return nil },
		fetch:      fetch,
		selfID:     "self",
		now:        clock.now,
		coldBudget: time.Second,
		entries:    make(map[string]*peerEntry),
	}, clock
}

var zbook = store.Machine{MachineID: "zbook-id", Label: "zbook", BaseUrl: "https://zbook", Token: "t", IdentityKey: "k"}

// The session that started this: running on zbook, in a repository this
// machine also has checked out, and invisible to the assistant.
func TestPeerRowsDescribeARemoteSession(t *testing.T) {
	unseen := "2026-09-13T06:00:00Z"
	snap := peerSnapshot{
		Sessions: []peer.SessionWire{
			{ID: "s1", ProjectID: "p-remote", Name: "Plugin Testing", State: "idle", Model: "claude-opus-5",
				WorktreeBranch: "session-s1", UpdatedAt: "2026-09-13T07:00:00Z"},
			{ID: "s2", ProjectID: "p-remote", Name: "Filed away", State: "idle", ArchivedAt: "2026-09-12T00:00:00Z"},
			{ID: "s3", ProjectID: "p-other", Name: "Waiting", State: "idle", PendingApproval: true},
			{ID: "s4", ProjectID: "p-other", Name: "Done", State: "done", UnseenCompletedAt: unseen},
			{ID: "s5", ProjectID: "p-other", Name: "Not waiting", State: "idle"},
		},
		Reach: assistant.ReachPeer,
		Projects: []peer.ProjectWire{
			{ID: "p-remote", Name: "seisiun on zbook", Slug: "seisiun", RemoteURL: "github.com/mdjarv/seisiun"},
			{ID: "p-other", Name: "Scratch", Slug: "scratch"},
		},
	}
	local := map[string]store.Project{
		"github.com/mdjarv/seisiun": {ID: "p-local", Name: "seisiun", Slug: "seisiun"},
	}

	rows := peerRows(zbook, snap, local)
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4 (the archived one dropped): %+v", len(rows), rows)
	}
	byID := make(map[string]assistant.SessionRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	got := byID["s1"]
	if got.Name != "Plugin Testing" || got.MachineID != "zbook-id" || got.MachineName != "zbook" {
		t.Errorf("s1 identity = %+v", got)
	}
	if got.ProjectName != "seisiun" {
		t.Errorf("s1 project = %q, want this host's name for the shared repository", got.ProjectName)
	}
	if got.ProjectID != "" {
		t.Errorf("s1 ProjectID = %q, want empty: a remote project id means nothing here", got.ProjectID)
	}
	if got.Reach != assistant.ReachPeer {
		t.Errorf("s1 reach = %q, want the snapshot's", got.Reach)
	}
	if got.Model != "Opus" || got.Branch != "session-s1" || got.LastActivity != "2026-09-13T07:00:00Z" {
		t.Errorf("s1 details = %+v", got)
	}
	if byID["s3"].Attention != assistant.AttentionApproval {
		t.Errorf("s3 attention = %q, want approval", byID["s3"].Attention)
	}
	if byID["s3"].ProjectName != "Scratch" {
		t.Errorf("s3 project = %q, want the remote's own name where this host has none", byID["s3"].ProjectName)
	}
	if byID["s4"].Attention != assistant.AttentionUnread {
		t.Errorf("s4 attention = %q, want unread", byID["s4"].Attention)
	}
	if byID["s5"].Attention != "" {
		t.Errorf("s5 attention = %q, want none", byID["s5"].Attention)
	}
}

func TestPeerViewCachesAFreshAnswer(t *testing.T) {
	var calls atomic.Int32
	peers, clock := newTestPeers([]store.Machine{zbook, {MachineID: "self", Label: "me"}},
		func(context.Context, store.Machine) (peerSnapshot, error) {
			calls.Add(1)
			return peerSnapshot{Sessions: []peer.SessionWire{{ID: "s1", Name: "Plugin Testing"}}}, nil
		})

	view := peers.View(context.Background())
	if len(view.Rows) != 1 || len(view.Unreachable) != 0 {
		t.Fatalf("cold view = %+v, want the one zbook row", view)
	}
	if calls.Load() != 1 {
		t.Fatalf("fetches = %d, want 1 — this machine's own catalog row must not be dialled", calls.Load())
	}

	clock.advance(peerFreshFor / 2)
	peers.View(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("fetches = %d after a fresh read, want 1", calls.Load())
	}
}

// Past fresh but inside stale the held answer is served at once and a refresh
// runs behind it: a tool call must not wait on another machine it already
// has an answer from.
func TestPeerViewServesStaleWhileRefreshing(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	peers, clock := newTestPeers([]store.Machine{zbook},
		func(context.Context, store.Machine) (peerSnapshot, error) {
			if calls.Add(1) > 1 {
				<-release
			}
			return peerSnapshot{Sessions: []peer.SessionWire{{ID: "s1"}}}, nil
		})

	peers.View(context.Background())
	clock.advance(peerFreshFor + time.Second)

	start := time.Now()
	view := peers.View(context.Background())
	if waited := time.Since(start); waited > 500*time.Millisecond {
		t.Fatalf("stale read waited %v for the refresh", waited)
	}
	if len(view.Rows) != 1 {
		t.Fatalf("stale view = %+v, want the held row", view)
	}
	close(release)
	waitFor(t, func() bool { return calls.Load() == 2 })
}

// A machine that is off is named, never silently absent, and costs the cold
// budget rather than the fetch's.
func TestPeerViewNamesAMachineThatDoesNotAnswer(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	desktop := store.Machine{MachineID: "desk-id", Label: "Blackpearl"}
	peers, _ := newTestPeers([]store.Machine{zbook, desktop},
		func(ctx context.Context, m store.Machine) (peerSnapshot, error) {
			if m.MachineID == desktop.MachineID {
				return peerSnapshot{}, errors.New("dial tcp: connection refused")
			}
			<-blocked
			return peerSnapshot{}, nil
		})
	peers.coldBudget = 50 * time.Millisecond

	start := time.Now()
	view := peers.View(context.Background())
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("view waited %v on a machine that never answers", waited)
	}
	if len(view.Rows) != 0 {
		t.Fatalf("rows = %+v, want none", view.Rows)
	}
	if len(view.Unreachable) != 2 {
		t.Fatalf("unreachable = %v, want both machines named", view.Unreachable)
	}
}

// A failure after a success forgets what the machine said: its sessions are
// unknown now, and an old "running" would be said as though it were current.
func TestPeerViewDropsRowsWhenARefreshFails(t *testing.T) {
	var fail atomic.Bool
	peers, clock := newTestPeers([]store.Machine{zbook},
		func(context.Context, store.Machine) (peerSnapshot, error) {
			if fail.Load() {
				return peerSnapshot{}, errors.New("timeout")
			}
			return peerSnapshot{Sessions: []peer.SessionWire{{ID: "s1", State: "running"}}}, nil
		})

	peers.View(context.Background())
	fail.Store(true)
	clock.advance(peerFreshFor + time.Second)
	peers.View(context.Background()) // serves stale, refresh fails behind it
	waitFor(t, func() bool {
		peers.mu.Lock()
		defer peers.mu.Unlock()
		return peers.entries[zbook.MachineID].err != nil
	})

	view := peers.View(context.Background())
	if len(view.Rows) != 0 || len(view.Unreachable) != 1 {
		t.Fatalf("view after failure = %+v, want no rows and zbook named", view)
	}
}

func TestMergeRowsKeepsTheLocalCopy(t *testing.T) {
	local := []assistant.SessionRow{{ID: "a", Name: "local truth", MachineID: "self", LastActivity: "2026-09-13T01:00:00Z"}}
	peer := []assistant.SessionRow{
		{ID: "a", Name: "peer echo", MachineID: "zbook-id", LastActivity: "2026-09-13T09:00:00Z"},
		{ID: "b", Name: "Plugin Testing", MachineID: "zbook-id", LastActivity: "2026-09-13T08:00:00Z"},
	}
	rows := mergeRows(local, peer)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want 2", rows)
	}
	if rows[0].ID != "b" || rows[1].Name != "local truth" {
		t.Fatalf("rows = %+v, want newest first and the local copy of a", rows)
	}
}

// An older release is read the transitional way, with the pairing bearer, and
// every row from it is described and never actionable. The wire names are that
// release's JSON tags, so the remote is built from the real types.
func TestTransitionalFetchReadsAnOlderRelease(t *testing.T) {
	machineID := uuid.New().String()
	identity, err := machine.LoadOrCreateSigningIdentity(t.TempDir(), machineID)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	unseen := "2026-09-13T06:00:00Z"
	sessions := []session.SessionInfo{{
		ID: "s1", ProjectID: "p1", Name: "Plugin Testing", State: "idle", Model: "claude-opus-5",
		WorktreeBranch: "session-s1", UnseenCompletedAt: &unseen,
		PendingQuestion: &session.WirePendingQuestion{QuestionID: "q"},
	}}
	projects := []store.Project{{ID: "p1", Name: "seisiun", Slug: "seisiun", RemoteUrl: "github.com/mdjarv/seisiun"}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agentique/environment", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"machineId": machineID, "identityKey": identity.PublicKey()})
	})
	mux.HandleFunc("POST /api/auth/identity-proof", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Nonce string `json:"nonce"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		proof, _ := identity.SignChallenge(body.Nonce)
		_ = json.NewEncoder(w).Encode(map[string]string{"machineId": machineID, "identityKey": identity.PublicKey(), "proof": proof})
	})
	authed := func(payload any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer live" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(payload)
		}
	}
	mux.HandleFunc("GET /api/sessions", authed(sessions))
	mux.HandleFunc("GET /api/projects", authed(projects))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	m := store.Machine{MachineID: machineID, Label: "zbook", BaseUrl: srv.URL, Token: "live", IdentityKey: identity.PublicKey()}
	// A link that says the machine has no peer surface is what sends the
	// reader down the transitional path.
	snap, err := peerFetch(noSurface{}, srv.Client())(context.Background(), m)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if snap.Reach != assistant.ReachPeerOld {
		t.Fatalf("reach = %q, want peer-old", snap.Reach)
	}
	rows := peerRows(m, snap, nil)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	row := rows[0]
	if row.Name != "Plugin Testing" || row.ProjectName != "seisiun" || row.Branch != "session-s1" ||
		row.Attention != assistant.AttentionQuestion || row.Model != "Opus" || row.Reach.CanAct() {
		t.Fatalf("row = %+v", row)
	}
}

type noSurface struct{}

func (noSurface) List(context.Context, string) (peer.SessionsResponse, error) {
	return peer.SessionsResponse{}, peerlink.ErrNoPeerSurface
}

// A machine with a peer surface is read through it, and the opt-ins decide the
// reach every row carries.
func TestPeerFetchUsesThePeerSurface(t *testing.T) {
	for _, tt := range []struct {
		actions, policies bool
		want              assistant.Reach
	}{
		{false, true, assistant.ReachPeerOff},
		{true, false, assistant.ReachPeer},
		{true, true, assistant.ReachPeer},
	} {
		link := listed{peer.SessionsResponse{AcceptActions: tt.actions, AcceptPolicies: tt.policies,
			Sessions: []peer.SessionWire{{ID: "s1", State: "idle"}}}}
		snap, err := peerFetch(link, nil)(context.Background(), zbook)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Reach != tt.want || snap.AcceptsPolicies != (tt.actions && tt.policies) || len(snap.Sessions) != 1 {
			t.Errorf("actions=%v policies=%v: snapshot = %+v", tt.actions, tt.policies, snap)
		}
	}
}

type listed struct{ resp peer.SessionsResponse }

func (l listed) List(context.Context, string) (peer.SessionsResponse, error) { return l.resp, nil }

// A session created moments ago is locatable before the machine's next list
// includes it, and stops standing in once two refreshes have passed.
func TestLocateRemembersACreation(t *testing.T) {
	peers, clock := newTestPeers([]store.Machine{zbook},
		func(context.Context, store.Machine) (peerSnapshot, error) {
			return peerSnapshot{Reach: assistant.ReachPeer}, nil
		})
	peers.View(context.Background())
	peers.Remember(peerLocation{Machine: zbook, Session: peer.SessionWire{ID: "new"},
		Row: assistant.SessionRow{ID: "new", Reach: assistant.ReachPeer}})
	if loc, ok := peers.Locate(context.Background(), "new"); !ok || loc.Row.Reach != assistant.ReachPeer {
		t.Fatalf("locate = %+v %v", loc, ok)
	}
	clock.advance(recentFor + time.Second)
	if _, ok := peers.Locate(context.Background(), "new"); ok {
		t.Fatal("a remembered creation outlived its window")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never held")
}

// An asleep machine costs one bounded wait. The read after it — while the
// fetch is still out, or once it has failed — answers at once.
func TestPeerViewWaitsOnAnAsleepMachineOnce(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	peers, clock := newTestPeers([]store.Machine{zbook},
		func(context.Context, store.Machine) (peerSnapshot, error) {
			calls.Add(1)
			<-release
			return peerSnapshot{}, errors.New("i/o timeout")
		})
	peers.coldBudget = 200 * time.Millisecond

	peers.View(context.Background())

	start := time.Now()
	if view := peers.View(context.Background()); len(view.Unreachable) != 1 {
		t.Fatalf("view = %+v, want zbook named", view)
	}
	if waited := time.Since(start); waited > 100*time.Millisecond {
		t.Fatalf("second read waited %v on a fetch already out", waited)
	}

	close(release)
	waitFor(t, func() bool {
		peers.mu.Lock()
		defer peers.mu.Unlock()
		return peers.entries[zbook.MachineID].err != nil
	})
	clock.advance(peerStaleFor + time.Minute)

	start = time.Now()
	peers.View(context.Background())
	if waited := time.Since(start); waited > 100*time.Millisecond {
		t.Fatalf("read after a failure waited %v", waited)
	}
	waitFor(t, func() bool { return calls.Load() == 2 })
}
