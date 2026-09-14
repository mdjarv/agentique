package server_test

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// peerRouteMatrix is every route family a peer credential must be refused on.
// It is explicit on purpose (docs/peers.md, Credential): adding a route to the
// server means adding it here, which is the moment to ask whether a paired
// server should reach it. The scope rule is a prefix check, so this list is the
// regression net around it rather than the rule itself.
var peerRouteMatrix = []struct{ method, path string }{
	{"GET", "/api/projects"},
	{"POST", "/api/projects"},
	{"PATCH", "/api/projects/00000000-0000-4000-8000-000000000001"},
	{"DELETE", "/api/projects/00000000-0000-4000-8000-000000000001"},
	{"GET", "/api/projects/00000000-0000-4000-8000-000000000001/files"},
	{"GET", "/api/projects/00000000-0000-4000-8000-000000000001/files/content"},
	{"GET", "/api/sessions"},
	{"GET", "/api/sessions/events"},
	{"GET", "/api/sessions/00000000-0000-4000-8000-000000000001"},
	{"DELETE", "/api/sessions/00000000-0000-4000-8000-000000000001"},
	{"GET", "/api/sessions/00000000-0000-4000-8000-000000000001/history"},
	{"GET", "/api/sessions/00000000-0000-4000-8000-000000000001/model"},
	{"GET", "/api/sessions/00000000-0000-4000-8000-000000000001/files/x.txt"},
	{"GET", "/api/sessions/00000000-0000-4000-8000-000000000001/events/1/images/0"},
	{"POST", "/api/sessions/00000000-0000-4000-8000-000000000001/query"},
	{"POST", "/api/sessions/00000000-0000-4000-8000-000000000001/stop"},
	{"GET", "/api/machines"},
	{"GET", "/api/machines/discover"},
	{"PUT", "/api/machines/00000000-0000-4000-8000-000000000001"},
	{"DELETE", "/api/machines/00000000-0000-4000-8000-000000000001"},
	{"PATCH", "/api/machines/00000000-0000-4000-8000-000000000001/presentation"},
	{"PUT", "/api/machine/presentation"},
	{"GET", "/api/filesystem/browse"},
	{"GET", "/api/filesystem/validate"},
	{"GET", "/api/templates"},
	{"POST", "/api/templates"},
	{"GET", "/api/preset-definitions"},
	{"GET", "/api/storage/disk"},
	{"GET", "/api/storage/usage"},
	{"POST", "/api/storage/reclaim"},
	{"POST", "/api/storage/backups/trim"},
	{"DELETE", "/api/storage/worktrees"},
	{"DELETE", "/api/storage/scratchpads"},
	{"GET", "/api/usage"},
	{"GET", "/api/update/status"},
	{"POST", "/api/update/apply"},
	{"GET", "/api/claude-account"},
	{"POST", "/api/claude-account/logout"},
	{"GET", "/api/voice/settings"},
	{"PATCH", "/api/user/preferences"},
	{"GET", "/api/auth/sessions"},
	{"DELETE", "/api/auth/sessions/some-id"},
	{"POST", "/api/auth/pairing-tokens"},
	{"POST", "/api/auth/peer-credential"},
	{"POST", "/api/auth/invite"},
	{"GET", "/ws"},
	{"GET", "/api/voice/live"},
	// Spelled to slip past a prefix test into another family.
	{"GET", "/api/peer/../projects"},
	{"GET", "/api/peer/..%2Fprojects"},
}

func TestPeerCredentialIsRefusedOutsideThePeerSurface(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()

	createCookieSession(t, queries, true)
	peer := "test-peer-token"
	if err := queries.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		TokenHash: auth.HashToken(peer),
		ID:        sql.NullString{String: "test-peer-id", Valid: true},
		UserID:    "00000000-0000-4000-8000-000000000001",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		Kind:      auth.KindPeer,
	}); err != nil {
		t.Fatalf("create peer session: %v", err)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, route := range peerRouteMatrix {
		body := strings.NewReader("{}")
		req, err := http.NewRequest(route.method, ts.URL+route.path, body)
		if err != nil {
			t.Fatalf("%s %s: %v", route.method, route.path, err)
		}
		req.Header.Set("Authorization", "Bearer "+peer)
		req.Header.Set("Content-Type", "application/json")
		if route.path == "/ws" || route.path == "/api/voice/live" {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", route.method, route.path, err)
		}
		resp.Body.Close()
		// 403 is how the invite handler words its own refusal.
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with a peer credential = %d, want 401 or 403", route.method, route.path, resp.StatusCode)
		}
	}
}

// The surface is mounted on a server with every feature flag off, admits a peer
// credential, and refuses a browser's.
func TestPeerSurfaceIsMountedAndScoped(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedTestServer(t)
	defer cleanup()

	browser := createCookieSession(t, queries, true)
	peer := "test-peer-token"
	if err := queries.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		TokenHash: auth.HashToken(peer),
		ID:        sql.NullString{String: "test-peer-id", Valid: true},
		UserID:    "00000000-0000-4000-8000-000000000001",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		Kind:      auth.KindPeer,
	}); err != nil {
		t.Fatalf("create peer session: %v", err)
	}

	get := func(token string) int {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/peer/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(peer); code != http.StatusOK {
		t.Errorf("peer credential on the peer surface = %d, want 200", code)
	}
	if code := get(browser); code != http.StatusUnauthorized {
		t.Errorf("browser credential on the peer surface = %d, want 401", code)
	}

	// Actions are off by default: a send is refused by the owner's opt-in.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/peer/sessions/00000000-0000-4000-8000-0000000000aa/send",
		strings.NewReader(`{"prompt":"x"}`))
	req.Header.Set("Authorization", "Bearer "+peer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("send with actions off = %d, want 403", resp.StatusCode)
	}
}
