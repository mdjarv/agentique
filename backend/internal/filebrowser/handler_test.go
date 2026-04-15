package filebrowser_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/filebrowser"
	"github.com/mdjarv/agentique/backend/internal/testutil"
)

func setup(t *testing.T) (*filebrowser.Handler, string, string) {
	t.Helper()
	_, q := testutil.SetupDB(t)
	root := t.TempDir()
	p := testutil.SeedProject(t, q, "test", root)
	return &filebrowser.Handler{Queries: q}, p.ID, root
}

func TestHandleList_Root(t *testing.T) {
	h, pid, root := setup(t)

	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi"), 0o644)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleList(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Path    string `json:"path"`
		Entries []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"isDir"`
		} `json:"entries"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
	// dirs first
	if resp.Entries[0].Name != "src" || !resp.Entries[0].IsDir {
		t.Fatalf("expected dir 'src' first, got %+v", resp.Entries[0])
	}
	if resp.Entries[1].Name != "README.md" || resp.Entries[1].IsDir {
		t.Fatalf("expected file 'README.md' second, got %+v", resp.Entries[1])
	}
}

func TestHandleList_Subdir(t *testing.T) {
	h, pid, root := setup(t)

	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o644)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files?path=src", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleList(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Entries []struct{ Name string } `json:"entries"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Entries) != 1 || resp.Entries[0].Name != "main.go" {
		t.Fatalf("expected [main.go], got %+v", resp.Entries)
	}
}

func TestHandleList_SkipsGitDir(t *testing.T) {
	h, pid, root := setup(t)

	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=x"), 0o644)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleList(w, req)

	var resp struct {
		Entries []struct{ Name string } `json:"entries"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Entries) != 1 || resp.Entries[0].Name != ".env" {
		t.Fatalf("expected [.env], got %+v", resp.Entries)
	}
}

func TestHandleList_PathTraversal(t *testing.T) {
	h, pid, _ := setup(t)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files?path=../../etc", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleList(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleContent_TextFile(t *testing.T) {
	h, pid, root := setup(t)

	content := "package main\n\nfunc main() {}\n"
	os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0o644)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files/content?path=main.go", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleContent(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != content {
		t.Fatalf("content mismatch: got %q", w.Body.String())
	}
}

func TestHandleContent_MissingPath(t *testing.T) {
	h, pid, _ := setup(t)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files/content", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleContent(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleContent_Directory(t *testing.T) {
	h, pid, root := setup(t)

	os.MkdirAll(filepath.Join(root, "src"), 0o755)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files/content?path=src", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleContent(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleContent_PathTraversal(t *testing.T) {
	h, pid, _ := setup(t)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files/content?path=../../etc/passwd", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleContent(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleContent_TooLarge(t *testing.T) {
	h, pid, root := setup(t)

	// Create a file just over 1MB.
	big := make([]byte, 1<<20+1)
	os.WriteFile(filepath.Join(root, "big.txt"), big, 0o644)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files/content?path=big.txt", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleContent(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
}

func TestHandleContent_NotFound(t *testing.T) {
	h, pid, _ := setup(t)

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files/content?path=nope.txt", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleContent(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
