package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// newTestServiceWithDB is newTestService plus the raw handle, so a test can go
// looking for secrets the store layer would never hand back.
func newTestServiceWithDB(t *testing.T) (*Service, *store.Queries, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	queries := store.New(db)
	svc, err := NewService(queries, "localhost", []string{"http://localhost:9201"})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	identity, err := machine.LoadOrCreateSigningIdentity(t.TempDir(), testMachineID)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetMachineIdentity(testMachineID, identity)
	return svc, queries, db
}

// The point of the whole change: after issuing every kind of credential, none
// of them is recoverable from the database file. Reads the raw tables rather
// than the store API, because that is what an attacker with the file does.
func TestNoIssuedCredentialIsRecoverableFromTheDatabase(t *testing.T) {
	svc, queries, db := newTestServiceWithDB(t)
	ctx := context.Background()
	admin := createAdminUser(t, queries)
	svc.SetAdminSecret("test-secret")

	sessionToken, err := svc.createSession(ctx, admin.ID, "laptop", "cookie")
	if err != nil {
		t.Fatal(err)
	}
	pairingToken := mintToken(t, svc, "")
	rekeyCode, _, err := MintRekeyCode(ctx, queries, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/auth/invite", nil)
	r.Header.Set(adminSecretHeader, "test-secret")
	w := httptest.NewRecorder()
	svc.handleCreateInvite(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create invite: %d %s", w.Code, w.Body.String())
	}
	var invite struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &invite); err != nil {
		t.Fatal(err)
	}

	secrets := map[string]string{
		"session token": sessionToken,
		"pairing token": pairingToken,
		"recovery code": rekeyCode,
		"invite token":  invite.Token,
	}

	for _, table := range []string{"auth_sessions", "pairing_tokens", "invite_tokens"} {
		rows, err := db.Query("SELECT * FROM " + table) //nolint:gosec // fixed table names
		if err != nil {
			t.Fatalf("select %s: %v", table, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cols {
			if c == "token" {
				t.Errorf("%s still has a plaintext `token` column", table)
			}
		}
		for rows.Next() {
			cells := make([]any, len(cols))
			for i := range cells {
				cells[i] = new(sql.NullString)
			}
			if err := rows.Scan(cells...); err != nil {
				t.Fatal(err)
			}
			for i, cell := range cells {
				got := cell.(*sql.NullString).String
				for name, secret := range secrets {
					if got == secret {
						t.Errorf("%s.%s stores the %s verbatim", table, cols[i], name)
					}
				}
			}
		}
		rows.Close()
	}
}

// Hashing is only worth anything if the credentials still work.
func TestHashedCredentialsStillAuthenticate(t *testing.T) {
	svc, queries, _ := newTestServiceWithDB(t)
	ctx := context.Background()
	admin := createAdminUser(t, queries)
	svc.SetAdminSecret("test-secret")

	token, err := svc.createSession(ctx, admin.ID, "laptop", "cookie")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	session, err := svc.authenticateRequest(r)
	if err != nil {
		t.Fatalf("cookie session did not authenticate: %v", err)
	}
	if session.UserID != admin.ID {
		t.Errorf("session user = %s, want %s", session.UserID, admin.ID)
	}

	// Pairing round-trips, and the bearer it returns authenticates.
	pairing := mintToken(t, svc, "")
	w := exchangeToken(t, svc, pairing)
	if w.Code != http.StatusOK {
		t.Fatalf("pair exchange = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	br := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	br.Header.Set("Authorization", "Bearer "+resp.Token)
	if _, err := svc.authenticateRequest(br); err != nil {
		t.Errorf("paired bearer did not authenticate: %v", err)
	}

	// Logout revokes by digest.
	lr := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	lr.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	svc.handleLogout(httptest.NewRecorder(), lr)
	if _, err := svc.authenticateRequest(r); err == nil {
		t.Error("session still authenticates after logout")
	}
}

func TestHashTokenIsStableAndOpaque(t *testing.T) {
	const token = "a-token"
	first, second := hashToken(token), hashToken(token)
	if first != second {
		t.Error("hashToken is not deterministic — lookups would fail")
	}
	if len(first) != 64 {
		t.Errorf("digest length = %d, want 64 hex chars", len(first))
	}
	if strings.Contains(first, token) {
		t.Error("digest contains the token")
	}
	if hashToken("a-tokem") == first {
		t.Error("distinct tokens collide")
	}
}
