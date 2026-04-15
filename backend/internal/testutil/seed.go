package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	dbpkg "github.com/allbin/agentique/backend/db"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/google/uuid"
)

// SetupDB creates a fresh SQLite database with migrations applied.
// Cleaned up automatically when the test finishes.
// Returns both the raw *sql.DB (for transactions) and the generated Queries.
func SetupDB(t *testing.T) (*sql.DB, *store.Queries) {
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
	return db, store.New(db)
}

// SeedProject inserts a project and returns it.
func SeedProject(t *testing.T, q *store.Queries, name, path string) store.Project {
	t.Helper()
	id := uuid.New().String()
	p, err := q.CreateProject(context.Background(), store.CreateProjectParams{
		ID:   id,
		Name: name,
		Path: path,
		Slug: name,
	})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return p
}

// SeedSession inserts a session and returns it.
func SeedSession(t *testing.T, q *store.Queries, projectID, state string) store.Session {
	t.Helper()
	id := uuid.New().String()
	s, err := q.CreateSession(context.Background(), store.CreateSessionParams{
		ID:             id,
		ProjectID:      projectID,
		Name:           "test-session",
		WorkDir:        t.TempDir(),
		State:          state,
		Model:          "opus",
		PermissionMode: "default",
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return s
}

// SeedSessionWithClaude inserts a session with a Claude session ID set.
func SeedSessionWithClaude(t *testing.T, q *store.Queries, projectID, state, claudeID string) store.Session {
	t.Helper()
	s := SeedSession(t, q, projectID, state)
	if err := q.UpdateClaudeSessionID(context.Background(), store.UpdateClaudeSessionIDParams{
		ClaudeSessionID: sql.NullString{String: claudeID, Valid: true},
		ID:              s.ID,
	}); err != nil {
		t.Fatalf("seed claude id: %v", err)
	}
	s.ClaudeSessionID = sql.NullString{String: claudeID, Valid: true}
	return s
}

// SeedEvent inserts a single session event.
func SeedEvent(t *testing.T, q *store.Queries, sessionID string, turnIndex, seq int64, typ, data string) {
	t.Helper()
	if err := q.InsertEvent(context.Background(), store.InsertEventParams{
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Seq:       seq,
		Type:      typ,
		Data:      data,
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}
