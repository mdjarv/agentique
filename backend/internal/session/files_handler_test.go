package session

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

func TestFilesHandlerAllowsFilenamesContainingDots(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	sessionID := "session-1"
	sessionDir := filepath.Join(paths.SessionFilesDir(), sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "notes..md"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/files/notes..md", nil)
	req.SetPathValue("id", sessionID)
	req.SetPathValue("filepath", "notes..md")
	w := httptest.NewRecorder()

	(&FilesHandler{}).HandleServe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", w.Body.String(), "ok")
	}
}

func TestFilesHandlerRejectsPathTraversal(t *testing.T) {
	t.Setenv("AGENTIQUE_HOME", t.TempDir())
	sessionID := "session-1"
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/files/../secret.md", nil)
	req.SetPathValue("id", sessionID)
	req.SetPathValue("filepath", "../secret.md")
	w := httptest.NewRecorder()

	(&FilesHandler{}).HandleServe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
