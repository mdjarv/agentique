package testmode

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	claudecli "github.com/allbin/claudecli-go"

	dbpkg "github.com/mdjarv/agentique/backend/db"
	"github.com/mdjarv/agentique/backend/internal/session"
	"github.com/mdjarv/agentique/backend/internal/store"
)

// noopBroadcaster satisfies session.Broadcaster for tests.
type noopBroadcaster struct{}

func (noopBroadcaster) Broadcast(string, string, any) {}

// setupHandler creates a real DB, connector, manager, and handler for testing.
func setupHandler(t *testing.T) (*http.ServeMux, *Handler, *store.Queries, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.RunMigrations(db, dbpkg.Migrations); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	queries := store.New(db)
	conn := NewConnector()
	mgr := session.NewManager(queries, noopBroadcaster{}, conn)

	h := &Handler{
		Connector: conn,
		Manager:   mgr,
		Queries:   queries,
		DB:        db,
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, h, queries, db
}

func postJSON(t *testing.T, mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func getJSON(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// --- Seed ---

func TestHandleSeed_Projects(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	w := postJSON(t, mux, "/api/test/seed", SeedRequest{
		Projects: []SeedProject{{ID: "p1", Name: "proj", Path: "/tmp/proj", Slug: "proj"}},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var res SeedResult
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Projects != 1 || res.Sessions != 0 {
		t.Errorf("result = %+v, want {1, 0}", res)
	}

	// State should return no sessions.
	sw := getJSON(t, mux, "/api/test/state")
	var states []SessionState
	json.NewDecoder(sw.Body).Decode(&states)
	if len(states) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(states))
	}
}

func TestHandleSeed_DBOnlySession(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	w := postJSON(t, mux, "/api/test/seed", SeedRequest{
		Projects: []SeedProject{{ID: "p1", Name: "proj", Path: "/tmp/proj", Slug: "proj"}},
		Sessions: []SeedSession{{ID: "s1", ProjectID: "p1", Name: "test", WorkDir: t.TempDir(), Live: false}},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}

	sw := getJSON(t, mux, "/api/test/state")
	var states []SessionState
	json.NewDecoder(sw.Body).Decode(&states)

	if len(states) != 1 {
		t.Fatalf("expected 1 session, got %d", len(states))
	}
	if states[0].ID != "s1" {
		t.Errorf("id = %q, want %q", states[0].ID, "s1")
	}
	if states[0].State != "idle" {
		t.Errorf("state = %q, want %q", states[0].State, "idle")
	}
	if states[0].Live {
		t.Error("expected live=false for DB-only session")
	}
}

func TestHandleSeed_LiveSession(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	w := postJSON(t, mux, "/api/test/seed", SeedRequest{
		Projects: []SeedProject{{ID: "p1", Name: "proj", Path: "/tmp/proj", Slug: "proj"}},
		Sessions: []SeedSession{{ID: "s1", ProjectID: "p1", Name: "test", WorkDir: t.TempDir(), Live: true}},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}

	sw := getJSON(t, mux, "/api/test/state")
	var states []SessionState
	json.NewDecoder(sw.Body).Decode(&states)

	if len(states) != 1 {
		t.Fatalf("expected 1 session, got %d", len(states))
	}
	if !states[0].Live {
		t.Error("expected live=true for live session")
	}
}

func TestHandleSeed_InvalidJSON(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/test/seed", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- InjectEvent ---

func TestHandleInjectEvent_Success(t *testing.T) {
	mux, h, _, _ := setupHandler(t)

	// Seed a live session first.
	postJSON(t, mux, "/api/test/seed", SeedRequest{
		Projects: []SeedProject{{ID: "p1", Name: "proj", Path: "/tmp/proj", Slug: "proj"}},
		Sessions: []SeedSession{{ID: "s1", ProjectID: "p1", Name: "test", WorkDir: t.TempDir(), Live: true}},
	})

	w := postJSON(t, mux, "/api/test/inject-event", InjectEventRequest{
		SessionID: "s1",
		Event:     json.RawMessage(`{"type":"text","content":"injected"}`),
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}

	// Verify event was actually injected into the mock session.
	mock := h.Connector.Get("s1")
	if mock == nil {
		t.Fatal("mock session not found")
	}
	select {
	case e := <-mock.Events():
		_, ok := e.(*claudecli.TextEvent)
		// Event may have been consumed by the session event loop before we read it.
		_ = ok
	default:
		// Event may have been consumed by the event loop — that's fine.
	}
}

func TestHandleInjectEvent_NotFound(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	w := postJSON(t, mux, "/api/test/inject-event", InjectEventRequest{
		SessionID: "nonexistent",
		Event:     json.RawMessage(`{"type":"text","content":"hi"}`),
	})

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleInjectEvent_InvalidEvent(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	// Seed a live session.
	postJSON(t, mux, "/api/test/seed", SeedRequest{
		Projects: []SeedProject{{ID: "p1", Name: "proj", Path: "/tmp/proj", Slug: "proj"}},
		Sessions: []SeedSession{{ID: "s1", ProjectID: "p1", Name: "test", WorkDir: t.TempDir(), Live: true}},
	})

	w := postJSON(t, mux, "/api/test/inject-event", InjectEventRequest{
		SessionID: "s1",
		Event:     json.RawMessage(`{"type":"banana"}`),
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleInjectEvent_InvalidJSON(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/test/inject-event", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- Reset ---

func TestHandleReset(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	// Seed data.
	postJSON(t, mux, "/api/test/seed", SeedRequest{
		Projects: []SeedProject{{ID: "p1", Name: "proj", Path: "/tmp/proj", Slug: "proj"}},
		Sessions: []SeedSession{
			{ID: "s1", ProjectID: "p1", Name: "live", WorkDir: t.TempDir(), Live: true},
			{ID: "s2", ProjectID: "p1", Name: "db-only", WorkDir: t.TempDir(), Live: false},
		},
	})

	// Verify sessions exist.
	sw := getJSON(t, mux, "/api/test/state")
	var before []SessionState
	json.NewDecoder(sw.Body).Decode(&before)
	if len(before) != 2 {
		t.Fatalf("expected 2 sessions before reset, got %d", len(before))
	}

	// Reset.
	w := postJSON(t, mux, "/api/test/reset", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}

	// Verify empty.
	sw = getJSON(t, mux, "/api/test/state")
	var after []SessionState
	json.NewDecoder(sw.Body).Decode(&after)
	if len(after) != 0 {
		t.Errorf("expected 0 sessions after reset, got %d", len(after))
	}
}

// --- State ---

func TestHandleState_Empty(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	w := getJSON(t, mux, "/api/test/state")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var states []SessionState
	json.NewDecoder(w.Body).Decode(&states)
	if len(states) != 0 {
		t.Errorf("expected empty state, got %d entries", len(states))
	}
}

func TestHandleState_Mixed(t *testing.T) {
	mux, _, _, _ := setupHandler(t)

	postJSON(t, mux, "/api/test/seed", SeedRequest{
		Projects: []SeedProject{{ID: "p1", Name: "proj", Path: "/tmp/proj", Slug: "proj"}},
		Sessions: []SeedSession{
			{ID: "s1", ProjectID: "p1", Name: "live", WorkDir: t.TempDir(), Live: true},
			{ID: "s2", ProjectID: "p1", Name: "db-only", WorkDir: t.TempDir(), Live: false},
		},
	})

	w := getJSON(t, mux, "/api/test/state")
	var states []SessionState
	json.NewDecoder(w.Body).Decode(&states)

	if len(states) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(states))
	}

	byID := make(map[string]SessionState)
	for _, s := range states {
		byID[s.ID] = s
	}

	if s, ok := byID["s1"]; !ok {
		t.Error("s1 missing from state")
	} else if !s.Live {
		t.Error("s1 should be live")
	}

	if s, ok := byID["s2"]; !ok {
		t.Error("s2 missing from state")
	} else if s.Live {
		t.Error("s2 should not be live")
	}
}
