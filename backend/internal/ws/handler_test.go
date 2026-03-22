package ws_test

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/server"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/ws"
)

func setupTestServer(t *testing.T) (*httptest.Server, func()) {
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
	srv := server.New(queries)
	ts := httptest.NewServer(srv)
	cleanup := func() {
		ts.Close()
		db.Close()
	}
	return ts, cleanup
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

func TestWebSocketUpgrade(t *testing.T) {
	ts, cleanup := setupTestServer(t)
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
	ts, cleanup := setupTestServer(t)
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
	ts, cleanup := setupTestServer(t)
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
	ts, cleanup := setupTestServer(t)
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
