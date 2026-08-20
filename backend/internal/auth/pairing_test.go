package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Queries) {
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
	return svc, queries
}

func createAdminUser(t *testing.T, queries *store.Queries) store.User {
	t.Helper()
	u, err := queries.CreateUser(context.Background(), store.CreateUserParams{
		ID: generateUUID(), DisplayName: "admin", IsAdmin: 1,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestGeneratePairingToken(t *testing.T) {
	seen := make(map[string]bool)
	for range 50 {
		token, err := generatePairingToken()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(token) != pairingTokenLength {
			t.Fatalf("length = %d, want %d", len(token), pairingTokenLength)
		}
		for _, c := range token {
			if !strings.ContainsRune(pairingAlphabet, c) {
				t.Fatalf("token %q contains %q outside alphabet", token, c)
			}
		}
		if seen[token] {
			t.Fatalf("duplicate token %q", token)
		}
		seen[token] = true
	}
}

func TestNormalizePairingToken(t *testing.T) {
	for input, want := range map[string]string{
		"abcd-efgh-jklm": "ABCDEFGHJKLM",
		" ab cd ":        "ABCD",
		"ABCDEFGHJKLM":   "ABCDEFGHJKLM",
	} {
		if got := normalizePairingToken(input); got != want {
			t.Errorf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRequireAdminFailsClosed(t *testing.T) {
	svc, queries := newTestService(t)
	createAdminUser(t, queries)

	// Unset secret must never match — including an empty presented header.
	r := httptest.NewRequest(http.MethodPost, "/api/auth/pairing-tokens", nil)
	if _, _, err := svc.requireAdmin(r); err == nil {
		t.Fatal("empty secret + empty header must be rejected")
	}

	svc.SetAdminSecret("real-secret")
	r = httptest.NewRequest(http.MethodPost, "/api/auth/pairing-tokens", nil)
	r.Header.Set(adminSecretHeader, "wrong")
	if _, _, err := svc.requireAdmin(r); err == nil {
		t.Fatal("wrong secret must be rejected")
	}

	r = httptest.NewRequest(http.MethodPost, "/api/auth/pairing-tokens", nil)
	r.Header.Set(adminSecretHeader, "real-secret")
	userID, _, err := svc.requireAdmin(r)
	if err != nil {
		t.Fatalf("valid secret rejected: %v", err)
	}
	if userID == "" {
		t.Fatal("expected acting admin user id")
	}
}

// mintToken drives the real handler with the admin secret and returns the token.
func mintToken(t *testing.T, svc *Service, body string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/pairing-tokens", strings.NewReader(body))
	r.Header.Set(adminSecretHeader, "test-secret")
	w := httptest.NewRecorder()
	svc.handleMintPairingToken(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("mint status = %d, body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode mint response: %v", err)
	}
	return resp.Token
}

func exchangeToken(t *testing.T, svc *Service, token string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": token, "label": "test client"})
	r := httptest.NewRequest(http.MethodPost, "/api/auth/pair", bytes.NewReader(body))
	w := httptest.NewRecorder()
	svc.handlePairExchange(w, r)
	return w
}

func TestPairingExchangeFlow(t *testing.T) {
	svc, queries := newTestService(t)
	createAdminUser(t, queries)
	svc.SetAdminSecret("test-secret")

	token := mintToken(t, svc, "")

	// Exchange succeeds once.
	w := exchangeToken(t, svc, token)
	if w.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected bearer token")
	}

	// The bearer authenticates requests.
	r := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	r.Header.Set("Authorization", "Bearer "+resp.Token)
	session, err := svc.authenticateRequest(r)
	if err != nil {
		t.Fatalf("bearer auth failed: %v", err)
	}
	if session.Kind != "bearer" || session.Label != "test client" {
		t.Fatalf("session kind/label = %q/%q, want bearer/test client", session.Kind, session.Label)
	}

	// Second exchange of the same token fails (one-time).
	if w := exchangeToken(t, svc, token); w.Code != http.StatusUnauthorized {
		t.Fatalf("reused token status = %d, want 401", w.Code)
	}

	// A normalized (lowercase, dashed) token still exchanges.
	token2 := mintToken(t, svc, "")
	lower := strings.ToLower(token2[:4] + "-" + token2[4:])
	if w := exchangeToken(t, svc, lower); w.Code != http.StatusOK {
		t.Fatalf("normalized token status = %d, body %s", w.Code, w.Body.String())
	}
}

func TestPairingTokenExpiry(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)

	token, err := generatePairingToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	expired := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if err := queries.CreatePairingToken(context.Background(), store.CreatePairingTokenParams{
		Token: token, UserID: admin.ID, ExpiresAt: expired,
	}); err != nil {
		t.Fatalf("create pairing token: %v", err)
	}

	if w := exchangeToken(t, svc, token); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token status = %d, want 401", w.Code)
	}
}

func TestBearerDoesNotFallBackToCookie(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	cookieToken, err := svc.createSession(context.Background(), admin.ID, "", "cookie")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// A bad bearer must fail even when a valid cookie rides along.
	r := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	r.Header.Set("Authorization", "Bearer bogus")
	r.AddCookie(&http.Cookie{Name: cookieName, Value: cookieToken})
	if _, err := svc.authenticateRequest(r); err == nil {
		t.Fatal("invalid bearer must not fall back to the cookie")
	}
}

func TestWSTicketFlow(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	bearer, err := svc.createSession(context.Background(), admin.ID, "client", "bearer")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/auth/ws-ticket", nil)
	r.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	svc.handleCreateWSTicket(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("ticket status = %d, body %s", w.Code, w.Body.String())
	}
	var resp struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode ticket response: %v", err)
	}

	// Redeems exactly once.
	session, err := svc.redeemWSTicket(context.Background(), resp.Ticket)
	if err != nil {
		t.Fatalf("redeem failed: %v", err)
	}
	if session.UserID != admin.ID {
		t.Fatalf("redeemed user = %q, want %q", session.UserID, admin.ID)
	}
	if _, err := svc.redeemWSTicket(context.Background(), resp.Ticket); err == nil {
		t.Fatal("second redeem must fail")
	}

	// A ticket for a revoked session must not authenticate.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, "/api/auth/ws-ticket", nil)
	r2.Header.Set("Authorization", "Bearer "+bearer)
	svc.handleCreateWSTicket(w2, r2)
	var resp2 struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode second ticket: %v", err)
	}
	if err := queries.DeleteAuthSession(context.Background(), bearer); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := svc.redeemWSTicket(context.Background(), resp2.Ticket); err == nil {
		t.Fatal("ticket for a revoked session must fail")
	}
}

func TestRevokeSession(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	svc.SetAdminSecret("test-secret")
	if _, err := svc.createSession(context.Background(), admin.ID, "phone", "bearer"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	rows, err := queries.ListAuthSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("sessions = %d, want 1", len(rows))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", svc.handleRevokeSession)
	r := httptest.NewRequest(http.MethodDelete, "/api/auth/sessions/"+rows[0].ID.String, nil)
	r.Header.Set(adminSecretHeader, "test-secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, body %s", w.Code, w.Body.String())
	}

	rows, err = queries.ListAuthSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("sessions after revoke = %d, want 0", len(rows))
	}
}
