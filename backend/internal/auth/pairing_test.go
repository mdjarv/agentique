package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/machine"
	"github.com/mdjarv/agentique/backend/internal/store"
)

const testMachineID = "10000000-0000-4000-8000-000000000001"

var testPairingNonce = base64.RawURLEncoding.EncodeToString(make([]byte, 32))

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
	identity, err := machine.LoadOrCreateSigningIdentity(t.TempDir(), testMachineID)
	if err != nil {
		t.Fatalf("create machine signing identity: %v", err)
	}
	svc.SetMachineIdentity(testMachineID, identity)
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

func TestLoadOrCreateAdminSecretSecuresExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, adminSecretFile)
	if err := os.WriteFile(path, []byte("existing-secret\n"), 0o644); err != nil {
		t.Fatalf("write existing secret: %v", err)
	}

	secret, err := LoadOrCreateAdminSecret(dir)
	if err != nil {
		t.Fatalf("load existing secret: %v", err)
	}
	if secret != "existing-secret" {
		t.Fatalf("secret = %q, want existing value", secret)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat secret: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode = %o, want 600", info.Mode().Perm())
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
	body, _ := json.Marshal(map[string]string{
		"token": token, "label": "test client", "nonce": testPairingNonce,
	})
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
		Token       string `json:"token"`
		SessionID   string `json:"sessionId"`
		MachineID   string `json:"machineId"`
		IdentityKey string `json:"identityKey"`
		Proof       string `json:"proof"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected bearer token")
	}
	if resp.SessionID == "" {
		t.Fatal("expected public auth session id")
	}
	if resp.MachineID != testMachineID || resp.IdentityKey == "" || resp.Proof == "" {
		t.Fatalf("incomplete machine identity response: %+v", resp)
	}
	if err := machine.VerifyChallenge(resp.IdentityKey, resp.MachineID, testPairingNonce, resp.Proof); err != nil {
		t.Fatalf("verify pairing identity proof: %v", err)
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

func TestPairingExchangeRejectsOversizedFieldsWithoutConsumingToken(t *testing.T) {
	svc, queries := newTestService(t)
	createAdminUser(t, queries)
	svc.SetAdminSecret("test-secret")

	for name, field := range map[string]string{
		"label":          "label",
		"replacement id": "replaceSessionId",
	} {
		t.Run(name, func(t *testing.T) {
			token := mintToken(t, svc, "")
			body, err := json.Marshal(map[string]string{
				"token": token, "label": "client", "nonce": testPairingNonce,
				field: strings.Repeat("x", 129),
			})
			if err != nil {
				t.Fatalf("encode request: %v", err)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/auth/pair", bytes.NewReader(body))
			w := httptest.NewRecorder()
			svc.handlePairExchange(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("oversized %s status = %d, want 400: %s", field, w.Code, w.Body.String())
			}

			if retry := exchangeToken(t, svc, token); retry.Code != http.StatusOK {
				t.Fatalf("rejected request consumed token: status = %d: %s", retry.Code, retry.Body.String())
			}
		})
	}
}

func TestSessionCookieIsSecureBehindConfiguredHTTPSReverseProxy(t *testing.T) {
	_, queries := newTestService(t)
	svc, err := NewService(queries, "public.example", []string{
		"https://public.example", "http://localhost:9201",
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	proxied := httptest.NewRequest(http.MethodPost, "http://public.example/api/auth/login/finish", nil)
	proxied.Host = "public.example"
	w := httptest.NewRecorder()
	svc.setSessionCookie(w, proxied, "token")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("proxied HTTPS cookie must be Secure: %+v", cookies)
	}

	local := httptest.NewRequest(http.MethodPost, "http://localhost:9201/api/auth/login/finish", nil)
	w = httptest.NewRecorder()
	svc.setSessionCookie(w, local, "token")
	cookies = w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("loopback HTTP cookie must remain usable without Secure: %+v", cookies)
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

func TestRepairRevokesPreviousBearerSession(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	svc.SetAdminSecret("test-secret")
	oldBearer, oldSessionID, err := svc.createSessionWithID(context.Background(), admin.ID, "old client", "bearer")
	if err != nil {
		t.Fatalf("create old session: %v", err)
	}
	pairingToken := mintToken(t, svc, "")
	body, _ := json.Marshal(map[string]string{
		"token": pairingToken, "label": "replacement client", "nonce": testPairingNonce,
		"replaceSessionId": oldSessionID,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/auth/pair", bytes.NewReader(body))
	w := httptest.NewRecorder()
	svc.handlePairExchange(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("re-pair status = %d: %s", w.Code, w.Body.String())
	}

	lookup := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	lookup.Header.Set("Authorization", "Bearer "+oldBearer)
	if _, err := svc.authenticateRequest(lookup); err == nil {
		t.Fatal("re-pair left the replaced bearer session valid")
	}
}

func TestBearerCanRevokeItsOwnSession(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	bearer, err := svc.createSession(context.Background(), admin.ID, "client", "bearer")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/session", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("self-revoke status = %d: %s", w.Code, w.Body.String())
	}

	lookup := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	lookup.Header.Set("Authorization", "Bearer "+bearer)
	if _, err := svc.authenticateRequest(lookup); err == nil {
		t.Fatal("self-revoked bearer remained valid")
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

func TestWSTicketMintIsBoundedPerSession(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	bearer, err := svc.createSession(context.Background(), admin.ID, "client", "bearer")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const ticketLimit = 32
	for i := 0; i < ticketLimit; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/ws-ticket", nil)
		r.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		svc.handleCreateWSTicket(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("ticket %d status = %d, body %s", i+1, w.Code, w.Body.String())
		}
	}

	r := httptest.NewRequest(http.MethodPost, "/api/auth/ws-ticket", nil)
	r.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	svc.handleCreateWSTicket(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("overflow ticket status = %d, want 429: %s", w.Code, w.Body.String())
	}
}

func TestWebAuthnCeremoniesAreBounded(t *testing.T) {
	svc, _ := newTestService(t)
	for i := 0; i < 1024; i++ {
		if err := svc.saveCeremony(fmt.Sprintf("login:%d", i), &webauthn.SessionData{}, ""); err != nil {
			t.Fatalf("save ceremony %d: %v", i, err)
		}
	}
	if err := svc.saveCeremony("login:overflow", &webauthn.SessionData{}, ""); err == nil {
		t.Fatal("unbounded WebAuthn ceremony was accepted")
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
