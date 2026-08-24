package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/store"
)

func registerBegin(t *testing.T, svc *Service, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/register/begin", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	svc.handleRegisterBegin(w, r)
	return w
}

// The rekey window (users exist, no credentials) lets anyone re-register by
// display name. Persisting the user row at /begin meant one abandoned first-run
// registration opened that window permanently, with no operator action.
func TestRegisterBeginDoesNotPersistTheUser(t *testing.T) {
	svc, queries := newTestService(t)

	w := registerBegin(t, svc, `{"displayName":"Mathias"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	count, err := queries.CountUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("CountUsers = %d after an unfinished ceremony, want 0", count)
	}

	// The ceremony still carries everything needed to create the user on finish.
	svc.ceremonyMu.Lock()
	defer svc.ceremonyMu.Unlock()
	if len(svc.ceremonies) != 1 {
		t.Fatalf("ceremonies = %d, want 1", len(svc.ceremonies))
	}
	for _, entry := range svc.ceremonies {
		if entry.pending == nil {
			t.Fatal("ceremony has no pending user, so finish could not create one")
		}
		if entry.pending.displayName != "Mathias" || entry.pending.isAdmin != 1 {
			t.Errorf("pending = %+v, want the first user as admin", entry.pending)
		}
	}
}

// An abandoned registration must not leave the server claimable, which is what
// the previous behaviour did: userCount 1, credentialCount 0 => rekey mode.
func TestAbandonedRegistrationLeavesNoRekeyWindow(t *testing.T) {
	svc, queries := newTestService(t)
	registerBegin(t, svc, `{"displayName":"Mathias"}`)

	users, err := queries.CountUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	creds, err := queries.CountWebAuthnCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if users > 0 && creds == 0 {
		t.Errorf("server is in rekey mode after an abandoned registration (users=%d creds=%d)", users, creds)
	}
}

// Rekey mode is reachable only with the one-time code `agentique auth rekey`
// prints. A display name is not a credential — knowing one must get nobody in.
func TestRekeyRequiresARecoveryCode(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries) // no credentials => rekey mode

	for _, body := range []string{
		`{"displayName":"admin"}`,
		`{"displayName":"admin","rekeyCode":"AAAAAAAAAAAA"}`,
		`{"rekeyCode":"short"}`,
		`{}`,
	} {
		w := registerBegin(t, svc, body)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("registerBegin(%s) = %d, want 401: %s", body, w.Code, w.Body.String())
		}
	}

	code, _, err := MintRekeyCode(context.Background(), queries, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := registerBegin(t, svc, `{"rekeyCode":"`+code+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("a valid recovery code = %d, want 200: %s", w.Code, w.Body.String())
	}

	// The ceremony binds to the code's user, not to anything the caller typed.
	svc.ceremonyMu.Lock()
	defer svc.ceremonyMu.Unlock()
	for _, entry := range svc.ceremonies {
		if entry.userID != admin.ID {
			t.Errorf("ceremony user = %s, want %s", entry.userID, admin.ID)
		}
		if entry.pending != nil {
			t.Error("rekey must not create a user — the account already exists")
		}
	}
}

func TestRekeyCodeIsOneTimeAndSurvivesFormatting(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	code, _, err := MintRekeyCode(context.Background(), queries, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Hand-typed codes arrive with dashes and in any case.
	formatted := strings.ToLower(code[:4] + "-" + code[4:8] + "-" + code[8:])
	if w := registerBegin(t, svc, `{"rekeyCode":"`+formatted+`"}`); w.Code != http.StatusOK {
		t.Fatalf("formatted code = %d, want 200: %s", w.Code, w.Body.String())
	}
	if w := registerBegin(t, svc, `{"rekeyCode":"`+code+`"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("reused code = %d, want 401", w.Code)
	}
}

func TestRekeyFailureDoesNotDistinguishCases(t *testing.T) {
	svc, queries := newTestService(t)
	createAdminUser(t, queries)

	// A wrong code and a well-formed-but-unknown one must read identically —
	// otherwise the reply is an oracle for what exists.
	var messages []string
	for _, body := range []string{
		`{"rekeyCode":"AAAAAAAAAAAA"}`,
		`{"rekeyCode":"BBBBBBBBBBBB","displayName":"admin"}`,
		`{"rekeyCode":""}`,
	} {
		w := registerBegin(t, svc, body)
		var parsed struct{ Error string }
		if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, parsed.Error)
	}
	for _, m := range messages[1:] {
		if m != messages[0] {
			t.Errorf("failure messages differ: %q vs %q", messages[0], m)
		}
	}
}

// Adding a second passkey used to create a SECOND, non-admin account rather
// than a second credential on the caller's own account.
func TestAddingAPasskeyBindsToTheCallersAccount(t *testing.T) {
	svc, queries := newTestService(t)
	ctx := context.Background()
	admin := createAdminUser(t, queries)
	// A credential so the flow is past first-user and rekey.
	if err := queries.CreateWebAuthnCredential(ctx, store.CreateWebAuthnCredentialParams{
		ID: "cred-1", UserID: admin.ID, PublicKey: []byte("k"),
	}); err != nil {
		t.Fatal(err)
	}
	token, err := svc.createSession(ctx, admin.ID, "laptop", "cookie")
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/auth/register/begin",
		strings.NewReader(`{"displayName":"a second key"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()
	svc.handleRegisterBegin(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	count, err := queries.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("CountUsers = %d, want 1 — adding a passkey must not create an account", count)
	}
	svc.ceremonyMu.Lock()
	defer svc.ceremonyMu.Unlock()
	for key, entry := range svc.ceremonies {
		if entry.pending != nil {
			t.Errorf("ceremony %s would create a user", key)
		}
		if entry.userID != admin.ID {
			t.Errorf("ceremony user = %s, want the caller %s", entry.userID, admin.ID)
		}
	}
}

// Invite creation authorized itself from the request context, which the auth
// middleware never populates for /api/auth/* — so it always returned 403.
func TestCreateInviteWorksForAnAdmin(t *testing.T) {
	svc, queries := newTestService(t)
	ctx := context.Background()
	admin := createAdminUser(t, queries)
	token, err := svc.createSession(ctx, admin.ID, "laptop", "cookie")
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/auth/invite", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()
	svc.handleCreateInvite(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" {
		t.Fatal("no invite token returned")
	}
	// The plaintext token goes to the operator; only its digest is stored.
	if _, err := queries.GetInviteToken(ctx, hashToken(body.Token)); err != nil {
		t.Errorf("invite token was not persisted: %v", err)
	}
}

func TestCreateInviteRefusesAnUnauthenticatedCaller(t *testing.T) {
	svc, queries := newTestService(t)
	createAdminUser(t, queries)

	w := httptest.NewRecorder()
	svc.handleCreateInvite(w, httptest.NewRequest(http.MethodPost, "/api/auth/invite", nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}
