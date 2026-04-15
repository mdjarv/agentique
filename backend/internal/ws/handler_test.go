package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/server"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/ws"
)

func newID() string { return uuid.New().String() }

func setupTestServer(t *testing.T) (*httptest.Server, *store.Queries, func()) {
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
	return ts, queries, cleanup
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
