package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/server"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/ws"
)

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
	srv, err := server.New(queries, server.Config{})
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
		ID:   "proj-" + name,
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
func insertTestSession(t *testing.T, queries *store.Queries, id, projectID, name, workDir, state string) {
	t.Helper()
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

	payload, _ := json.Marshal(ws.SessionCreatePayload{ProjectID: "nonexistent"})
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

	payload, _ := json.Marshal(ws.SessionQueryPayload{SessionID: "nonexistent", Prompt: "hello"})
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
	createTestProject(t, queries, "testproj", projDir)

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.create", "10",
		ws.SessionCreatePayload{ProjectID: "proj-testproj", Name: "My Session"})

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
	createTestProject(t, queries, "listproj", projDir)

	// Insert sessions directly into DB to avoid needing Claude CLI.
	insertTestSession(t, queries, "sess-1", "proj-listproj", "Session 1", projDir, "idle")
	insertTestSession(t, queries, "sess-2", "proj-listproj", "Session 2", projDir, "running")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.list", "20",
		ws.SessionListPayload{ProjectID: "proj-listproj"})

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
	if result.Sessions[0].ID != "sess-1" {
		t.Fatalf("expected first session ID 'sess-1', got %q", result.Sessions[0].ID)
	}
	if result.Sessions[1].ID != "sess-2" {
		t.Fatalf("expected second session ID 'sess-2', got %q", result.Sessions[1].ID)
	}
	if result.Sessions[0].Name != "Session 1" {
		t.Fatalf("expected first session name 'Session 1', got %q", result.Sessions[0].Name)
	}
}

func TestSessionListEmpty(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	createTestProject(t, queries, "emptyproj", projDir)

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.list", "21",
		ws.SessionListPayload{ProjectID: "proj-emptyproj"})

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
	createTestProject(t, queries, "stopproj", projDir)

	insertTestSession(t, queries, "sess-stop-1", "proj-stopproj", "Session 1", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	// Stop the session.
	resp := sendAndReceive(t, conn, "session.stop", "30",
		ws.SessionStopPayload{SessionID: "sess-stop-1"})

	if resp.Error != nil {
		t.Fatalf("unexpected error stopping session: %s", resp.Error.Message)
	}

	// List sessions and verify the stopped session has state "stopped".
	resp = sendAndReceive(t, conn, "session.list", "31",
		ws.SessionListPayload{ProjectID: "proj-stopproj"})

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

	resp := sendAndReceive(t, conn, "project.subscribe", "40",
		ws.ProjectSubscribePayload{ProjectID: "proj-test"})

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
	createTestProject(t, queries, "multiproj", projDir)

	insertTestSession(t, queries, "multi-1", "proj-multiproj", "Session 1", projDir, "idle")
	insertTestSession(t, queries, "multi-2", "proj-multiproj", "Session 2", projDir, "running")
	insertTestSession(t, queries, "multi-3", "proj-multiproj", "Session 3", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.list", "50",
		ws.SessionListPayload{ProjectID: "proj-multiproj"})

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
	for i, s := range result.Sessions {
		expectedID := []string{"multi-1", "multi-2", "multi-3"}[i]
		if s.ID != expectedID {
			t.Fatalf("session %d: expected ID %q, got %q", i, expectedID, s.ID)
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

func TestTagCRUD(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Create.
	resp := sendAndReceive(t, conn, "tag.create", "70",
		ws.TagCreatePayload{Name: "Bug", Color: "#ff0000"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	created := unmarshalPayload[store.Tag](t, resp)
	if created.ID == "" {
		t.Fatal("expected non-empty tag ID")
	}
	if created.Name != "Bug" {
		t.Fatalf("expected name 'Bug', got %q", created.Name)
	}
	if created.Color != "#ff0000" {
		t.Fatalf("expected color '#ff0000', got %q", created.Color)
	}

	// List — should contain the one tag.
	resp = sendAndReceive(t, conn, "tag.list", "71", struct{}{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	listed := unmarshalPayload[ws.TagListResult](t, resp)
	if len(listed.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(listed.Tags))
	}
	if listed.Tags[0].ID != created.ID {
		t.Fatalf("expected tag ID %q, got %q", created.ID, listed.Tags[0].ID)
	}

	// Update.
	resp = sendAndReceive(t, conn, "tag.update", "72",
		ws.TagUpdatePayload{ID: created.ID, Name: "Feature", Color: "#00ff00"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	updated := unmarshalPayload[store.Tag](t, resp)
	if updated.Name != "Feature" {
		t.Fatalf("expected name 'Feature', got %q", updated.Name)
	}
	if updated.Color != "#00ff00" {
		t.Fatalf("expected color '#00ff00', got %q", updated.Color)
	}

	// Delete.
	resp = sendAndReceive(t, conn, "tag.delete", "73",
		ws.TagDeletePayload{ID: created.ID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	// List — should be empty.
	resp = sendAndReceive(t, conn, "tag.list", "74", struct{}{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	listed = unmarshalPayload[ws.TagListResult](t, resp)
	if len(listed.Tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(listed.Tags))
	}
}

func TestTagListMultiple(t *testing.T) {
	ts, _, cleanup := setupTestServer(t)
	defer cleanup()

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "tag.create", "75",
		ws.TagCreatePayload{Name: "Alpha", Color: "#aaaaaa"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	tag1 := unmarshalPayload[store.Tag](t, resp)

	resp = sendAndReceive(t, conn, "tag.create", "76",
		ws.TagCreatePayload{Name: "Beta", Color: "#bbbbbb"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	tag2 := unmarshalPayload[store.Tag](t, resp)

	resp = sendAndReceive(t, conn, "tag.list", "77", struct{}{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	listed := unmarshalPayload[ws.TagListResult](t, resp)
	if len(listed.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(listed.Tags))
	}
	if listed.Tags[0].ID != tag1.ID {
		t.Fatalf("expected first tag ID %q, got %q", tag1.ID, listed.Tags[0].ID)
	}
	if listed.Tags[1].ID != tag2.ID {
		t.Fatalf("expected second tag ID %q, got %q", tag2.ID, listed.Tags[1].ID)
	}
}

func TestSessionDelete(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	createTestProject(t, queries, "delproj", projDir)
	insertTestSession(t, queries, "sess-del-1", "proj-delproj", "Delete Me", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.delete", "80",
		ws.SessionDeletePayload{SessionID: "sess-del-1"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = sendAndReceive(t, conn, "session.list", "81",
		ws.SessionListPayload{ProjectID: "proj-delproj"})
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
	createTestProject(t, queries, "bulkproj", projDir)
	insertTestSession(t, queries, "sess-bulk-1", "proj-bulkproj", "Bulk 1", projDir, "idle")
	insertTestSession(t, queries, "sess-bulk-2", "proj-bulkproj", "Bulk 2", projDir, "idle")
	insertTestSession(t, queries, "sess-bulk-3", "proj-bulkproj", "Bulk 3", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.delete-bulk", "82",
		ws.SessionDeleteBulkPayload{SessionIDs: []string{"sess-bulk-1", "sess-bulk-2"}})
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
		ws.SessionListPayload{ProjectID: "proj-bulkproj"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	listResult := unmarshalPayload[session.ListSessionsResult](t, resp)
	if len(listResult.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listResult.Sessions))
	}
	if listResult.Sessions[0].ID != "sess-bulk-3" {
		t.Fatalf("expected remaining session 'sess-bulk-3', got %q", listResult.Sessions[0].ID)
	}
}

func TestSessionRename(t *testing.T) {
	ts, queries, cleanup := setupTestServer(t)
	defer cleanup()

	projDir := t.TempDir()
	createTestProject(t, queries, "renproj", projDir)
	insertTestSession(t, queries, "sess-ren-1", "proj-renproj", "Old Name", projDir, "idle")

	conn := dialWS(t, ts)
	defer conn.Close()

	resp := sendAndReceive(t, conn, "session.rename", "84",
		ws.SessionRenamePayload{SessionID: "sess-ren-1", Name: "New Name"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resp = sendAndReceive(t, conn, "session.list", "85",
		ws.SessionListPayload{ProjectID: "proj-renproj"})
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

	cases := []struct {
		name      string
		msgType   string
		id        string
		payload   any
		errSubstr string
	}{
		{"merge/empty-sessionId", "session.merge", "90", ws.SessionMergePayload{SessionID: ""}, "sessionId"},
		{"rename/empty-both", "session.rename", "91", ws.SessionRenamePayload{SessionID: "", Name: ""}, "sessionId"},
		{"rename/empty-name", "session.rename", "92", ws.SessionRenamePayload{SessionID: "x", Name: ""}, "name"},
		{"commit/empty-both", "session.commit", "93", ws.SessionCommitPayload{SessionID: "", Message: ""}, "sessionId"},
		{"commit/empty-message", "session.commit", "94", ws.SessionCommitPayload{SessionID: "x", Message: ""}, "message"},
		{"tag.create/empty-name", "tag.create", "95", ws.TagCreatePayload{Name: ""}, "name"},
		{"tag.update/empty-id", "tag.update", "96", ws.TagUpdatePayload{ID: ""}, "id"},
		{"tag.delete/empty-id", "tag.delete", "97", ws.TagDeletePayload{ID: ""}, "id"},
		{"team.create/empty-projectId", "team.create", "98", ws.TeamCreatePayload{ProjectID: ""}, "projectId"},
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
