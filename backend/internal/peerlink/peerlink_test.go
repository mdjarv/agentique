package peerlink

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/peer"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

const (
	ownerMachineID = "20000000-0000-4000-8000-000000000001"
	adminID        = "20000000-0000-4000-8000-0000000000aa"
	remoteSession  = "20000000-0000-4000-8000-0000000000b1"
	remoteProject  = "20000000-0000-4000-8000-0000000000c1"
)

func openDB(t *testing.T) (*sql.DB, *store.Queries) {
	t.Helper()
	db := testutil.OpenMigratedDB(t)
	return db, store.New(db)
}

type ownerSessions struct {
	mu   sync.Mutex
	sent []string
}

func (o *ownerSessions) ListAllSessions(context.Context) (session.ListSessionsResult, error) {
	return session.ListSessionsResult{Sessions: []session.SessionInfo{{ID: remoteSession, ProjectID: remoteProject,
		Name: "Plugin Testing", State: "idle", WorktreeBranch: "session-b1", AutoApproveMode: "fullAuto"}}}, nil
}

func (o *ownerSessions) GetSessionInfo(_ context.Context, id string) (session.SessionInfo, error) {
	if id != remoteSession {
		return session.SessionInfo{}, sql.ErrNoRows
	}
	return session.SessionInfo{ID: id, WorktreeBranch: "session-b1", AutoApproveMode: "fullAuto"}, nil
}

func (o *ownerSessions) CreateSession(context.Context, session.CreateSessionParams) (session.CreateSessionResult, error) {
	return session.CreateSessionResult{}, errors.New("not in this test")
}

func (o *ownerSessions) EnqueueMessageWithOrigin(_ context.Context, _, prompt string, _ []session.QueryAttachment,
	_ session.QueryOrigin,
) (session.MessageDelivery, error) {
	o.mu.Lock()
	o.sent = append(o.sent, prompt)
	o.mu.Unlock()
	return session.DeliveryTurn, nil
}

type noProjects struct{}

func (noProjects) ListProjects(context.Context) ([]store.Project, error) { return nil, nil }

// owner is a paired machine built from the real pieces: the auth service and
// its middleware, the peer handler, and its identity endpoints.
type owner struct {
	server  *httptest.Server
	queries *store.Queries
	auth    *auth.Service
	key     string
	bearer  string
	mints   atomic.Int32
}

func newOwner(t *testing.T, withPeerSurface bool) *owner {
	t.Helper()
	db, q := openDB(t)
	// The session the fake service describes exists as a row too, because the
	// outbox's follows reference sessions(id).
	if _, err := db.Exec(`INSERT INTO projects (id, name, path, slug) VALUES (?, 'seisiun', '/tmp/s', 'seisiun')`, remoteProject); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, project_id, name, work_dir) VALUES (?, ?, 'Plugin Testing', '/tmp/s')`, remoteSession, remoteProject); err != nil {
		t.Fatal(err)
	}
	svc, err := auth.NewService(q, "localhost", []string{"http://localhost"})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := machine.LoadOrCreateSigningIdentity(t.TempDir(), ownerMachineID)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetMachineIdentity(ownerMachineID, identity)
	if _, err := q.CreateUser(context.Background(), store.CreateUserParams{ID: adminID, DisplayName: "admin", IsAdmin: 1}); err != nil {
		t.Fatal(err)
	}
	bearer := "owner-bearer-token"
	if err := q.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		TokenHash: auth.HashToken(bearer), ID: sql.NullString{String: "bearer-id", Valid: true}, UserID: adminID,
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Kind: "bearer",
	}); err != nil {
		t.Fatal(err)
	}

	o := &owner{queries: q, auth: svc, key: identity.PublicKey(), bearer: bearer}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agentique/environment", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"machineId": ownerMachineID, "identityKey": identity.PublicKey()})
	})
	if withPeerSurface {
		svc.RegisterRoutes(mux)
		peer.New(&ownerSessions{}, noProjects{}, peer.WithSettings(peer.Settings{AcceptActions: true}),
			peer.WithOutbox(peer.NewOutbox(q, nil))).RegisterRoutes(mux)
	} else {
		// An older release: identity proof, no peer credential, no peer routes.
		mux.HandleFunc("POST /api/auth/identity-proof", func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Nonce string `json:"nonce"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			proof, _ := identity.SignChallenge(body.Nonce)
			_ = json.NewEncoder(w).Encode(map[string]string{"machineId": ownerMachineID, "identityKey": identity.PublicKey(), "proof": proof})
		})
	}
	counting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/peer-credential" {
			o.mints.Add(1)
		}
		mux.ServeHTTP(w, r)
	})
	o.server = httptest.NewServer(svc.Middleware(counting))
	t.Cleanup(o.server.Close)
	return o
}

// actingCatalog is this server's database with the owner paired.
func actingCatalog(t *testing.T, o *owner) *store.Queries {
	t.Helper()
	_, q := openDB(t)
	if err := q.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: ownerMachineID, Label: "zbook", BaseUrl: o.server.URL, Token: o.bearer,
		AddedAt: "2026-09-14T00:00:00Z", IdentityKey: o.key,
	}); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestClientMintsOnceAndActsWithThePeerCredential(t *testing.T) {
	o := newOwner(t, true)
	catalog := actingCatalog(t, o)
	c := New(o.server.Client(), catalog, WithLabel(func(context.Context) string { return "review" }))
	ctx := context.Background()

	list, err := c.List(ctx, ownerMachineID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.PeerSurface != peer.SurfaceVersion || !list.AcceptActions || len(list.Sessions) != 1 {
		t.Fatalf("list = %+v", list)
	}
	m, _ := catalog.GetMachine(ctx, ownerMachineID)
	if m.PeerToken == "" || m.PeerToken == o.bearer || m.PeerSessionID == "" {
		t.Fatalf("stored credential = %q/%q, want a peer credential distinct from the bearer", m.PeerToken, m.PeerSessionID)
	}

	sent, err := c.Send(ctx, ownerMachineID, remoteSession, peer.SendRequest{Prompt: "go"})
	if err != nil || sent.Delivery != "turn" {
		t.Fatalf("send = %+v, %v", sent, err)
	}
	if err := c.Follow(ctx, ownerMachineID, remoteSession); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if o.mints.Load() != 1 {
		t.Fatalf("mints = %d, want 1", o.mints.Load())
	}
}

// A refused peer credential is rotated once — the old one deleted on the
// owner, not left alive — and the call succeeds.
func TestClientRotatesARefusedCredential(t *testing.T) {
	o := newOwner(t, true)
	catalog := actingCatalog(t, o)
	c := New(o.server.Client(), catalog)
	ctx := context.Background()

	if _, err := c.List(ctx, ownerMachineID); err != nil {
		t.Fatal(err)
	}
	first, _ := catalog.GetMachine(ctx, ownerMachineID)
	if _, err := o.queries.DeleteAuthSessionByID(ctx, sql.NullString{String: first.PeerSessionID, Valid: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := c.List(ctx, ownerMachineID); err != nil {
		t.Fatalf("list after revocation: %v", err)
	}
	second, _ := catalog.GetMachine(ctx, ownerMachineID)
	if second.PeerToken == first.PeerToken || o.mints.Load() != 2 {
		t.Fatalf("not rotated: mints=%d", o.mints.Load())
	}
}

func TestClientRefusalIsTyped(t *testing.T) {
	o := newOwner(t, true)
	c := New(o.server.Client(), actingCatalog(t, o))
	_, err := c.Send(context.Background(), ownerMachineID, "20000000-0000-4000-8000-0000000000ff", peer.SendRequest{Prompt: "x"})
	var refusal *RefusalError
	if !errors.As(err, &refusal) || refusal.Reason != peer.ReasonNotFound {
		t.Fatalf("err = %v, want a not-found refusal", err)
	}
}

// An older release has no peer surface: the answer says so, and nothing is
// attempted with the bearer.
func TestClientReportsAnOlderRelease(t *testing.T) {
	o := newOwner(t, false)
	c := New(o.server.Client(), actingCatalog(t, o))
	_, err := c.List(context.Background(), ownerMachineID)
	if !errors.Is(err, ErrNoPeerSurface) {
		t.Fatalf("err = %v, want ErrNoPeerSurface", err)
	}
}

func TestClientNeedsAPairing(t *testing.T) {
	o := newOwner(t, true)
	catalog := actingCatalog(t, o)
	_ = catalog.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: ownerMachineID, Label: "zbook", BaseUrl: o.server.URL, AddedAt: "x", IdentityKey: o.key,
	})
	_, err := New(o.server.Client(), catalog).List(context.Background(), ownerMachineID)
	if !errors.Is(err, ErrNotPaired) {
		t.Fatalf("err = %v, want ErrNotPaired", err)
	}
}
