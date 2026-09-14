package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialAllowed(t *testing.T) {
	tests := []struct {
		kind, target string
		want         bool
	}{
		{KindPeer, "/api/peer/sessions", true},
		{KindPeer, "/api/peer/events", true},
		{KindPeer, "/api/auth/ws-ticket", false},
		{KindPeer, "/api/auth/session", true},
		{KindPeer, "/api/projects", false},
		{KindPeer, "/api/sessions", false},
		{KindPeer, "/api/machines", false},
		{KindPeer, "/ws", false},
		{KindPeer, "/api/voice/live", false},
		{KindPeer, "/api/auth/sessions", false},
		{KindPeer, "/api/auth/pairing-tokens", false},
		{KindPeer, "/api/auth/peer-credential", false},
		{KindPeer, "/api/peer", false},
		// The prefix test reads the decoded path and the mux does not route it
		// verbatim: an unclean or encoded peer path is refused outright.
		{KindPeer, "/api/peer/../projects", false},
		{KindPeer, "/api/peer/..%2Fprojects", false},
		{KindPeer, "/api/peer/sessions/", false},
		{KindPeer, "/api/peer//sessions", false},
		{"bearer", "/api/projects", true},
		{"bearer", "/api/peer/sessions", false},
		{"bearer", "/api/peer/events", false},
		{"cookie", "/api/peer/sessions", false},
		{"cookie", "/ws", true},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, tt.target, nil)
		if got := credentialAllowed(tt.kind, r); got != tt.want {
			t.Errorf("credentialAllowed(%s, %s) = %v, want %v", tt.kind, tt.target, got, tt.want)
		}
	}
}

func mintPeer(t *testing.T, svc *Service, bearer, body string) (*httptest.ResponseRecorder, string, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/peer-credential", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	svc.handleMintPeerCredential(w, r)
	var resp struct {
		Token     string `json:"token"`
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp.Token, resp.SessionID
}

func authAt(svc *Service, method, target, token string) error {
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	_, err := svc.authenticateRequest(r)
	return err
}

// The whole life of a peer credential: minted by a paired bearer, good only on
// its own routes, unable to mint another, rotated without touching anything
// else, and able to revoke itself.
func TestPeerCredentialLifecycle(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	bearer, err := svc.createSession(context.Background(), admin.ID, "review", "bearer")
	if err != nil {
		t.Fatalf("create bearer: %v", err)
	}

	w, peer, peerID := mintPeer(t, svc, bearer, `{"label":"peer: review"}`)
	if w.Code != http.StatusOK || peer == "" || peerID == "" {
		t.Fatalf("mint = %d %s", w.Code, w.Body.String())
	}

	if err := authAt(svc, http.MethodGet, "/api/peer/sessions", peer); err != nil {
		t.Errorf("peer on its own route: %v", err)
	}
	if err := authAt(svc, http.MethodGet, "/api/projects", peer); err == nil {
		t.Error("peer credential authenticated on /api/projects")
	}
	if err := authAt(svc, http.MethodGet, "/api/peer/sessions", bearer); err == nil {
		t.Error("browser bearer authenticated on the peer surface")
	}

	if w, _, _ := mintPeer(t, svc, peer, `{}`); w.Code != http.StatusUnauthorized {
		t.Errorf("a peer credential minted another: %d", w.Code)
	}

	// Rotation deletes the old peer credential and only that.
	w, rotated, _ := mintPeer(t, svc, bearer, `{"replaceSessionId":"`+peerID+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate = %d %s", w.Code, w.Body.String())
	}
	if err := authAt(svc, http.MethodGet, "/api/peer/sessions", peer); err == nil {
		t.Error("the rotated-out peer credential still authenticates")
	}
	if err := authAt(svc, http.MethodGet, "/api/projects", bearer); err != nil {
		t.Errorf("rotation touched the bearer: %v", err)
	}

	// replaceSessionId never reaches a browser's session.
	bearerRow, err := svc.lookupSession(context.Background(), bearer)
	if err != nil {
		t.Fatalf("lookup bearer: %v", err)
	}
	if w, _, _ := mintPeer(t, svc, bearer, `{"replaceSessionId":"`+bearerRow.ID.String+`"}`); w.Code != http.StatusOK {
		t.Fatalf("mint naming a bearer = %d", w.Code)
	}
	if err := authAt(svc, http.MethodGet, "/api/projects", bearer); err != nil {
		t.Errorf("replaceSessionId deleted a bearer session: %v", err)
	}

	r := httptest.NewRequest(http.MethodDelete, "/api/auth/session", nil)
	r.Header.Set("Authorization", "Bearer "+rotated)
	rec := httptest.NewRecorder()
	svc.handleRevokeCurrentBearer(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("peer self-revoke = %d %s", rec.Code, rec.Body.String())
	}
	if err := authAt(svc, http.MethodGet, "/api/peer/sessions", rotated); err == nil {
		t.Error("revoked peer credential still authenticates")
	}
}

// A peer credential cannot mint a WebSocket ticket, so it can open no socket.
func TestPeerCredentialOpensNoSocket(t *testing.T) {
	svc, queries := newTestService(t)
	admin := createAdminUser(t, queries)
	bearer, _ := svc.createSession(context.Background(), admin.ID, "review", "bearer")
	_, peer, _ := mintPeer(t, svc, bearer, `{}`)

	r := httptest.NewRequest(http.MethodPost, "/api/auth/ws-ticket", nil)
	r.Header.Set("Authorization", "Bearer "+peer)
	w := httptest.NewRecorder()
	svc.handleCreateWSTicket(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("peer ws-ticket mint = %d, want 401", w.Code)
	}
}
