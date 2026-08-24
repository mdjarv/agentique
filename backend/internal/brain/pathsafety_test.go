package brain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/httperror"
)

// Go's ServeMux matches on the ESCAPED path but unescapes the capture, so a
// {id} wildcard happily yields "../../x" for a %2F-encoded request. These
// exercise the two routes where that value became a filesystem path.

func newTestBrain(t *testing.T) (*Service, string) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "brain")
	svc, err := New(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatalf("brain.New: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	return svc, base
}

func TestDeleteMemoryRejectsEscapedTraversal(t *testing.T) {
	svc, base := newTestBrain(t)
	victim := filepath.Join(base, "victim.md")
	if err := os.WriteFile(victim, []byte("do not delete"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{Service: svc}
	mux := http.NewServeMux()
	mux.Handle("DELETE /api/brain/memories/{id}", httperror.HandlerFunc(h.HandleDelete))

	req := httptest.NewRequest(http.MethodDelete, "/api/brain/memories/..%2F..%2Fvictim", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("file outside the brain dir was deleted: %v", err)
	}
}

func TestRestoreSnapshotRejectsEscapedTraversal(t *testing.T) {
	svc, base := newTestBrain(t)
	// A directory the caller must not be able to read into the brain.
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(outside, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "global", "leak.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &Handler{Service: svc}
	mux := http.NewServeMux()
	mux.Handle("POST /api/brain/snapshots/{id}/restore", httperror.HandlerFunc(h.HandleRestoreSnapshot))

	req := httptest.NewRequest(http.MethodPost, "/api/brain/snapshots/..%2F..%2Foutside/restore", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(svc.dir, "global", "leak.md")); err == nil {
		t.Error("a directory outside the brain was copied into it")
	}
}

func TestRestoreSnapshotAcceptsARealSnapshotID(t *testing.T) {
	svc, _ := newTestBrain(t)
	info, err := svc.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := svc.RestoreSnapshot(info.ID); err != nil {
		t.Errorf("RestoreSnapshot(%q): %v", info.ID, err)
	}
}
