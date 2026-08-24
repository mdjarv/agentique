package session

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdjarv/agentique/backend/internal/paths"
)

func TestSessionFileDisposition(t *testing.T) {
	cases := []struct {
		name        string
		wantType    string
		wantAttach  bool
		explanation string
	}{
		{"shot.png", "image/png", false, "screenshots must still render inline"},
		{"a.JPEG", "image/jpeg", false, "extension match is case-insensitive"},
		{"notes.md", "text/plain; charset=utf-8", false, "the UI fetches and renders markdown itself"},
		{"data.json", "application/json", false, "inert"},
		{"clip.mp4", "video/mp4", false, "media elements do not execute their payload"},

		{"report.html", "application/octet-stream", true, "HTML would run as a same-origin document"},
		{"page.htm", "application/octet-stream", true, "same as .html"},
		{"pic.svg", "application/octet-stream", true, "SVG runs script when navigated to directly"},
		{"x.xhtml", "application/octet-stream", true, "same as .html"},
		{"app.js", "application/octet-stream", true, "never serve script from this origin"},
		{"doc.pdf", "application/octet-stream", true, "PDF viewers are an active surface"},
		{"README", "application/octet-stream", true, "no extension means no proof it is inert"},
		{"weird.unknown", "application/octet-stream", true, "unknown types are sniffable"},
	}
	for _, c := range cases {
		ct, disp := sessionFileDisposition(c.name)
		if ct != c.wantType {
			t.Errorf("%s: content type = %q, want %q (%s)", c.name, ct, c.wantType, c.explanation)
		}
		if attached := disp != ""; attached != c.wantAttach {
			t.Errorf("%s: attachment = %v, want %v (%s)", c.name, attached, c.wantAttach, c.explanation)
		}
	}
}

func TestSanitizeFilenameKeepsTheHeaderUnambiguous(t *testing.T) {
	for in, want := range map[string]string{
		`plain.txt`:      `plain.txt`,
		`a"b.txt`:        `a_b.txt`,
		"a\r\nb.txt":     `a__b.txt`,
		`back\slash.txt`: `back_slash.txt`,
		``:               `download`,
	} {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

// The end-to-end shape of the fix: an agent-written HTML file reaches the
// browser as an inert download, never as a document on the app's origin.
func TestSessionFilesServeAgentHTMLAsInertDownload(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTIQUE_HOME", tmp)
	const sid = "11111111-2222-3333-4444-555555555555"
	dir := filepath.Join(paths.SessionFilesDir(), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "evil.html"),
		[]byte(`<script>fetch("/api/sessions")</script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/files/{filepath...}", (&FilesHandler{}).HandleServe)

	serve := func(name string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/"+sid+"/files/"+name, nil))
		return rec
	}

	html := serve("evil.html")
	if ct := html.Header().Get("Content-Type"); ct == "text/html; charset=utf-8" {
		t.Errorf("evil.html served as an active document (%s)", ct)
	}
	if html.Header().Get("Content-Disposition") == "" {
		t.Error("evil.html must be served as an attachment")
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
	} {
		if got := html.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	png := serve("shot.png")
	if ct := png.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("shot.png content type = %q, want image/png", ct)
	}
	if d := png.Header().Get("Content-Disposition"); d != "" {
		t.Errorf("shot.png must render inline, got disposition %q", d)
	}
}

func TestSessionFilesRefuseDirectoryListings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTIQUE_HOME", tmp)
	const sid = "11111111-2222-3333-4444-555555555555"
	if err := os.MkdirAll(filepath.Join(paths.SessionFilesDir(), sid, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/files/{filepath...}", (&FilesHandler{}).HandleServe)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/"+sid+"/files/sub", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a directory", rec.Code)
	}
}
