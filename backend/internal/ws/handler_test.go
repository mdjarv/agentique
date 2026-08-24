package ws_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/allbin/agentkit/eventbus"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/auth"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/ws"
)

func newID() string { return uuid.New().String() }

func setupTestServer(t *testing.T) (*httptest.Server, *store.Queries, func()) {
	t.Helper()
	ts, _, queries, cleanup := setupTestServerWithDB(t)
	return ts, queries, cleanup
}

// setupTestServerWithDB is setupTestServer plus the raw *sql.DB handle, for
// tests that need direct SQL (e.g. backdating created_at defaults).
func setupTestServerWithDB(t *testing.T) (*httptest.Server, *sql.DB, *store.Queries, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	queries := store.New(db)
	srv, err := server.New(queries, server.Config{DB: db})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	ts := httptest.NewServer(srv)
	cleanup := func() {
		srv.Shutdown()
		ts.Close()
		db.Close()
	}
	return ts, db, queries, cleanup
}

func setupAuthenticatedWSServer(t *testing.T) (*httptest.Server, *store.Queries, func()) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		db.Close()
		t.Fatalf("run migrations: %v", err)
	}
	queries := store.New(db)
	srv, err := server.New(queries, server.Config{
		AuthEnabled: true,
		RPID:        "localhost",
		RPOrigins:   []string{"http://trusted.example"},
		AdminSecret: "test-admin-secret",
		DB:          db,
	})
	if err != nil {
		db.Close()
		t.Fatalf("create server: %v", err)
	}
	ts := httptest.NewServer(srv)
	return ts, queries, func() {
		srv.Shutdown()
		ts.Close()
		db.Close()
	}
}

func dialWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial WebSocket: %v", err)
	}
	return conn
}

// createTestProject inserts a project directly into the DB for testing.
func createTestProject(t *testing.T, queries *store.Queries, name, path string) store.Project {
	t.Helper()
	p, err := queries.CreateProject(context.Background(), store.CreateProjectParams{
		ID:   newID(),
		Name: name,
		Path: path,
	})
	if err != nil {
		t.Fatalf("failed to create test project: %v", err)
	}
	return p
}

// insertTestSession inserts a session record directly into the DB for testing
// without needing to connect to Claude CLI.
func insertTestSession(t *testing.T, queries *store.Queries, projectID, name, workDir, state string) string {
	t.Helper()
	id := newID()
	_, err := queries.CreateSession(context.Background(), store.CreateSessionParams{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
		WorkDir:   workDir,
		State:     state,
	})
	if err != nil {
		t.Fatalf("failed to insert test session: %v", err)
	}
	return id
}

// sendAndReceive sends a WS message and reads the response.
// It skips over server push messages (which don't have an ID matching the request)
// and returns the first response that matches the request ID.
func sendAndReceive(t *testing.T, conn *websocket.Conn, msgType string, id string, payload any) ws.ServerResponse {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	msg := ws.ClientMessage{
		ID:      id,
		Type:    msgType,
		Payload: raw,
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	// Read messages until we get the response matching our request ID.
	// Push messages (session.state, session.event) may arrive before the response.
	for {
		var raw json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			t.Fatalf("read error: %v", err)
		}

		// Try to parse as ServerResponse first.
		var resp ws.ServerResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// ServerResponse has Type "response" and a matching ID.
		if resp.Type == "response" && resp.ID == id {
			return resp
		}
		// Otherwise it's a push message -- skip it.
	}
}

func TestWebSocketUpgrade(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Send unknown message type.
	msg := ws.ClientMessage{
		ID:      "1",
		Type:    "unknown",
		Payload: json.RawMessage("{}"),
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	var resp ws.ServerResponse
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read error: %v", err)
	}

	if resp.ID != "1" {
		t.Fatalf("expected id '1', got %q", resp.ID)
	}
	if resp.Error == nil {
		t.Fatal("expected error response for unknown type")
	}
	if !strings.Contains(resp.Error.Message, "unknown") {
		t.Fatalf("expected error about unknown type, got %q", resp.Error.Message)
	}
}

func TestAuthDisabledWebSocketRejectsForeignOrigin(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	headers := http.Header{"Origin": []string{"http://evil.example"}}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("foreign-origin WebSocket unexpectedly connected")
	}
	if resp == nil {
		t.Fatalf("upgrade failed without an HTTP response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestWebSocketConnectionLimit(t *testing.T) {
	handler := &ws.Handler{Bus: eventbus.New(), MaxConnections: 1}
	ts := httptest.NewServer(handler)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial first socket: %v", err)
	}
	defer first.Close()

	second, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if second != nil {
		second.Close()
	}
	if err == nil {
		t.Fatal("second socket exceeded the connection limit")
	}
	if resp == nil {
		t.Fatalf("second dial failed without response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestWebSocketRejectsMessageOverConfiguredLimit(t *testing.T) {
	handler := &ws.Handler{Bus: eventbus.New(), MaxMessageBytes: 1024}
	ts := httptest.NewServer(handler)
	defer ts.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

	prefix := []byte(`{"id":"large","type":"unknown","payload":"`)
	suffix := []byte(`"}`)
	message := make([]byte, 0, 2048)
	message = append(message, prefix...)
	message = append(message, bytes.Repeat([]byte("x"), 2048-len(prefix)-len(suffix))...)
	message = append(message, suffix...)
	if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
		t.Fatalf("write oversized message: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("oversized message was dispatched")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("server left socket open after oversized message: %v", err)
	}
}

func TestRevokingSessionClosesEstablishedWebSocket(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedWSServer(t)
	defer cleanup()

	userID := newID()
	if _, err := queries.CreateUser(context.Background(), store.CreateUserParams{
		ID: userID, DisplayName: "operator", IsAdmin: 1,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	const bearer = "paired-bearer-token"
	const sessionID = "paired-session-id"
	if err := queries.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		TokenHash: auth.HashToken(bearer),
		ID:        sql.NullString{String: sessionID, Valid: true}, UserID: userID,
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		Label:     "desktop", Kind: "bearer",
	}); err != nil {
		t.Fatalf("create auth session: %v", err)
	}

	ticketReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/ws-ticket", nil)
	if err != nil {
		t.Fatalf("create ticket request: %v", err)
	}
	ticketReq.Header.Set("Authorization", "Bearer "+bearer)
	ticketResp, err := http.DefaultClient.Do(ticketReq)
	if err != nil {
		t.Fatalf("mint ticket: %v", err)
	}
	defer ticketResp.Body.Close()
	if ticketResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(ticketResp.Body)
		t.Fatalf("ticket status = %d: %s", ticketResp.StatusCode, body)
	}
	var ticketBody struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(ticketResp.Body).Decode(&ticketBody); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?wsTicket=" + ticketBody.Ticket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"http://primary.example"}})
	if err != nil {
		t.Fatalf("dial authenticated WebSocket: %v", err)
	}
	defer conn.Close()

	revokeReq, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/sessions/"+sessionID, nil)
	if err != nil {
		t.Fatalf("create revoke request: %v", err)
	}
	revokeReq.Header.Set("X-Agentique-Admin-Secret", "test-admin-secret")
	revokeResp, err := http.DefaultClient.Do(revokeReq)
	if err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	defer revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(revokeResp.Body)
		t.Fatalf("revoke status = %d: %s", revokeResp.StatusCode, body)
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("socket remained readable after revocation")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("socket remained open after revocation: %v", err)
	}
}

func TestExpiredSessionClosesEstablishedWebSocket(t *testing.T) {
	ts, queries, cleanup := setupAuthenticatedWSServer(t)
	defer cleanup()

	userID := newID()
	if _, err := queries.CreateUser(context.Background(), store.CreateUserParams{
		ID: userID, DisplayName: "operator", IsAdmin: 1,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	const bearer = "short-lived-bearer-token"
	if err := queries.CreateAuthSession(context.Background(), store.CreateAuthSessionParams{
		TokenHash: auth.HashToken(bearer),
		ID:        sql.NullString{String: "short-lived-session", Valid: true}, UserID: userID,
		ExpiresAt: time.Now().Add(3 * time.Second).UTC().Format(time.RFC3339),
		Label:     "desktop", Kind: "bearer",
	}); err != nil {
		t.Fatalf("create auth session: %v", err)
	}

	ticketReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/ws-ticket", nil)
	if err != nil {
		t.Fatalf("create ticket request: %v", err)
	}
	ticketReq.Header.Set("Authorization", "Bearer "+bearer)
	ticketResp, err := http.DefaultClient.Do(ticketReq)
	if err != nil {
		t.Fatalf("mint ticket: %v", err)
	}
	defer ticketResp.Body.Close()
	var ticketBody struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(ticketResp.Body).Decode(&ticketBody); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	if ticketResp.StatusCode != http.StatusOK || ticketBody.Ticket == "" {
		t.Fatalf("ticket status/body = %d %+v", ticketResp.StatusCode, ticketBody)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?wsTicket=" + ticketBody.Ticket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"http://primary.example"}})
	if err != nil {
		t.Fatalf("dial authenticated WebSocket: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("socket remained readable after session expiry")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("socket remained open after session expiry: %v", err)
	}
}

func TestSessionCreateRequiresValidProject(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Valid UUID but nonexistent project.
	nonexistentID := newID()
	payload, _ := json.Marshal(ws.SessionCreatePayload{ProjectID: nonexistentID})
	msg := ws.ClientMessage{
		ID:      "2",
		Type:    "session.create",
		Payload: payload,
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	var resp ws.ServerResponse
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read error: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent project")
	}
	if !strings.Contains(resp.Error.Message, "not found") {
		t.Fatalf("expected 'not found' error, got %q", resp.Error.Message)
	}
}

func TestSessionCreateRequiresProjectID(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	payload, _ := json.Marshal(ws.SessionCreatePayload{ProjectID: ""})
	msg := ws.ClientMessage{
		ID:      "3",
		Type:    "session.create",
		Payload: payload,
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	var resp ws.ServerResponse
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read error: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for empty projectId")
	}
}

func TestSessionQueryRequiresSession(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Valid UUID but nonexistent session.
	nonexistentID := newID()
	payload, _ := json.Marshal(ws.SessionQueryPayload{SessionID: nonexistentID, Prompt: "hello"})
	msg := ws.ClientMessage{
		ID:      "4",
		Type:    "session.query",
		Payload: payload,
	}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write error: %v", err)
	}

	var resp ws.ServerResponse
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("read error: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSessionCreateProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test that connects to Claude CLI in short mode")
	}
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "testproj", projDir)

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.create", "10",
		ws.SessionCreatePayload{ProjectID: proj.ID, Name: "My Session"})

	if resp.Error != nil {
		// CLI not available -- verify error message is sensible.
		if !strings.Contains(resp.Error.Message, "failed to create session") {
			t.Fatalf("expected 'failed to create session' error, got %q", resp.Error.Message)
		}
	} else {
		// CLI is available -- verify response has session info.
		raw, _ := json.Marshal(resp.Payload)
		var result session.CreateSessionResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if result.SessionID == "" {
			t.Fatal("expected non-empty session ID")
		}
		if result.Name != "My Session" {
			t.Fatalf("expected name 'My Session', got %q", result.Name)
		}
	}
}

func TestSessionList(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "listproj", projDir)

	// Insert sessions directly into DB to avoid needing Claude CLI.
	sess1 := insertTestSession(t, queries, proj.ID, "Session 1", projDir, "idle")
	sess2 := insertTestSession(t, queries, proj.ID, "Session 2", projDir, "running")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.list", "20",
		ws.SessionListPayload{ProjectID: proj.ID})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	// Parse the result.
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var result session.ListSessionsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(result.Sessions))
	}
	if result.Sessions[0].ID != sess1 {
		t.Fatalf("expected first session ID %q, got %q", sess1, result.Sessions[0].ID)
	}
	if result.Sessions[1].ID != sess2 {
		t.Fatalf("expected second session ID %q, got %q", sess2, result.Sessions[1].ID)
	}
	if result.Sessions[0].Name != "Session 1" {
		t.Fatalf("expected first session name 'Session 1', got %q", result.Sessions[0].Name)
	}
}

func TestSessionListEmpty(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "emptyproj", projDir)

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.list", "21",
		ws.SessionListPayload{ProjectID: proj.ID})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	raw, _ := json.Marshal(resp.Payload)
	var result session.ListSessionsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(result.Sessions))
	}
}

func TestSessionListRequiresProjectID(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.list", "22",
		ws.SessionListPayload{ProjectID: ""})

	if resp.Error == nil {
		t.Fatal("expected error for empty projectId")
	}
}

func TestSessionStop(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "stopproj", projDir)

	sessID := insertTestSession(t, queries, proj.ID, "Session 1", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	// Stop the session.
	resp := sendAndReceive(t, conn, "session.stop", "30",
		ws.SessionStopPayload{SessionID: sessID})

	if resp.Error != nil {
		t.Fatalf("unexpected error stopping session: %s", resp.Error.Message)
	}

	// List sessions and verify the stopped session has state "stopped".
	resp = sendAndReceive(t, conn, "session.list", "31",
		ws.SessionListPayload{ProjectID: proj.ID})

	if resp.Error != nil {
		t.Fatalf("unexpected error listing sessions: %s", resp.Error.Message)
	}

	raw, _ := json.Marshal(resp.Payload)
	var result session.ListSessionsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result.Sessions))
	}
	if result.Sessions[0].State != "stopped" {
		t.Fatalf("expected state 'stopped', got %q", result.Sessions[0].State)
	}
}

func TestSessionStopRequiresSessionID(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.stop", "32",
		ws.SessionStopPayload{SessionID: ""})

	if resp.Error == nil {
		t.Fatal("expected error for empty sessionId")
	}
}

func TestProjectSubscribe(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Valid UUID — subscribe doesn't require project to exist in DB.
	resp := sendAndReceive(t, conn, "project.subscribe", "40",
		ws.ProjectSubscribePayload{ProjectID: newID()})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestProjectSubscribeRequiresProjectID(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "project.subscribe", "41",
		ws.ProjectSubscribePayload{ProjectID: ""})

	if resp.Error == nil {
		t.Fatal("expected error for empty projectId")
	}
}

func TestMultipleSessions(t *testing.T) {
	// Test that multiple sessions can be created for the same project (DB records).
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "multiproj", projDir)

	sess1 := insertTestSession(t, queries, proj.ID, "Session 1", projDir, "idle")
	sess2 := insertTestSession(t, queries, proj.ID, "Session 2", projDir, "running")
	sess3 := insertTestSession(t, queries, proj.ID, "Session 3", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.list", "50",
		ws.SessionListPayload{ProjectID: proj.ID})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	raw, _ := json.Marshal(resp.Payload)
	var result session.ListSessionsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(result.Sessions))
	}

	// Verify sessions are ordered by created_at ASC.
	expectedIDs := []string{sess1, sess2, sess3}
	for i, s := range result.Sessions {
		if s.ID != expectedIDs[i] {
			t.Fatalf("session %d: expected ID %q, got %q", i, expectedIDs[i], s.ID)
		}
	}
}

func unmarshalPayload[T any](t *testing.T, resp ws.ServerResponse) T {
	t.Helper()
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return result
}

func TestSessionDelete(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "delproj", projDir)
	sessID := insertTestSession(t, queries, proj.ID, "Delete Me", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.delete", "80",
		ws.SessionDeletePayload{SessionID: sessID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = sendAndReceive(t, conn, "session.list", "81",
		ws.SessionListPayload{ProjectID: proj.ID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	result := unmarshalPayload[session.ListSessionsResult](t, resp)
	if len(result.Sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(result.Sessions))
	}
}

func TestSessionDeleteBulk(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "bulkproj", projDir)
	sess1 := insertTestSession(t, queries, proj.ID, "Bulk 1", projDir, "idle")
	sess2 := insertTestSession(t, queries, proj.ID, "Bulk 2", projDir, "idle")
	_ = insertTestSession(t, queries, proj.ID, "Bulk 3", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.delete-bulk", "82",
		ws.SessionDeleteBulkPayload{SessionIDs: []string{sess1, sess2}})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	bulkResult := unmarshalPayload[ws.SessionDeleteBulkResult](t, resp)
	if len(bulkResult.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(bulkResult.Results))
	}
	for i, r := range bulkResult.Results {
		if !r.Success {
			t.Fatalf("result %d: expected success, got error %q", i, r.Error)
		}
	}

	resp = sendAndReceive(t, conn, "session.list", "83",
		ws.SessionListPayload{ProjectID: proj.ID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	listResult := unmarshalPayload[session.ListSessionsResult](t, resp)
	if len(listResult.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listResult.Sessions))
	}
}

func TestSessionRename(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "renproj", projDir)
	sessID := insertTestSession(t, queries, proj.ID, "Old Name", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.rename", "84",
		ws.SessionRenamePayload{SessionID: sessID, Name: "New Name"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = sendAndReceive(t, conn, "session.list", "85",
		ws.SessionListPayload{ProjectID: proj.ID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	result := unmarshalPayload[session.ListSessionsResult](t, resp)
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result.Sessions))
	}
	if result.Sessions[0].Name != "New Name" {
		t.Fatalf("expected name 'New Name', got %q", result.Sessions[0].Name)
	}
}

func TestSessionSetPinned(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "pinproj", projDir)
	sessID := insertTestSession(t, queries, proj.ID, "Pin Me", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.set-pinned", "86",
		ws.SessionSetPinnedPayload{SessionID: sessID, Pinned: true, PinOrder: 3})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = sendAndReceive(t, conn, "session.list", "87",
		ws.SessionListPayload{ProjectID: proj.ID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	result := unmarshalPayload[session.ListSessionsResult](t, resp)
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result.Sessions))
	}
	if !result.Sessions[0].Pinned {
		t.Fatal("expected session to be pinned")
	}
	if result.Sessions[0].PinOrder != 3 {
		t.Fatalf("expected pinOrder 3, got %d", result.Sessions[0].PinOrder)
	}

	resp = sendAndReceive(t, conn, "session.set-pinned", "88",
		ws.SessionSetPinnedPayload{SessionID: sessID, Pinned: false, PinOrder: 0})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = sendAndReceive(t, conn, "session.list", "89",
		ws.SessionListPayload{ProjectID: proj.ID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	result = unmarshalPayload[session.ListSessionsResult](t, resp)
	if result.Sessions[0].Pinned {
		t.Fatal("expected session to be unpinned")
	}
	if result.Sessions[0].PinOrder != 0 {
		t.Fatalf("expected pinOrder 0, got %d", result.Sessions[0].PinOrder)
	}
}

func TestSessionUnarchive(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	proj := createTestProject(t, queries, "unmarkproj", projDir)
	sessID := insertTestSession(t, queries, proj.ID, "Archived", projDir, "done")
	if err := queries.SetSessionArchived(context.Background(), sessID); err != nil {
		t.Fatalf("set completed: %v", err)
	}

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.unarchive", "95",
		ws.SessionUnarchivePayload{SessionID: sessID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = sendAndReceive(t, conn, "session.list", "96",
		ws.SessionListPayload{ProjectID: proj.ID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	result := unmarshalPayload[session.ListSessionsResult](t, resp)
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(result.Sessions))
	}
	if result.Sessions[0].ArchivedAt != "" {
		t.Fatalf("expected empty archivedAt, got %q", result.Sessions[0].ArchivedAt)
	}
}

func TestHandlerValidation(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	validID := newID()
	cases := []struct {
		name      string
		msgType   string
		id        string
		payload   any
		errSubstr string
	}{
		{"merge/empty-sessionId", "session.merge", "90", ws.SessionMergePayload{SessionID: ""}, "sessionId"},
		{"rename/empty-both", "session.rename", "91", ws.SessionRenamePayload{SessionID: "", Name: ""}, "sessionId"},
		{"rename/empty-name", "session.rename", "92", ws.SessionRenamePayload{SessionID: validID, Name: ""}, "name"},
		{"commit/empty-both", "session.commit", "93", ws.SessionCommitPayload{SessionID: "", Message: ""}, "sessionId"},
		{"commit/empty-message", "session.commit", "94", ws.SessionCommitPayload{SessionID: validID, Message: ""}, "message"},
		{"set-pinned/empty-sessionId", "session.set-pinned", "95", ws.SessionSetPinnedPayload{SessionID: "", Pinned: true}, "sessionId"},
		{"unarchive/empty-sessionId", "session.unarchive", "96", ws.SessionUnarchivePayload{SessionID: ""}, "sessionId"},
		{"channel.create/empty-projectId", "channel.create", "98", ws.ChannelCreatePayload{ProjectID: ""}, "projectId"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := sendAndReceive(t, conn, tc.msgType, tc.id, tc.payload)
			if resp.Error == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(resp.Error.Message, tc.errSubstr) {
				t.Fatalf("expected error containing %q, got %q", tc.errSubstr, resp.Error.Message)
			}
		})
	}
}

func TestSessionQueryRequiresFields(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Missing both fields.
	resp := sendAndReceive(t, conn, "session.query", "60",
		ws.SessionQueryPayload{SessionID: "", Prompt: ""})
	if resp.Error == nil {
		t.Fatal("expected error for empty fields")
	}

	// Missing prompt.
	resp = sendAndReceive(t, conn, "session.query", "61",
		ws.SessionQueryPayload{SessionID: "something", Prompt: ""})
	if resp.Error == nil {
		t.Fatal("expected error for empty prompt")
	}
}

// wireListItems sends a wire.list request and decodes the result.
func wireListItems(t *testing.T, conn *websocket.Conn, id string, payload ws.WireListPayload) []session.ActivityItem {
	t.Helper()
	resp := sendAndReceive(t, conn, "wire.list", id, payload)
	if resp.Error != nil {
		t.Fatalf("wire.list error: %s", resp.Error.Message)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var items []session.ActivityItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("unmarshal items: %v", err)
	}
	return items
}

func TestWireList(t *testing.T) {
	ts, db, queries, cleanup := setupTestServerWithDB(t)
	defer cleanup()
	ctx := context.Background()

	// Two projects with distinct slugs.
	projA, err := queries.CreateProject(ctx, store.CreateProjectParams{
		ID: newID(), Name: "Wire A", Path: t.TempDir(), Slug: "wire-a",
	})
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projB, err := queries.CreateProject(ctx, store.CreateProjectParams{
		ID: newID(), Name: "Wire B", Path: t.TempDir(), Slug: "wire-b",
	})
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}

	sessA := insertTestSession(t, queries, projA.ID, "Sess A", projA.Path, "idle")
	sessB := insertTestSession(t, queries, projB.ID, "Sess B", projB.Path, "idle")

	// tool_use event in project A.
	if err := queries.InsertEvent(ctx, store.InsertEventParams{
		SessionID: sessA, TurnIndex: 0, Seq: 0, Type: "tool_use",
		Data: `{"toolName":"Edit","category":"file_write","toolInput":{"file_path":"/tmp/x.go"}}`,
	}); err != nil {
		t.Fatalf("insert tool_use event: %v", err)
	}
	// error event in project B.
	if err := queries.InsertEvent(ctx, store.InsertEventParams{
		SessionID: sessB, TurnIndex: 0, Seq: 0, Type: "error",
		Data: `{"content":"boom"}`,
	}); err != nil {
		t.Fatalf("insert error event: %v", err)
	}
	// Old event in project A — backdated far outside any hours window.
	if err := queries.InsertEvent(ctx, store.InsertEventParams{
		SessionID: sessA, TurnIndex: 0, Seq: 1, Type: "tool_use",
		Data: `{"toolName":"OldTool","category":"execution"}`,
	}); err != nil {
		t.Fatalf("insert old event: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE session_events SET created_at = '2000-01-01T00:00:00.000'
		 WHERE json_extract(data, '$.toolName') = 'OldTool'`,
	); err != nil {
		t.Fatalf("backdate old event: %v", err)
	}

	// Channel message in project B.
	ch, err := queries.CreateChannel(ctx, store.CreateChannelParams{
		ID: newID(), Name: "general",
		ProjectID: sql.NullString{String: projB.ID, Valid: true},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := queries.InsertMessage(ctx, store.InsertMessageParams{
		ID: newID(), ChannelID: ch.ID, SenderType: "user", SenderID: "u1",
		SenderName: "mathias", Content: "hello wire", MessageType: "message",
		Metadata: "{}",
	}); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	conn := dialWS(t, ts)
	defer conn.Close()

	items := wireListItems(t, conn, "70", ws.WireListPayload{Hours: 48, Limit: 200})

	// Merged across projects, hours filter excludes the backdated row.
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d: %+v", len(items), items)
	}

	// Newest-first ordering (non-strict: same-millisecond inserts tie).
	for i := 1; i < len(items); i++ {
		if items[i-1].CreatedAt < items[i].CreatedAt {
			t.Fatalf("items not newest-first at %d: %q < %q", i, items[i-1].CreatedAt, items[i].CreatedAt)
		}
	}

	byContent := make(map[string]session.ActivityItem, len(items))
	for _, it := range items {
		byContent[it.Content] = it
	}

	toolUse, ok := byContent["Edit"]
	if !ok {
		t.Fatalf("missing tool_use item, got %+v", items)
	}
	if toolUse.Kind != "event" || toolUse.EventType != "tool_use" {
		t.Fatalf("tool_use item wrong shape: %+v", toolUse)
	}
	if toolUse.ProjectSlug != "wire-a" {
		t.Fatalf("expected tool_use projectSlug wire-a, got %q", toolUse.ProjectSlug)
	}
	if toolUse.Category != "file_write" || toolUse.FilePath != "/tmp/x.go" {
		t.Fatalf("tool_use JSON extraction wrong: %+v", toolUse)
	}
	if toolUse.SourceID != sessA || toolUse.SourceName != "Sess A" {
		t.Fatalf("tool_use source wrong: %+v", toolUse)
	}

	errItem, ok := byContent["boom"]
	if !ok {
		t.Fatalf("missing error item, got %+v", items)
	}
	if errItem.Kind != "event" || errItem.EventType != "error" {
		t.Fatalf("error item wrong shape: %+v", errItem)
	}
	if errItem.ProjectSlug != "wire-b" {
		t.Fatalf("expected error projectSlug wire-b, got %q", errItem.ProjectSlug)
	}

	msgItem, ok := byContent["hello wire"]
	if !ok {
		t.Fatalf("missing message item, got %+v", items)
	}
	if msgItem.Kind != "message" || msgItem.SourceID != ch.ID || msgItem.SourceName != "mathias" {
		t.Fatalf("message item wrong shape: %+v", msgItem)
	}
	if msgItem.ProjectSlug != "wire-b" {
		t.Fatalf("expected message projectSlug wire-b, got %q", msgItem.ProjectSlug)
	}

	// Limit is respected.
	limited := wireListItems(t, conn, "71", ws.WireListPayload{Hours: 48, Limit: 2})
	if len(limited) != 2 {
		t.Fatalf("expected 2 items with limit 2, got %d", len(limited))
	}

	// Defaulted payload (zero values clamp to 48h/200) still sees all rows.
	defaulted := wireListItems(t, conn, "72", ws.WireListPayload{})
	if len(defaulted) != 3 {
		t.Fatalf("expected 3 items with defaults, got %d", len(defaulted))
	}
}

func TestWireListPayloadValidate(t *testing.T) {
	cases := []struct {
		name      string
		in        ws.WireListPayload
		wantHours int64
		wantLimit int64
	}{
		{"zero defaults", ws.WireListPayload{}, 48, 200},
		{"negative defaults", ws.WireListPayload{Hours: -5, Limit: -1}, 48, 200},
		{"caps", ws.WireListPayload{Hours: 1000, Limit: 1000}, 168, 500},
		{"in range unchanged", ws.WireListPayload{Hours: 24, Limit: 100}, 24, 100},
		{"boundary unchanged", ws.WireListPayload{Hours: 168, Limit: 500}, 168, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.in
			if err := p.Validate(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Hours != tc.wantHours || p.Limit != tc.wantLimit {
				t.Fatalf("got {hours:%d limit:%d}, want {hours:%d limit:%d}",
					p.Hours, p.Limit, tc.wantHours, tc.wantLimit)
			}
		})
	}
}

func TestSessionQueryPayloadBoundsAttachments(t *testing.T) {
	payload := ws.SessionQueryPayload{
		SessionID: newID(),
		Prompt:    "inspect these",
		Attachments: []session.QueryAttachment{
			{Name: "huge.png", MimeType: "image/png", DataUrl: "data:image/png;base64," + strings.Repeat("A", 8<<20)},
		},
	}
	if err := payload.Validate(); err == nil {
		t.Fatal("oversized attachment was accepted")
	}

	payload.Attachments = make([]session.QueryAttachment, 5)
	for i := range payload.Attachments {
		payload.Attachments[i] = session.QueryAttachment{
			Name: "small.png", MimeType: "image/png", DataUrl: "data:image/png;base64,QQ==",
		}
	}
	if err := payload.Validate(); err == nil {
		t.Fatal("more than four attachments were accepted")
	}
}
