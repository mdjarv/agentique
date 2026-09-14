package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/allbin/agentkit/eventbus"
	"github.com/mdjarv/agentique/backend/internal/assistant"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/peerlink"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

const (
	zbookSession   = "30000000-0000-4000-8000-0000000000a1"
	zbookProjectID = "30000000-0000-4000-8000-0000000000c1"
	createdRemote  = "30000000-0000-4000-8000-0000000000f1"
)

// fakeLink is a paired machine's peer surface as the acting side sees it.
type fakeLink struct {
	mu        sync.Mutex
	sent      []peer.SendRequest
	sentTo    []string
	created   []peer.CreateRequest
	followed  []string
	sendErr   error
	createErr error
}

func (f *fakeLink) Create(_ context.Context, _ string, req peer.CreateRequest) (peer.CreateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return peer.CreateResponse{}, f.createErr
	}
	f.created = append(f.created, req)
	return peer.CreateResponse{Session: peer.SessionWire{ID: createdRemote, State: "idle", WorktreeBranch: "session-f1",
		AutoApproveMode: "fullAuto", Model: "claude-opus-5"}}, nil
}

func (f *fakeLink) Send(_ context.Context, _, sessionID string, req peer.SendRequest) (peer.SendResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return peer.SendResponse{}, f.sendErr
	}
	f.sent = append(f.sent, req)
	f.sentTo = append(f.sentTo, sessionID)
	return peer.SendResponse{Delivery: "mid_turn"}, nil
}

func (f *fakeLink) Follow(_ context.Context, _, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.followed = append(f.followed, sessionID)
	return nil
}

func (f *fakeLink) Transcript(context.Context, string, string) (string, error) { return "", nil }

type routingRig struct {
	dir     *assistantDirectory
	disp    *assistantDispatcher
	link    *fakeLink
	queries *store.Queries
	local   store.Session
}

// newRoutingRig is this machine (one local session, a real session service)
// paired with zbook, which accepts actions and lists one session and one
// project — the same repository this machine has checked out.
func newRoutingRig(t *testing.T) *routingRig {
	t.Helper()
	db, queries := testutil.SetupDB(t)
	project := testutil.SeedProject(t, queries, "seisiun", t.TempDir())
	local := testutil.SeedSession(t, queries, project.ID, "idle")
	mgr := session.NewManager(db, queries, eventbus.New(), nil)
	svc := session.NewService(mgr, queries, eventbus.New(), nil)

	zb := store.Machine{MachineID: "zbook-id", Label: "zbook", BaseUrl: "https://zbook", Token: "t", IdentityKey: "k"}
	if err := queries.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: zb.MachineID, Label: zb.Label, BaseUrl: zb.BaseUrl, Token: zb.Token, AddedAt: "x", IdentityKey: zb.IdentityKey,
	}); err != nil {
		t.Fatal(err)
	}
	peers, _ := newTestPeers([]store.Machine{zb}, func(_ context.Context, m store.Machine) (peerSnapshot, error) {
		return peerSnapshot{
			Reach: assistant.ReachPeer,
			Sessions: []peer.SessionWire{
				{ID: zbookSession, ProjectID: zbookProjectID, Name: "Plugin Testing", State: "idle",
					WorktreeBranch: "session-a1", AutoApproveMode: "fullAuto"},
			},
			Projects: []peer.ProjectWire{{ID: zbookProjectID, Name: "seisiun", Slug: "seisiun",
				RemoteURL: "github.com/mdjarv/seisiun"}},
		}, nil
	})
	link := &fakeLink{}
	dir := newAssistantDirectory(svc, queries, nil, nil, "self", func(context.Context) string { return "review" })
	dir.peers, dir.link = peers, link
	disp := &assistantDispatcher{svc: svc, queries: queries, peers: peers, link: link}
	return &routingRig{dir: dir, disp: disp, link: link, queries: queries, local: local}
}

func TestLocateRoutesByOwner(t *testing.T) {
	rig := newRoutingRig(t)
	ctx := context.Background()

	local, ok := rig.dir.Locate(ctx, rig.local.ID)
	if !ok || local.Reach != assistant.ReachLocal {
		t.Fatalf("local = %+v %v", local, ok)
	}
	remote, ok := rig.dir.Locate(ctx, zbookSession)
	if !ok || remote.Reach != assistant.ReachPeer || remote.MachineName != "zbook" || remote.ProjectID != "" {
		t.Fatalf("remote = %+v %v", remote, ok)
	}
	if _, ok := rig.dir.SessionBrief(ctx, zbookSession); ok {
		t.Fatal("SessionBrief answered for a paired machine's session; it is the local-only test")
	}
}

func TestProjectsListBothMachines(t *testing.T) {
	rig := newRoutingRig(t)
	rows := rig.dir.ListProjects(context.Background())
	var here, there bool
	for _, row := range rows {
		switch {
		case row.Reach == assistant.ReachLocal && row.MachineName == "review":
			here = true
		case row.ID == zbookProjectID && row.Reach == assistant.ReachPeer && row.MachineName == "zbook":
			there = true
		}
	}
	if !here || !there {
		t.Fatalf("projects = %+v, want this machine's and zbook's", rows)
	}
}

// A send to a paired machine's session goes through its peer surface, with the
// reporting instruction and the policy, and the answer maps to the assistant's
// delivery words.
func TestDispatchRoutesToThePeer(t *testing.T) {
	rig := newRoutingRig(t)
	ctx := context.Background()

	delivery, err := rig.disp.DispatchUnderPolicy(ctx, zbookSession, "load the sample", true, "nightly")
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if delivery != assistant.DeliveryMidTurn {
		t.Fatalf("delivery = %q", delivery)
	}
	if len(rig.link.sent) != 1 || rig.link.sentTo[0] != zbookSession || rig.link.sent[0].PolicyID != "nightly" ||
		!strings.Contains(rig.link.sent[0].Prompt, "AssistantReport") {
		t.Fatalf("sent = %+v to %v", rig.link.sent, rig.link.sentTo)
	}

	ok, _, err := rig.disp.AutoRunnable(ctx, zbookSession)
	if err != nil || !ok {
		t.Fatalf("auto runnable = %v %v", ok, err)
	}
}

// The owner's refusal comes back as a sentence to relay, not a bare failure.
func TestDispatchRelaysTheOwnersRefusal(t *testing.T) {
	rig := newRoutingRig(t)
	rig.link.sendErr = &peerlink.RefusalError{Status: 403, Reason: peer.ReasonMainWorktree, Message: "that session works in the main worktree"}
	_, err := rig.disp.Dispatch(context.Background(), zbookSession, "x", false)
	var refused *assistant.RefusedError
	if !errors.As(err, &refused) || refused.Reason != peer.ReasonMainWorktree || !strings.Contains(refused.Say, "zbook") {
		t.Fatalf("err = %v, want a relayable refusal naming zbook", err)
	}
}

// Creating in zbook's project goes through zbook, and the new session is
// immediately something a send can find.
func TestCreateRoutesToThePeerAndIsImmediatelyAddressable(t *testing.T) {
	rig := newRoutingRig(t)
	ctx := context.Background()

	row, err := rig.dir.CreateSession(ctx, zbookProjectID, "opus")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.ID != createdRemote || row.Reach != assistant.ReachPeer || row.MachineName != "zbook" || row.Model != "Opus" {
		t.Fatalf("row = %+v", row)
	}
	if len(rig.link.created) != 1 || rig.link.created[0].ProjectID != zbookProjectID || rig.link.created[0].Model != "opus" ||
		rig.link.created[0].RequestID == "" {
		t.Fatalf("created = %+v", rig.link.created)
	}
	if _, err := rig.disp.Dispatch(ctx, createdRemote, "go", false); err != nil {
		t.Fatalf("dispatch to the new session: %v", err)
	}
}

func TestCreateRemoteUnknownModelIsTheQuestion(t *testing.T) {
	rig := newRoutingRig(t)
	rig.link.createErr = &peerlink.RefusalError{Status: 422, Reason: peer.ReasonUnknownModel, Families: []string{"Opus", "Sonnet"}}
	_, err := rig.dir.CreateSession(context.Background(), zbookProjectID, "gpt")
	var unknown *assistant.UnknownModelError
	if !errors.As(err, &unknown) || unknown.Spoken != "gpt" || len(unknown.Families) != 2 {
		t.Fatalf("err = %v, want the families zbook has", err)
	}
}

// Through the assistant's own verbs: find, send and create reach zbook, and the
// send leaves the session followed on both sides.
func TestAssistantVerbsActOnAPairedMachine(t *testing.T) {
	rig := newRoutingRig(t)
	svc, err := assistant.New(rig.queries, assistant.WithDirectory(rig.dir), assistant.WithDispatcher(rig.disp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()

	out, err := svc.Invoke(ctx, assistant.VerbRunPrompt, map[string]any{
		"session_id": zbookSession, "prompt": "load the bodhran sample", "target": "Plugin Testing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, refused := out["error"]; refused {
		t.Fatalf("run_prompt refused: %v", out)
	}
	if !svc.Following(ctx, zbookSession) || len(rig.link.followed) != 1 {
		t.Fatalf("following=%v remote follows=%v", svc.Following(ctx, zbookSession), rig.link.followed)
	}

	out, err = svc.Invoke(ctx, assistant.VerbCreateSession, map[string]any{
		"project": "seisiun", "machine": "zbook", "prompt": "add a reel",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sent, _ := out["sent"].(bool); !sent {
		t.Fatalf("create_session on zbook = %v", out)
	}

	// Without a machine the same repository on two machines is a question.
	out, _ = svc.Invoke(ctx, assistant.VerbCreateSession, map[string]any{"project": "seisiun", "prompt": "x"})
	if say, _ := out["error"].(string); !strings.Contains(say, "zbook") {
		t.Fatalf("ambiguous create = %v, want a question naming zbook", out)
	}
}
