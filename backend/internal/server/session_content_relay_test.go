package server_test

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

const (
	relayOwnerID  = "40000000-0000-4000-8000-000000000001"
	relayProject  = "40000000-0000-4000-8000-0000000000c1"
	relaySession  = "40000000-0000-4000-8000-0000000000a1"
	relayOperator = "00000000-0000-4000-8000-000000000001"
)

// Two real servers: a browser that holds only the primary's cookie opens a
// file an agent wrote on the paired machine, through the primary, and gets the
// primary's headers for it.
func TestSessionFileRelaysThroughThePrimary(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	dir := filepath.Join(paths.SessionFilesDir(), relaySession)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"report.md": "# from zbook", "page.html": "<script>1</script>"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	owner, ownerBearer, ownerKey := startRelayOwner(t)
	primary, cookie := startRelayPrimary(t, owner.URL, ownerBearer, ownerKey)

	get := func(path string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, primary.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: "agentique_session", Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	resp := get("/api/sessions/" + relaySession + "/files/report.md")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "# from zbook" {
		t.Fatalf("relayed report = %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("report Content-Type = %q", ct)
	}

	resp = get("/api/sessions/" + relaySession + "/files/page.html")
	if resp.Header.Get("Content-Type") != "application/octet-stream" ||
		resp.Header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" {
		t.Errorf("relayed html headers = %v", resp.Header)
	}

	if resp := get("/api/sessions/" + relaySession + "/files/missing.md"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing on the owner = %d, want 404", resp.StatusCode)
	}
	if resp := get("/api/sessions/40000000-0000-4000-8000-0000000000ff/files/report.md"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("session nobody has = %d, want 404", resp.StatusCode)
	}

	// The owner goes away: the file is unavailable, and says so.
	owner.Close()
	if resp := get("/api/sessions/" + relaySession + "/files/report.md"); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("owner down = %d, want 502", resp.StatusCode)
	}
}

func startRelayOwner(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	db := testutil.OpenMigratedDB(t)
	q := store.New(db)
	identity, err := machine.LoadOrCreateSigningIdentity(t.TempDir(), relayOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, name, path, slug) VALUES (?, 'seisiun', '/tmp/s', 'seisiun')`, relayProject); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, project_id, name, work_dir) VALUES (?, ?, 'Plugin Testing', '/tmp/s')`, relaySession, relayProject); err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateUser(context.Background(), store.CreateUserParams{ID: relayOperator, DisplayName: "operator", IsAdmin: 1}); err != nil {
		t.Fatal(err)
	}
	bearer := "owner-pairing-bearer"
	if err := q.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		TokenHash: auth.HashToken(bearer), ID: sql.NullString{String: "pairing", Valid: true}, UserID: relayOperator,
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Kind: "bearer",
	}); err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(q, server.Config{
		AuthEnabled: true, RPID: "localhost", RPOrigins: []string{"http://localhost"}, DB: db,
		MachineID: relayOwnerID, MachineIdentity: identity,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { srv.Shutdown(); ts.Close(); db.Close() })
	return ts, bearer, identity.PublicKey()
}

func startRelayPrimary(t *testing.T, ownerURL, ownerBearer, ownerKey string) (*httptest.Server, string) {
	t.Helper()
	db := testutil.OpenMigratedDB(t)
	q := store.New(db)
	srv, err := server.New(q, server.Config{
		AuthEnabled: true, RPID: "localhost", RPOrigins: []string{"http://localhost"}, DB: db,
		MachineID: "40000000-0000-4000-8000-000000000002",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { srv.Shutdown(); ts.Close(); db.Close() })
	cookie := createCookieSession(t, q, true)
	if err := q.UpsertMachine(context.Background(), store.UpsertMachineParams{
		MachineID: relayOwnerID, Label: "zbook", BaseUrl: ownerURL, Token: ownerBearer,
		AddedAt: "2026-09-15T00:00:00Z", IdentityKey: ownerKey,
	}); err != nil {
		t.Fatal(err)
	}
	return ts, cookie
}
