package peer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/providers"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

const (
	worktreeSession = "00000000-0000-4000-8000-00000000000a"
	mainSession     = "00000000-0000-4000-8000-00000000000b"
	projectSeisiun  = "00000000-0000-4000-8000-0000000000c1"
	projectTwinA    = "00000000-0000-4000-8000-0000000000c2"
	projectTwinB    = "00000000-0000-4000-8000-0000000000c3"
)

type fakeSessions struct {
	mu      sync.Mutex
	infos   map[string]session.SessionInfo
	sent    []session.QueryOrigin
	created []session.CreateSessionParams
	sendErr error
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{infos: map[string]session.SessionInfo{
		worktreeSession: {ID: worktreeSession, ProjectID: projectSeisiun, Name: "Plugin Testing", State: "idle",
			WorktreeBranch: "session-a", AutoApproveMode: "fullAuto", Model: "claude-opus-5"},
		mainSession: {ID: mainSession, ProjectID: projectSeisiun, Name: "Local work", State: "idle",
			AutoApproveMode: "fullAuto"},
	}}
}

func (f *fakeSessions) ListAllSessions(context.Context) (session.ListSessionsResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out session.ListSessionsResult
	for _, info := range f.infos {
		out.Sessions = append(out.Sessions, info)
	}
	return out, nil
}

func (f *fakeSessions) GetSessionInfo(_ context.Context, id string) (session.SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.infos[id]
	if !ok {
		return session.SessionInfo{}, sql.ErrNoRows
	}
	return info, nil
}

func (f *fakeSessions) CreateSession(_ context.Context, p session.CreateSessionParams) (session.CreateSessionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, p)
	id := "00000000-0000-4000-8000-0000000000f1"
	f.infos[id] = session.SessionInfo{ID: id, ProjectID: p.ProjectID, State: "idle", WorktreeBranch: "session-new",
		AutoApproveMode: p.AutoApproveMode, Origin: p.Origin}
	return session.CreateSessionResult{SessionID: id, State: "idle", WorktreeBranch: "session-new",
		AutoApproveMode: p.AutoApproveMode, Model: p.Model}, nil
}

func (f *fakeSessions) EnqueueMessageWithOrigin(_ context.Context, _, _ string, _ []session.QueryAttachment,
	origin session.QueryOrigin,
) (session.MessageDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return "", f.sendErr
	}
	f.sent = append(f.sent, origin)
	return session.DeliveryTurn, nil
}

type fakeProjects []store.Project

func (p fakeProjects) ListProjects(context.Context) ([]store.Project, error) { return p, nil }

type fakeCatalog struct{}

func (fakeCatalog) ResolveFamily(_ context.Context, _, spoken string) (providers.ModelInfo, bool) {
	if strings.EqualFold(spoken, "opus") {
		return providers.ModelInfo{Slug: "claude-opus-5", DisplayName: "Opus"}, true
	}
	return providers.ModelInfo{}, false
}

func (fakeCatalog) FamilyNames(context.Context, string) []string { return []string{"Opus", "Sonnet"} }

var testProjects = fakeProjects{
	{ID: projectSeisiun, Name: "seisiun", Slug: "seisiun", RemoteUrl: "github.com/mdjarv/seisiun", Path: "/secret/path"},
	{ID: projectTwinA, Name: "twin", RemoteUrl: "github.com/x/twin"},
	{ID: projectTwinB, Name: "twin copy", RemoteUrl: "github.com/x/twin"},
}

func peerRow(kind string) *store.GetAuthSessionRow {
	return &store.GetAuthSessionRow{Kind: kind, ID: sql.NullString{String: "cred-1", Valid: true}}
}

func serve(t *testing.T, h *Handler, row *store.GetAuthSessionRow, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if row != nil {
		req = req.WithContext(auth.ContextWithSession(req.Context(), row))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func reasonOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Reason
}

// A browser's credential, or none at all on an auth-disabled listener, never
// opens the surface.
func TestHandlerRequiresAPeerCredential(t *testing.T) {
	h := New(newFakeSessions(), testProjects, WithSettings(Settings{AcceptActions: true}))
	for _, row := range []*store.GetAuthSessionRow{nil, peerRow("bearer"), peerRow("cookie")} {
		rec := serve(t, h, row, http.MethodGet, "/api/peer/sessions", "")
		if rec.Code != http.StatusForbidden || reasonOf(t, rec) != ReasonNotPeer {
			t.Errorf("credential %+v: %d %s", row, rec.Code, rec.Body.String())
		}
	}
}

func TestListNeverSendsAPath(t *testing.T) {
	h := New(newFakeSessions(), testProjects, WithMachineID("zbook-id"))
	rec := serve(t, h, peerRow(auth.KindPeer), http.MethodGet, "/api/peer/sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/secret/path") {
		t.Fatal("a project path left the machine")
	}
	var out SessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.PeerSurface != SurfaceVersion || out.AcceptActions || len(out.Sessions) != 2 || out.MachineID != "zbook-id" {
		t.Fatalf("list = %+v", out)
	}
}

func TestSendIsAssistantOriginAndGuarded(t *testing.T) {
	sessions := newFakeSessions()
	h := New(sessions, testProjects, WithSettings(Settings{AcceptActions: true}))
	peer := peerRow(auth.KindPeer)

	rec := serve(t, h, peer, http.MethodPost, "/api/peer/sessions/"+worktreeSession+"/send", `{"prompt":"load the sample"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"turn"`) {
		t.Fatalf("send = %d %s", rec.Code, rec.Body.String())
	}
	if len(sessions.sent) != 1 || sessions.sent[0].Kind != session.OriginAssistant {
		t.Fatalf("origin = %+v, want assistant from the credential", sessions.sent)
	}

	cases := []struct {
		target, body, reason string
	}{
		{"/api/peer/sessions/" + mainSession + "/send", `{"prompt":"x"}`, ReasonMainWorktree},
		{"/api/peer/sessions/" + worktreeSession + "/send", `{"prompt":"x","policyId":"nightly"}`, ReasonPoliciesOff},
		{"/api/peer/sessions/00000000-0000-4000-8000-0000000000ff/send", `{"prompt":"x"}`, ReasonNotFound},
		{"/api/peer/sessions/not-a-uuid/send", `{"prompt":"x"}`, ReasonBadRequest},
		{"/api/peer/sessions/" + worktreeSession + "/send", `{"prompt":"  "}`, ReasonBadRequest},
		// There is no origin field to claim, and an unknown field is refused
		// rather than silently ignored.
		{"/api/peer/sessions/" + worktreeSession + "/send", `{"prompt":"x","origin":""}`, ReasonBadRequest},
	}
	for _, c := range cases {
		rec := serve(t, h, peer, http.MethodPost, c.target, c.body)
		if got := reasonOf(t, rec); got != c.reason {
			t.Errorf("%s %s = %d %q, want %q", c.target, c.body, rec.Code, got, c.reason)
		}
	}
	if len(sessions.sent) != 1 {
		t.Fatalf("a refused send reached the session: %d sends", len(sessions.sent))
	}
}

// A machine that has not opted in answers the same for an id that exists and
// one that does not.
func TestSendOffRevealsNothing(t *testing.T) {
	h := New(newFakeSessions(), testProjects)
	peer := peerRow(auth.KindPeer)
	for _, id := range []string{worktreeSession, "00000000-0000-4000-8000-0000000000ff"} {
		rec := serve(t, h, peer, http.MethodPost, "/api/peer/sessions/"+id+"/send", `{"prompt":"x"}`)
		if reasonOf(t, rec) != ReasonActionsOff {
			t.Errorf("%s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
}

func TestSendRateLimitPerCredential(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	h := New(newFakeSessions(), testProjects, WithSettings(Settings{AcceptActions: true}),
		withClock(func() time.Time { return now }))
	target := "/api/peer/sessions/" + worktreeSession + "/send"
	for i := 0; i < SendsPerMinute; i++ {
		if rec := serve(t, h, peerRow(auth.KindPeer), http.MethodPost, target, `{"prompt":"x"}`); rec.Code != http.StatusOK {
			t.Fatalf("send %d = %d", i, rec.Code)
		}
	}
	if rec := serve(t, h, peerRow(auth.KindPeer), http.MethodPost, target, `{"prompt":"x"}`); reasonOf(t, rec) != ReasonRate {
		t.Fatalf("over the limit = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateResolvesAndForcesContainment(t *testing.T) {
	sessions := newFakeSessions()
	h := New(sessions, testProjects, WithSettings(Settings{AcceptActions: true}), WithCatalog(fakeCatalog{}))
	peer := peerRow(auth.KindPeer)

	rec := serve(t, h, peer, http.MethodPost, "/api/peer/sessions",
		`{"remoteUrl":"github.com/mdjarv/seisiun","model":"opus","prompt":"add a bodhran","requestId":"r1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var out CreateResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Delivery != "turn" || out.Session.ID == "" {
		t.Fatalf("create = %+v", out)
	}
	p := sessions.created[0]
	if !p.Worktree || p.AutoApproveMode != "fullAuto" || p.Origin != session.OriginAssistant ||
		p.ProjectID != projectSeisiun || p.Model != "claude-opus-5" || p.IdempotencyKey != "peer:cred-1:r1" {
		t.Fatalf("params = %+v", p)
	}

	cases := []struct{ body, reason string }{
		{`{"remoteUrl":"github.com/x/twin"}`, ReasonAmbiguous},
		{`{"remoteUrl":"github.com/nope/nope"}`, ReasonNoProject},
		{`{"projectId":"` + projectSeisiun + `","model":"gpt"}`, ReasonUnknownModel},
		{`{}`, ReasonBadRequest},
		{`{"projectId":"` + projectSeisiun + `","remoteUrl":"github.com/mdjarv/seisiun"}`, ReasonBadRequest},
		{`{"projectId":"` + projectSeisiun + `","policyId":"nightly"}`, ReasonPoliciesOff},
	}
	for _, c := range cases {
		rec := serve(t, h, peer, http.MethodPost, "/api/peer/sessions", c.body)
		if got := reasonOf(t, rec); got != c.reason {
			t.Errorf("%s = %d %q, want %q", c.body, rec.Code, got, c.reason)
		}
	}
	if len(sessions.created) != 1 {
		t.Fatalf("a refused create reached the service: %d", len(sessions.created))
	}
}

// The send half failing does not unmake the session, and the answer says both.
func TestCreateReportsBothHalves(t *testing.T) {
	sessions := newFakeSessions()
	sessions.sendErr = errors.New("runtime not ready")
	h := New(sessions, testProjects, WithSettings(Settings{AcceptActions: true}))
	rec := serve(t, h, peerRow(auth.KindPeer), http.MethodPost, "/api/peer/sessions",
		`{"projectId":"`+projectSeisiun+`","prompt":"go"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var out CreateResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Session.ID == "" || out.Delivery != "" || out.SendError == "" {
		t.Fatalf("create = %+v, want the session and the send's failure", out)
	}
	if strings.Contains(rec.Body.String(), "runtime not ready") {
		t.Fatal("an internal error's text reached the response")
	}
}

func TestCreateInFlightCap(t *testing.T) {
	sessions := newFakeSessions()
	for i := 0; i < MaxInFlightAssistant; i++ {
		id := "10000000-0000-4000-8000-" + strings.Repeat("0", 10) + string(rune('a'+i/10)) + string(rune('a'+i%10))
		sessions.infos[id] = session.SessionInfo{ID: id, State: "stopped", Origin: session.OriginAssistant}
	}
	h := New(sessions, testProjects, WithSettings(Settings{AcceptActions: true}))
	rec := serve(t, h, peerRow(auth.KindPeer), http.MethodPost, "/api/peer/sessions", `{"projectId":"`+projectSeisiun+`"}`)
	if reasonOf(t, rec) != ReasonInFlight {
		t.Fatalf("at the cap (parked sessions count) = %d %s", rec.Code, rec.Body.String())
	}
}
