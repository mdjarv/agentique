package session

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

// The session id is a path component. A {id} wildcard does not constrain it to
// one segment (ServeMux unescapes the capture), and the handler's own
// "still inside the session dir" check derives its root from that same value,
// so it cannot detect an escape. Only validating the id can.
func TestSessionFilesRejectsTraversingSessionID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTIQUE_HOME", tmp)
	if err := os.MkdirAll(paths.SessionFilesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// Mirrors the real data dir: the database sits beside session-files.
	secret := filepath.Join(filepath.Dir(paths.SessionFilesDir()), "agentique.db")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/files/{filepath...}", (&FilesHandler{}).HandleServe)

	for _, raw := range []string{
		"/api/sessions/..%2F/files/agentique.db",
		"/api/sessions/%2e%2e/files/agentique.db",
		"/api/sessions/..%2f..%2f..%2f..%2f..%2f..%2f..%2fetc/files/hostname",
		"/api/sessions/not-a-uuid/files/x",
	} {
		req := httptest.NewRequest(http.MethodGet, raw, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s served content (status 200, body %q)", raw, rec.Body.String())
		}
	}
}

func TestSessionFilesServesItsOwnFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTIQUE_HOME", tmp)
	const sid = "11111111-2222-3333-4444-555555555555"
	dir := filepath.Join(paths.SessionFilesDir(), sid)
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "shot.png"), []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/files/{filepath...}", (&FilesHandler{}).HandleServe)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sid+"/files/nested/shot.png", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "PNGDATA" {
		t.Errorf("body = %q, want PNGDATA", rec.Body.String())
	}
}
