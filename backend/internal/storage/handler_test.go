package storage

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/paths"
	"github.com/mdjarv/agentique/backend/internal/store"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

func TestSafeWorktreePath(t *testing.T) {
	root := filepath.Clean(paths.WorktreeDir())

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid two levels", filepath.Join(root, "myproject", "session-abc"), false},
		{"valid deeper", filepath.Join(root, "myproject", "session-abc", "sub"), false},
		{"root itself", root, true},
		{"bucket only", filepath.Join(root, "myproject"), true},
		{"traversal escape", filepath.Join(root, "myproject", "..", "..", "etc"), true},
		{"outside root", "/etc/passwd", true},
		{"relative path", "myproject/session-abc", true},
		{"sneaky prefix sibling", root + "-evil/x/y", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeWorktreePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("safeWorktreePath(%q) err = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func deleteWorktree(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/storage/worktrees?path="+path, nil)
	rec := httptest.NewRecorder()
	h.HandleDeleteWorktree(rec, req)
	return rec
}

// The usage snapshot the client acts on can be a minute stale, so containment
// alone is not enough: a path a session row still references must be refused
// server-side, or the delete pulls a worktree out from under a running CLI.
func TestHandleDeleteWorktreeRefusesASessionOwnedPath(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	_, q := testutil.SetupDB(t)
	project := testutil.SeedProject(t, q, "proj", t.TempDir())
	sess := testutil.SeedSession(t, q, project.ID, "running")

	wt := filepath.Join(paths.WorktreeDir(), "proj", "session-live")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	// Stored uncleaned on purpose: the guard must compare cleaned paths.
	if err := q.UpdateSessionWorktree(context.Background(), store.UpdateSessionWorktreeParams{
		WorkDir:      wt,
		WorktreePath: sql.NullString{String: wt + string(os.PathSeparator), Valid: true},
		ID:           sess.ID,
	}); err != nil {
		t.Fatal(err)
	}

	h := &Handler{Queries: q}
	rec := deleteWorktree(t, h, wt)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("owned worktree must survive the refused delete: %v", err)
	}

	// A genuinely orphaned sibling is still removable.
	orphan := filepath.Join(paths.WorktreeDir(), "proj", "session-orphan")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	rec = deleteWorktree(t, h, orphan)
	if rec.Code != http.StatusOK {
		t.Fatalf("orphan delete status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan worktree should be gone, stat err = %v", err)
	}
}

// Without a DB view orphan-hood cannot be proven, and an unprovable orphan is
// treated as owned: the guard fails closed.
func TestHandleDeleteWorktreeFailsClosedWithoutQueries(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	wt := filepath.Join(paths.WorktreeDir(), "proj", "session-x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := deleteWorktree(t, &Handler{}, wt)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree must survive when ownership cannot be checked: %v", err)
	}
}
