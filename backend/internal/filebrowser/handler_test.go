package filebrowser_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

	// A directory is not a file, and content.OpenInRoot answers that the
	// same way on every route: nothing by that name to serve.
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
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

// A project directory is agent-written, so an .html or .svg in it (or a file
// whose bytes merely look like HTML) must never come back as a document on the
// app's own origin.
func TestHandleContent_ActiveTypesServedInert(t *testing.T) {
	h, pid, root := setup(t)

	files := map[string]string{
		"evil.html": `<script>fetch("/api/projects")</script>`,
		"evil.svg":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"noext":     `<html><script>alert(1)</script>`,
	}
	for name, body := range files {
		os.WriteFile(filepath.Join(root, name), []byte(body), 0o644)
	}

	for name, body := range files {
		w := serveContent(t, h, pid, name)
		if w.Code != 200 {
			t.Fatalf("%s: status %d: %s", name, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("%s: Content-Type = %q, want application/octet-stream", name, ct)
		}
		if d := w.Header().Get("Content-Disposition"); !strings.HasPrefix(d, "attachment") {
			t.Errorf("%s: Content-Disposition = %q, want an attachment", name, d)
		}
		// The file browser fetches the bytes itself; they must still arrive.
		if w.Body.String() != body {
			t.Errorf("%s: body = %q, want %q", name, w.Body.String(), body)
		}
	}
}

func TestHandleContent_SecurityHeaders(t *testing.T) {
	h, pid, root := setup(t)
	os.WriteFile(filepath.Join(root, "shot.png"), []byte("PNG"), 0o644)
	os.WriteFile(filepath.Join(root, "evil.html"), []byte("<script></script>"), 0o644)

	for _, name := range []string{"shot.png", "evil.html"} {
		w := serveContent(t, h, pid, name)
		for header, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Content-Security-Policy": "default-src 'none'; sandbox",
		} {
			if got := w.Header().Get(header); got != want {
				t.Errorf("%s: %s = %q, want %q", name, header, got, want)
			}
		}
	}

	png := serveContent(t, h, pid, "shot.png")
	if ct := png.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("shot.png: Content-Type = %q, want image/png", ct)
	}
	if d := png.Header().Get("Content-Disposition"); d != "" {
		t.Errorf("shot.png must render inline, got disposition %q", d)
	}
}

func serveContent(t *testing.T, h *filebrowser.Handler, pid, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files/content?path="+path, nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleContent(w, req)
	return w
}

// The listing follows a symlink only while it stays inside the project: one
// that leaves it is not described, not even as a type and size.
func TestHandleList_SymlinkOutOfProjectIsLeftOut(t *testing.T) {
	h, pid, root := setup(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, "real"), 0o755)
	os.Symlink(outside, filepath.Join(root, "out"))
	os.Symlink("real", filepath.Join(root, "in"))

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleList(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Entries []struct {
			Name  string `json:"name"`
			IsDir bool   `json:"isDir"`
		} `json:"entries"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	names := map[string]bool{}
	for _, e := range resp.Entries {
		names[e.Name] = e.IsDir
	}
	if _, listed := names["out"]; listed {
		t.Errorf("a symlink out of the project was listed: %+v", resp.Entries)
	}
	if !names["in"] {
		t.Errorf("a symlink inside the project should list as a directory: %+v", resp.Entries)
	}
}

func TestHandleList_TraversalThroughSymlink(t *testing.T) {
	h, pid, root := setup(t)
	os.Symlink(t.TempDir(), filepath.Join(root, "out"))

	req := httptest.NewRequest("GET", "/api/projects/"+pid+"/files?path=out", nil)
	req.SetPathValue("id", pid)
	w := httptest.NewRecorder()
	h.HandleList(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
